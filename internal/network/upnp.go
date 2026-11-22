package network

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

const defaultPMPPort = 5351

type portMapper interface {
	GetExternalIPAddressCtx(ctx context.Context) (string, error)
	AddPortMappingCtx(ctx context.Context, NewRemoteHost string, NewExternalPort uint16, NewProtocol string, NewInternalPort uint16, NewInternalClient string, NewEnabled bool, NewPortMappingDescription string, NewLeaseDuration uint32) error
	DeletePortMappingCtx(ctx context.Context, NewRemoteHost string, NewExternalPort uint16, NewProtocol string) error
}

func findPortMapper(ctx context.Context) (portMapper, error) {
	cctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()

	if clients, _, err := internetgateway2.NewWANIPConnection2ClientsCtx(cctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	if clients, _, err := internetgateway2.NewWANIPConnection1ClientsCtx(cctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	if clients, _, err := internetgateway2.NewWANPPPConnection1ClientsCtx(cctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	if clients, _, err := internetgateway1.NewWANIPConnection1ClientsCtx(cctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	if clients, _, err := internetgateway1.NewWANPPPConnection1ClientsCtx(cctx); err == nil && len(clients) > 0 {
		return clients[0], nil
	}
	return nil, fmt.Errorf("UPnP destekli modem/router bulunamadi")
}

// ProbeUPnPExternalIP probes UPnP, NAT-PMP (RFC 6886), and PCP (RFC 6887) gateways concurrently.
func ProbeUPnPExternalIP(ctx context.Context) (string, bool) {
	pCtx, cancel := context.WithTimeout(ctx, 2600*time.Millisecond)
	defer cancel()

	var (
		wg        sync.WaitGroup
		mu        sync.Mutex
		foundIP   string
		supported bool
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		mapper, err := findPortMapper(pCtx)
		if err != nil {
			return
		}
		mu.Lock()
		supported = true
		mu.Unlock()
		ip, err := mapper.GetExternalIPAddressCtx(pCtx)
		if err == nil && strings.TrimSpace(ip) != "" {
			mu.Lock()
			if foundIP == "" {
				foundIP = strings.TrimSpace(ip)
			}
			mu.Unlock()
		}
	}()

	go func() {
		defer wg.Done()
		for _, gw := range discoverGatewayIPs() {
			ip, ok := probeNATPMPExternalIP(pCtx, net.JoinHostPort(gw, strconv.Itoa(defaultPMPPort)))
			if ok {
				mu.Lock()
				supported = true
				if foundIP == "" && ip != "" {
					foundIP = ip
				}
				mu.Unlock()
				return
			}
		}
	}()

	wg.Wait()
	return foundIP, supported
}

// MapPortUPnP attempts automatic router port forwarding using UPnP, PCP (RFC 6887), and NAT-PMP (RFC 6886).
func MapPortUPnP(ctx context.Context, port int, protocol string, desc string) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("gecersiz port: %d", port)
	}
	localIP := engine.DetectLocalLANIP()

	mapper, upnpErr := findPortMapper(ctx)
	if upnpErr == nil {
		if err := mapper.AddPortMappingCtx(ctx, "", uint16(port), protocol, uint16(port), localIP, true, desc, 0); err == nil {
			return nil
		} else {
			upnpErr = err
		}
	}

	gateways := discoverGatewayIPs()
	var pcpErr, pmpErr error
	for _, gw := range gateways {
		gwAddr := net.JoinHostPort(gw, strconv.Itoa(defaultPMPPort))
		if err := pcpMapPort(ctx, gwAddr, localIP, port, protocol, 7200); err == nil {
			return nil
		} else if pcpErr == nil {
			pcpErr = err
		}
		if err := natPmpMapPort(ctx, gwAddr, port, protocol, 7200); err == nil {
			return nil
		} else if pmpErr == nil {
			pmpErr = err
		}
	}

	return fmt.Errorf("otomatik port acma basarisiz (UPnP=%v, PCP=%v, NAT-PMP=%v)", upnpErr, pcpErr, pmpErr)
}

func UnmapPortUPnP(ctx context.Context, port int, protocol string) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("gecersiz port: %d", port)
	}
	if mapper, err := findPortMapper(ctx); err == nil {
		if err2 := mapper.DeletePortMappingCtx(ctx, "", uint16(port), protocol); err2 == nil {
			return nil
		}
	}
	localIP := engine.DetectLocalLANIP()
	for _, gw := range discoverGatewayIPs() {
		gwAddr := net.JoinHostPort(gw, strconv.Itoa(defaultPMPPort))
		if err := pcpMapPort(ctx, gwAddr, localIP, port, protocol, 0); err == nil {
			return nil
		}
		if err := natPmpMapPort(ctx, gwAddr, port, protocol, 0); err == nil {
			return nil
		}
	}
	return fmt.Errorf("port kapatilamadi (UPnP/PCP/NAT-PMP yanit vermedi)")
}

