package network

import (
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

func TestRelayServerAndClientTCPUDP(t *testing.T) {
	// 1. Local TCP echo server (simulating Java Edition)
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpLn.Close()
	go func() {
		for {
			c, err := tcpLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(c)
		}
	}()
	localJavaPort := tcpLn.Addr().(*net.TCPAddr).Port

	// 2. Local UDP echo server (simulating Bedrock RakNet)
	udpEcho, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer udpEcho.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := udpEcho.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = udpEcho.WriteToUDP(buf[:n], addr)
		}
	}()
	localBedrockPort := udpEcho.LocalAddr().(*net.UDPAddr).Port

	// 3. Start SekaiLink RelayServer
	relayLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewRelayServer("127.0.0.1", "test-secret")
	defer srv.Close()
	go func() { _ = srv.Serve(relayLn) }()

	// 4. Connect RelayManager
	rm := NewRelayManager()
	defer rm.Stop()
	st, err := rm.Start(relayLn.Addr().String(), "test-secret", localJavaPort, localBedrockPort, true, true)
	if err != nil {
		t.Fatalf("RelayManager.Start failed: %v", err)
	}
	if !st.Running || st.JavaAddress == "" || st.BedrockAddress == "" {
		t.Fatalf("unexpected RelayState: %+v", st)
	}

	// 5. Verify TCP forwarding
	tc, err := net.DialTimeout("tcp", st.JavaAddress, 3*time.Second)
	if err != nil {
		t.Fatalf("dial JavaAddress failed: %v", err)
	}
	defer tc.Close()
	tcpMsg := "java-relay-ok"
	if _, err := tc.Write([]byte(tcpMsg)); err != nil {
		t.Fatal(err)
	}
	tcpBuf := make([]byte, len(tcpMsg))
	_ = tc.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(tc, tcpBuf); err != nil {
		t.Fatalf("TCP read failed: %v", err)
	}
	if string(tcpBuf) != tcpMsg {
		t.Fatalf("TCP echo mismatch: %q vs %q", string(tcpBuf), tcpMsg)
	}

	// 6. Verify UDP forwarding
	uAddr, err := net.ResolveUDPAddr("udp", st.BedrockAddress)
	if err != nil {
		t.Fatal(err)
	}
	uc, err := net.DialUDP("udp", nil, uAddr)
	if err != nil {
		t.Fatalf("dial BedrockAddress failed: %v", err)
	}
	defer uc.Close()
	udpMsg := "bedrock-udp-relay-ok"
	if _, err := uc.Write([]byte(udpMsg)); err != nil {
		t.Fatal(err)
	}
	udpBuf := make([]byte, 256)
	_ = uc.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := uc.Read(udpBuf)
	if err != nil {
		t.Fatalf("UDP read failed: %v", err)
	}
	if string(udpBuf[:n]) != udpMsg {
		t.Fatalf("UDP echo mismatch: %q vs %q", string(udpBuf[:n]), udpMsg)
	}
}

