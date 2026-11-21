package network

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

func TestQuiclimeControlVarIntFraming(t *testing.T) {
	longText := strings.Repeat("sekailink-", 25) // > 127 bytes to exercise multi-byte VarInt
	var buf bytes.Buffer
	if err := writeQuiclimeControl(&buf, e4mcControlMsg{
		Kind:    "request_message_broadcast",
		Message: longText,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := readQuiclimeControl(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var got e4mcControlMsg
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "request_message_broadcast" || got.Message != longText {
		t.Fatalf("unexpected decoded control message: %+v", got)
	}
}

func generateTestQUICTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"localhost", "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{der},
			PrivateKey:  key,
		}},
		NextProtos: []string{"quiclime"},
	}
}

func TestE4mcManagerQUICForwarding(t *testing.T) {
	// 1. Local TCP echo server simulating local Minecraft Java server
	localLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer localLn.Close()
	go func() {
		for {
			c, err := localLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(c)
		}
	}()
	localPort := localLn.Addr().(*net.TCPAddr).Port

	// 2. Mock QUIClime relay + public TCP listener
	pubLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pubLn.Close()

	srvTLS := generateTestQUICTLSConfig(t)
	ql, err := quic.ListenAddr("127.0.0.1:0", srvTLS, &quic.Config{
		MaxIncomingStreams: 64,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ql.Close()

	_, relayPortStr, _ := net.SplitHostPort(ql.Addr().String())
	relayPort, _ := strconv.Atoi(relayPortStr)

	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(e4mcRelayInfo{
			ID:   "eu",
			Host: "127.0.0.1",
			Port: relayPort,
		})
	}))
	defer broker.Close()

	go func() {
		qConn, err := ql.Accept(context.Background())
		if err != nil {
			return
		}
		ctrl, err := qConn.AcceptStream(context.Background())
		if err != nil {
			return
		}
		raw, err := readQuiclimeControl(ctrl)
		if err != nil {
			return
		}
		var req e4mcControlMsg
		_ = json.Unmarshal(raw, &req)
		if req.Kind != "request_domain_assignment" {
			return
		}
		_ = writeQuiclimeControl(ctrl, e4mcControlMsg{
			Kind:   "domain_assignment_complete",
			Domain: "test-eu.e4mc.link",
		})

		for {
			playerConn, err := pubLn.Accept()
			if err != nil {
				return
			}
			go func(pc net.Conn) {
				defer pc.Close()
				qs, err := qConn.OpenStreamSync(context.Background())
				if err != nil {
					return
				}
				var wg sync.WaitGroup
				wg.Add(2)
				go func() {
					defer wg.Done()
					_, _ = io.Copy(qs, pc)
					_ = qs.Close()
				}()
				go func() {
					defer wg.Done()
					_, _ = io.Copy(pc, qs)
					if tc, ok := pc.(*net.TCPConn); ok {
						_ = tc.CloseWrite()
					}
				}()
				wg.Wait()
			}(playerConn)
		}
	}()

	// 3. Connect E4mcManager and round-trip traffic through the QUIC tunnel
	em := NewE4mcManager()
	defer em.Stop()

	st, err := em.startWithTLS(broker.URL, localPort, &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quiclime"},
	})
	if err != nil {
		t.Fatalf("E4mcManager.startWithTLS failed: %v", err)
	}
	if !st.Running || st.Domain != "test-eu.e4mc.link" || st.Region != "EU" {
		t.Fatalf("unexpected E4mcState: %+v", st)
	}

	clientConn, err := net.DialTimeout("tcp", pubLn.Addr().String(), 3*time.Second)
	if err != nil {
		t.Fatalf("dial mock public endpoint failed: %v", err)
	}
	defer clientConn.Close()

	want := "hello-e4mc-quiclime"
	if _, err := clientConn.Write([]byte(want)); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	_ = clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(clientConn, got); err != nil {
		t.Fatalf("read echoed bytes via QUIC failed: %v", err)
	}
	if string(got) != want {
		t.Fatalf("QUIC echo mismatch: got %q, want %q", string(got), want)
	}
}
