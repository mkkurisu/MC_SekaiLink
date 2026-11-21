package network

import (
	"io"
	"net"
	"testing"
	"time"
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