func TestRelayTLSNodeIDPortRangeAndProbe(t *testing.T) {
	// 1. Local TCP & UDP echo servers
	tcpLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpLn.Close()
	go func() {
		for {
			c, err := tcpLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(c)
		}
	}()
	localJavaPort := tcpLn.Addr().(*net.TCPAddr).Port

	udpEcho, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer udpEcho.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			n, addr, err := udpEcho.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = udpEcho.WriteToUDP(buf[:n], addr)
		}
	}()
	localBedrockPort := udpEcho.LocalAddr().(*net.UDPAddr).Port

	// 2. Start TLS 1.3 RelayServer with persistent cert dir, NodeID SV-Oracle, and constrained port range
	certDir := t.TempDir()
	tlsCfg, pin, err := LoadOrCreateRelayTLS(certDir)
	if err != nil || pin == "" {
		t.Fatalf("LoadOrCreateRelayTLS failed: %v (pin=%q)", err, pin)
	}
	// Reloading from same certDir must preserve identical CertPin
	_, pin2, err := LoadOrCreateRelayTLS(certDir)
	if err != nil || pin2 != pin {
		t.Fatalf("expected persistent CertPin %q on reload, got %q (err=%v)", pin, pin2, err)
	}

	relayLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := NewRelayServerWithOptions(RelayServerOptions{
		NodeID:     "SV-Oracle",
		PublicHost: "127.0.0.1",
		Secret:     "vds-secret",
		PortMin:    37100,
		PortMax:    37150,
		TLSConfig:  tlsCfg,
		AllowPlain: false,
	})
	defer srv.Close()
	go func() { _ = srv.Serve(relayLn) }()

	// Find an unused closed port to simulate an offline dead VDS node
	deadLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	deadAddr := deadLn.Addr().String()
	deadLn.Close()

	// 3. Probe pool (offline + online TLS node) & StartBestNode
	nodes := []models.RelayNode{
		{ID: "SV-Dead", Address: deadAddr, Secret: "x", TLS: true, Enabled: true},
		{ID: "SV-Oracle", Address: relayLn.Addr().String(), Secret: "vds-secret", TLS: true, CertPin: pin, Enabled: true},
	}

	probes := ProbeRelayNodes(nodes)
	if len(probes) != 2 || !probes[0].Online || probes[0].NodeID != "SV-Oracle" || !probes[0].Encrypted || probes[1].Online {
		t.Fatalf("unexpected probe ordering/results: %+v", probes)
	}

	// Wrong CertPin must be rejected
	badProbe := ProbeRelayNode(models.RelayNode{
		ID:      "SV-BadPin",
		Address: relayLn.Addr().String(),
		TLS:     true,
		CertPin: "0000000000000000000000000000000000000000000000000000000000000000",
	}, 2*time.Second)
	if badProbe.Online {
		t.Fatalf("expected mismatched CertPin probe to fail")
	}

	// 4. Auto-connect best node (should skip SV-Dead and connect to SV-Oracle with TLS)
	rm := NewRelayManager()
	defer rm.Stop()
	st, _, err := rm.StartBestNode(nodes, localJavaPort, localBedrockPort, true, true)
	if err != nil {
		t.Fatalf("StartBestNode failed: %v", err)
	}
	if !st.Running || st.NodeID != "SV-Oracle" || !st.Encrypted {
		t.Fatalf("unexpected TLS RelayState: %+v", st)
	}

	_, jPortStr, _ := net.SplitHostPort(st.JavaAddress)
	jPort, _ := strconv.Atoi(jPortStr)
	if jPort < 37100 || jPort > 37150 {
		t.Fatalf("expected Java port inside [37100,37150], got %d", jPort)
	}

	// 5. Verify encrypted TCP forwarding
	tc, err := net.DialTimeout("tcp", st.JavaAddress, 3*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer tc.Close()
	wantTCP := "tls-java-ok"
	_, _ = tc.Write([]byte(wantTCP))
	buf := make([]byte, len(wantTCP))
	_ = tc.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(tc, buf); err != nil {
		t.Fatalf("TLS TCP read failed: %v", err)
	}
	if string(buf) != wantTCP {
		t.Fatalf("TLS TCP echo mismatch: %q vs %q", string(buf), wantTCP)
	}

	// 6. Verify encrypted UDP forwarding
	uAddr, _ := net.ResolveUDPAddr("udp", st.BedrockAddress)
	uc, err := net.DialUDP("udp", nil, uAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer uc.Close()
	wantUDP := "tls-bedrock-ok"
	_, _ = uc.Write([]byte(wantUDP))
	uBuf := make([]byte, 256)
	_ = uc.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := uc.Read(uBuf)
	if err != nil {
		t.Fatalf("TLS UDP read failed: %v", err)
	}
	if string(uBuf[:n]) != wantUDP {
		t.Fatalf("TLS UDP echo mismatch: %q vs %q", string(uBuf[:n]), wantUDP)
	}
}
