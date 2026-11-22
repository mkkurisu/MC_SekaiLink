package network

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

type inMemorySignalBus struct {
	mu   sync.Mutex
	subs map[string][]chan string
}

func newInMemorySignalBus() *inMemorySignalBus {
	return &inMemorySignalBus{
		subs: make(map[string][]chan string),
	}
}

func (b *inMemorySignalBus) Status() string {
	return "in-memory"
}

func (b *inMemorySignalBus) Subscribe(ctx context.Context, topicHash string) (<-chan string, error) {
	ch := make(chan string, 32)
	b.mu.Lock()
	b.subs[topicHash] = append(b.subs[topicHash], ch)
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		list := b.subs[topicHash]
		kept := list[:0]
		for _, c := range list {
			if c != ch {
				kept = append(kept, c)
			}
		}
		b.subs[topicHash] = kept
		b.mu.Unlock()
		close(ch)
	}()
	return ch, nil
}

func (b *inMemorySignalBus) Publish(_ context.Context, topicHash string, payload string) error {
	b.mu.Lock()
	targets := append([]chan string{}, b.subs[topicHash]...)
	b.mu.Unlock()
	for _, ch := range targets {
		select {
		case ch <- payload:
		default:
		}
	}
	return nil
}

func TestP2PRoomCodeAndEncryption(t *testing.T) {
	code := GenerateRoomCode()
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("unexpected generated room code: %q", code)
	}
	norm, err := NormalizeRoomCode("  " + strings.ToLower(strings.ReplaceAll(code, "-", "")) + " ")
	if err != nil || norm != code {
		t.Fatalf("NormalizeRoomCode failed: got=%q err=%v want=%q", norm, err, code)
	}
	if _, err := NormalizeRoomCode("SHORT"); err == nil {
		t.Fatal("expected invalid length error")
	}
	if _, err := NormalizeRoomCode("ABCD-123O"); err == nil {
		t.Fatal("expected ambiguous char error for 'O'")
	}

	_, aes1, _ := deriveRoomKeys(norm)
	_, aes2, _ := deriveRoomKeys("WXYZ-2345")

	enc, err := encryptSignal(aes1, []byte(`{"secret":"1.2.3.4:5555"}`))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := decryptSignal(aes1, enc)
	if err != nil || string(dec) != `{"secret":"1.2.3.4:5555"}` {
		t.Fatalf("decrypt failed: %q %v", string(dec), err)
	}
	if _, err := decryptSignal(aes2, enc); err == nil {
		t.Fatal("expected decryption with wrong room key to fail")
	}
}

func TestSTUNNATDetectionMock(t *testing.T) {
	startMockSTUN := func(fakeIP [4]byte, fakePort uint16) (*net.UDPConn, string) {
		pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			buf := make([]byte, 512)
			for {
				n, rAddr, err := pc.ReadFromUDP(buf)
				if err != nil {
					return
				}
				if n < 20 || buf[0] != 0x00 || buf[1] != 0x01 {
					continue
				}
				resp := make([]byte, 32)
				resp[0], resp[1] = 0x01, 0x01
				binary.BigEndian.PutUint16(resp[2:4], 12)
				copy(resp[4:8], []byte{0x21, 0x12, 0xA4, 0x42})
				copy(resp[8:20], buf[8:20])
				binary.BigEndian.PutUint16(resp[20:22], 0x0020)
				binary.BigEndian.PutUint16(resp[22:24], 8)
				resp[24], resp[25] = 0x00, 0x01
				binary.BigEndian.PutUint16(resp[26:28], fakePort^0x2112)
				resp[28] = fakeIP[0] ^ 0x21
				resp[29] = fakeIP[1] ^ 0x12
				resp[30] = fakeIP[2] ^ 0xA4
				resp[31] = fakeIP[3] ^ 0x42
				_, _ = pc.WriteToUDP(resp, rAddr)
			}
		}()
		return pc, pc.LocalAddr().String()
	}

	s1, addr1 := startMockSTUN([4]byte{198, 51, 100, 10}, 45000)
	defer s1.Close()
	s2, addr2 := startMockSTUN([4]byte{198, 51, 100, 10}, 45000)
	defer s2.Close()
	s3, addr3 := startMockSTUN([4]byte{198, 51, 100, 10}, 45004)
	defer s3.Close()

	var key [32]byte
	uc, tr, mux, err := startP2PMux(key)
	if err != nil {
		t.Fatal(err)
	}
	defer uc.Close()
	defer tr.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go mux.run(ctx)

	cone := mux.discoverSTUN(ctx, []string{addr1, addr2})
	if cone.NATType != "cone" || cone.PublicEndpoint != "198.51.100.10:45000" {
		t.Fatalf("expected cone NAT 198.51.100.10:45000, got %+v", cone)
	}

	sym := mux.discoverSTUN(ctx, []string{addr1, addr3})
	if sym.NATType != "symmetric" || sym.PortDelta != 4 {
		t.Fatalf("expected symmetric NAT delta 4, got %+v", sym)
	}
}