// discoverGatewayIPs finds probable IPv4 default gateway addresses for PCP/NAT-PMP across Windows, Linux, and Android.
func discoverGatewayIPs() []string {
	var out []string
	seen := make(map[string]bool)
	add := func(ipStr string) {
		ip := net.ParseIP(strings.TrimSpace(ipStr)).To4()
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() {
			return
		}
		s := ip.String()
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}

	// 1. Linux / Android routing table default gateway
	if f, err := os.Open("/proc/net/route"); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) >= 3 && fields[1] == "00000000" {
				if val, pErr := strconv.ParseUint(fields[2], 16, 32); pErr == nil && val != 0 {
					// /proc/net/route gateway hex is host-order little-endian IPv4 on LE systems (x86_64/arm64)
					b0 := byte(val & 0xFF)
					b1 := byte((val >> 8) & 0xFF)
					b2 := byte((val >> 16) & 0xFF)
					b3 := byte((val >> 24) & 0xFF)
					add(fmt.Sprintf("%d.%d.%d.%d", b0, b1, b2, b3))
				}
			}
		}
		_ = f.Close()
	}

	// 2. Cross-platform subnet gateway heuristic (.1 and .254 on active private IPv4 interfaces)
	if lanIP := net.ParseIP(engine.DetectLocalLANIP()).To4(); lanIP != nil && lanIP.IsPrivate() {
		add(fmt.Sprintf("%d.%d.%d.1", lanIP[0], lanIP[1], lanIP[2]))
		add(fmt.Sprintf("%d.%d.%d.254", lanIP[0], lanIP[1], lanIP[2]))
	}
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, a := range addrs {
				ipNet, ok := a.(*net.IPNet)
				if !ok {
					continue
				}
				ip4 := ipNet.IP.To4()
				if ip4 == nil || !ip4.IsPrivate() {
					continue
				}
				add(fmt.Sprintf("%d.%d.%d.1", ip4[0], ip4[1], ip4[2]))
			}
		}
	}
	return out
}

// --- Pure-Go PCP (Port Control Protocol - RFC 6887) ---

func pcpMapPort(ctx context.Context, gwAddr, localIPStr string, internalPort int, protocol string, lifetimeSec uint32) error {
	clientIP := net.ParseIP(strings.TrimSpace(localIPStr)).To16()
	if clientIP == nil {
		clientIP = net.ParseIP("127.0.0.1").To16()
	}

	var protoNum byte = 6 // TCP
	if strings.EqualFold(strings.TrimSpace(protocol), "UDP") {
		protoNum = 17
	}

	var nonce [12]byte
	_, _ = rand.Read(nonce[:])

	suggestedPort := uint16(internalPort)
	if lifetimeSec == 0 {
		suggestedPort = 0
	}

	req := buildPCPMapRequest(clientIP, nonce, protoNum, uint16(internalPort), suggestedPort, lifetimeSec)

	rAddr, err := net.ResolveUDPAddr("udp4", gwAddr)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp4", nil, rAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	deadline := time.Now().Add(1400 * time.Millisecond)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write(req); err != nil {
		return err
	}
	buf := make([]byte, 128)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	_, _, err = parsePCPMapResponse(buf[:n], nonce)
	return err
}

