package engine

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// JavaLANMulticastAddr is the standard Minecraft Java Edition LAN server discovery endpoint.
	JavaLANMulticastAddr        = "224.0.2.60:4445"
	defaultLANBroadcastInterval = 1500 * time.Millisecond
	testLANBroadcastInterval    = 120 * time.Millisecond
)

// FormatJavaLANPacket formats a Minecraft Java Edition LAN announcement payload.
func FormatJavaLANPacket(motd string, port int) []byte {
	replacer := strings.NewReplacer("[", " ", "]", " ", "\r", " ", "\n", " ")
	clean := strings.Join(strings.Fields(replacer.Replace(motd)), " ")
	if clean == "" {
		clean = "MC SekaiLink"
	}
	if utf8.RuneCountInString(clean) > 80 {
		runes := []rune(clean)
		clean = strings.TrimSpace(string(runes[:80]))
		if clean == "" {
			clean = "MC SekaiLink"
		}
	}
	return []byte(fmt.Sprintf("[MOTD]%s[/MOTD][AD]%d[/AD]", clean, port))
}

func detectRouteSourceIPv4(remoteAddr string) net.IP {
	c, err := net.Dial("udp4", remoteAddr)
	if err != nil {
		return nil
	}
	defer c.Close()
	if ua, ok := c.LocalAddr().(*net.UDPAddr); ok && ua.IP != nil {
		return ua.IP.To4()
	}
	return nil
}

func detectPrimaryOutboundIPv4() net.IP {
	return detectRouteSourceIPv4("1.1.1.1:80")
}

func detectMulticastRouteIPv4() net.IP {
	return detectRouteSourceIPv4(JavaLANMulticastAddr)
}

// RunJavaLANBroadcaster periodically broadcasts Minecraft Java LAN discovery beacons until ctx ends.
// If targetAddr is non-empty, packets are sent there (used for deterministic tests); otherwise 224.0.2.60:4445 is used.
func RunJavaLANBroadcaster(ctx context.Context, motd string, port int, shouldSend func() bool, targetAddr string) {
	if port <= 0 || port > 65535 {
		return
	}
	pkt := FormatJavaLANPacket(motd, port)

	if strings.TrimSpace(targetAddr) != "" {
		dst, err := net.ResolveUDPAddr("udp4", strings.TrimSpace(targetAddr))
		if err != nil {
			return
		}
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
		if err != nil {
			return
		}
		defer conn.Close()

		broadcastTickLoop(ctx, []*net.UDPConn{conn}, dst, pkt, testLANBroadcastInterval, shouldSend)
		return
	}

	mcastDst := &net.UDPAddr{IP: net.IPv4(224, 0, 2, 60), Port: 4445}
	var conns []*net.UDPConn
	if defConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0}); err == nil {
		conns = append(conns, defConn)
		defer defConn.Close()
	}

	primaryIP := detectPrimaryOutboundIPv4()
	mcastIP := detectMulticastRouteIPv4()
	if primaryIP != nil && !primaryIP.IsLoopback() && (mcastIP == nil || !primaryIP.Equal(mcastIP)) {
		if lanConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: primaryIP, Port: 0}); err == nil {
			conns = append(conns, lanConn)
			defer lanConn.Close()
		}
	}

	if len(conns) == 0 {
		return
	}
	broadcastTickLoop(ctx, conns, mcastDst, pkt, defaultLANBroadcastInterval, shouldSend)
}

func broadcastTickLoop(
	ctx context.Context,
	conns []*net.UDPConn,
	dst *net.UDPAddr,
	pkt []byte,
	interval time.Duration,
	shouldSend func() bool,
) {
	send := func() {
		if shouldSend != nil && !shouldSend() {
			return
		}
		for _, c := range conns {
			_, _ = c.WriteToUDP(pkt, dst)
		}
	}

	send()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			send()
		}
	}
}