func TestP2PEndToEndHostAndJoinJavaTCPAndBedrockUDP(t *testing.T) {
	// 1. Mock local Java TCP echo server
	javaLn, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer javaLn.Close()
	javaPort := javaLn.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			c, err := javaLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(c)
		}
	}()

	// 2. Mock local Bedrock UDP echo server
	bedrockConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer bedrockConn.Close()
	bedrockPort := bedrockConn.LocalAddr().(*net.UDPAddr).Port

	go func() {
		buf := make([]byte, 2048)
		for {
			n, rAddr, err := bedrockConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			reply := append([]byte("raknet:"), buf[:n]...)
			_, _ = bedrockConn.WriteToUDP(reply, rAddr)
		}
	}()

	bus := newInMemorySignalBus()
	hostMgr := NewP2PManager()
	joinMgr := NewP2PManager()
	defer hostMgr.Stop()
	defer joinMgr.Stop()

	hostState, err := hostMgr.StartHost(P2PHostOptions{
		LocalJavaPort:    javaPort,
		LocalBedrockPort: bedrockPort,
		WantJava:         true,
		WantBedrock:      true,
		bus:              bus,
		disableSTUN:      true,
	})
	if err != nil {
		t.Fatalf("StartHost failed: %v", err)
	}
	if !hostState.Running || hostState.RoomCode == "" || hostState.Mode != "host" {
		t.Fatalf("unexpected host state: %+v", hostState)
	}

	// Pick occupied ports or explicit high preferred ports so we also confirm clean binding
	joinState, err := joinMgr.StartJoin(P2PJoinOptions{
		RoomCode:             hostState.RoomCode,
		PeerName:             "LinuxTester",
		PreferredJavaPort:    0, // triggers 25565 or fallback
		PreferredBedrockPort: 0, // triggers 19132 or fallback
		bus:                  bus,
		disableSTUN:          true,
	})
	if err != nil {
		t.Fatalf("StartJoin failed: %v", err)
	}
	if !joinState.Running || joinState.Phase != "connected" || joinState.LocalJavaAddr == "" || joinState.LocalBedrockAddr == "" {
		t.Fatalf("unexpected join state: %+v", joinState)
	}

	// Verify Java TCP stream forwarding end-to-end
	jc, err := net.DialTimeout("tcp4", joinState.LocalJavaAddr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial local java proxy failed: %v", err)
	}
	_ = jc.SetDeadline(time.Now().Add(5 * time.Second))
	msg := []byte("minecraft-java-handshake-test")
	if _, err := jc.Write(msg); err != nil {
		t.Fatal(err)
	}
	if tc, ok := jc.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
	gotJava, err := io.ReadAll(jc)
	_ = jc.Close()
	if err != nil || !bytes.Equal(gotJava, msg) {
		t.Fatalf("Java P2P roundtrip mismatch: got=%q want=%q err=%v", string(gotJava), string(msg), err)
	}

	// Verify Bedrock UDP datagram forwarding end-to-end
	bc, err := net.Dial("udp4", joinState.LocalBedrockAddr)
	if err != nil {
		t.Fatalf("dial local bedrock udp proxy failed: %v", err)
	}
	if _, err := bc.Write([]byte("unconnected-ping")); err != nil {
		t.Fatal(err)
	}
	_ = bc.SetReadDeadline(time.Now().Add(3 * time.Second))
	udpBuf := make([]byte, 512)
	n, err := bc.Read(udpBuf)
	_ = bc.Close()
	if err != nil || string(udpBuf[:n]) != "raknet:unconnected-ping" {
		t.Fatalf("Bedrock P2P UDP roundtrip mismatch: got=%q err=%v", string(udpBuf[:n]), err)
	}

	// Wait briefly for control stream peer info update on host
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hs := hostMgr.Status()
		if hs.PeerCount == 1 && len(hs.Peers) == 1 && hs.Peers[0].Name == "LinuxTester" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	finalHost := hostMgr.Status()
	if finalHost.PeerCount != 1 {
		t.Fatalf("expected PeerCount == 1 on host, got %+v", finalHost)
	}

	_ = joinMgr.Stop()
	_ = hostMgr.Stop()
	if joinMgr.Status().Running || hostMgr.Status().Running {
		t.Fatal("expected both P2P managers to stop cleanly")
	}
}