func buildPCPMapRequest(clientIP16 net.IP, nonce [12]byte, protoNum byte, internalPort, suggestedExternalPort uint16, lifetimeSec uint32) []byte {
	req := make([]byte, 60)
	req[0] = 2 // PCP Version 2
	req[1] = 1 // R=0 | Opcode=1 (MAP)
	binary.BigEndian.PutUint32(req[4:8], lifetimeSec)
	copy(req[8:24], clientIP16.To16())
	copy(req[24:36], nonce[:])
	req[36] = protoNum
	binary.BigEndian.PutUint16(req[40:42], internalPort)
	binary.BigEndian.PutUint16(req[42:44], suggestedExternalPort)
	// IPv4-mapped unspecified external address ::ffff:0.0.0.0
	req[54], req[55] = 0xFF, 0xFF
	return req
}

func parsePCPMapResponse(resp []byte, expectedNonce [12]byte) (string, uint16, error) {
	if len(resp) < 60 {
		return "", 0, fmt.Errorf("PCP yaniti cok kisa (%d)", len(resp))
	}
	if resp[0] != 2 || resp[1] != 0x81 {
		return "", 0, fmt.Errorf("gecersiz PCP basligi: ver=%d op=0x%02x", resp[0], resp[1])
	}
	if resp[3] != 0 {
		return "", 0, fmt.Errorf("PCP hata kodu: %d", resp[3])
	}
	if !bytes.Equal(resp[24:36], expectedNonce[:]) {
		return "", 0, fmt.Errorf("PCP nonce eslesmedi")
	}
	extPort := binary.BigEndian.Uint16(resp[42:44])
	extIP := net.IP(resp[44:60])
	extStr := ""
	if ip4 := extIP.To4(); ip4 != nil && !ip4.IsUnspecified() {
		extStr = ip4.String()
	} else if !extIP.IsUnspecified() {
		extStr = extIP.String()
	}
	return extStr, extPort, nil
}

// --- Pure-Go NAT-PMP (RFC 6886) ---

func probeNATPMPExternalIP(ctx context.Context, gwAddr string) (string, bool) {
	rAddr, err := net.ResolveUDPAddr("udp4", gwAddr)
	if err != nil {
		return "", false
	}
	conn, err := net.DialUDP("udp4", nil, rAddr)
	if err != nil {
		return "", false
	}
	defer conn.Close()

	deadline := time.Now().Add(1200 * time.Millisecond)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write([]byte{0x00, 0x00}); err != nil {
		return "", false
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil || n < 4 {
		return "", false
	}
	// Version mismatch from PCP-only gateway (version=2, R=1, result=UNSUPP_VERSION=1) still proves port 5351 gateway is alive
	if buf[0] == 2 && (buf[1]&0x80) != 0 {
		return "", true
	}
	ipStr, ok := parseNATPMPExternalIPResponse(buf[:n])
	return ipStr, ok
}

func parseNATPMPExternalIPResponse(resp []byte) (string, bool) {
	if len(resp) < 12 || resp[0] != 0x00 || resp[1] != 0x80 {
		return "", false
	}
	result := binary.BigEndian.Uint16(resp[2:4])
	if result != 0 {
		return "", false
	}
	ip := net.IPv4(resp[8], resp[9], resp[10], resp[11])
	return ip.String(), true
}

func natPmpMapPort(ctx context.Context, gwAddr string, internalPort int, protocol string, lifetimeSec uint32) error {
	var op byte = 2 // TCP
	if strings.EqualFold(strings.TrimSpace(protocol), "UDP") {
		op = 1
	}

	req := make([]byte, 12)
	req[0] = 0
	req[1] = op
	binary.BigEndian.PutUint16(req[4:6], uint16(internalPort))
	if lifetimeSec > 0 {
		binary.BigEndian.PutUint16(req[6:8], uint16(internalPort))
	}
	binary.BigEndian.PutUint32(req[8:12], lifetimeSec)

	rAddr, err := net.ResolveUDPAddr("udp4", gwAddr)
	if err != nil {
		return err
	}
	conn, err := net.DialUDP("udp4", nil, rAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	deadline := time.Now().Add(1400 * time.Millisecond)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write(req); err != nil {
		return err
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		return err
	}
	if n < 16 || buf[0] != 0 || buf[1] != (0x80|op) {
		return fmt.Errorf("gecersiz NAT-PMP yaniti")
	}
	result := binary.BigEndian.Uint16(buf[2:4])
	if result != 0 {
		return fmt.Errorf("NAT-PMP hata kodu: %d", result)
	}
	return nil
}
