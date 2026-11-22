package network

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestPinggyManagerMockSSHTunnel(t *testing.T) {
	// 1. Local mock Minecraft echo server
	mcLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer mcLn.Close()
	localPort := mcLn.Addr().(*net.TCPAddr).Port

	go func() {
		for {
			c, err := mcLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}(c)
		}
	}()

	// 2. Mock Pinggy SSH server
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	srvCfg := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, _ []byte) (*ssh.Permissions, error) {
			return nil, nil
		},
	}
	srvCfg.AddHostKey(signer)

	sshLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sshLn.Close()

	triggerForward := make(chan struct{})
	forwardedDone := make(chan string, 1)
	forwardedErr := make(chan error, 1)

	go func() {
		raw, err := sshLn.Accept()
		if err != nil {
			forwardedErr <- err
			return
		}
		sConn, chans, reqs, err := ssh.NewServerConn(raw, srvCfg)
		if err != nil {
			forwardedErr <- err
			return
		}
		defer sConn.Close()

		go func() {
			for newCh := range chans {
				if newCh.ChannelType() != "direct-tcpip" {
					_ = newCh.Reject(ssh.UnknownChannelType, "unsupported")
					continue
				}
				var payload struct {
					DestAddr   string
					DestPort   uint32
					OriginAddr string
					OriginPort uint32
				}
				_ = ssh.Unmarshal(newCh.ExtraData(), &payload)
				ch, requests, err := newCh.Accept()
				if err != nil {
					continue
				}
				go ssh.DiscardRequests(requests)
				go func(target string, port uint32, c ssh.Channel) {
					defer c.Close()
					switch fmt.Sprintf("%s:%d", target, port) {
					case "primaryHost:4":
						_, _ = c.Write([]byte(`{"StatusPort":12,"UrlTcp":4300}`))
						_ = c.CloseWrite()
					case "localhost:12":
						_, _ = c.Write([]byte(`{"Success":true,"Authenticated":false}`))
						_ = c.CloseWrite()
					case "localhost:4300":
						_, _ = http.ReadRequest(bufio.NewReader(c))
						body := `{"urls":["tcp://mock.a.pinggy.link:41234"]}`
						_, _ = fmt.Fprintf(c, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
						_ = c.CloseWrite()
					}
				}(payload.DestAddr, payload.DestPort, ch)
			}
		}()

		forwardActive := false
		for req := range reqs {
			if req.Type == "tcpip-forward" {
				var p struct {
					BindAddr string
					BindPort uint32
				}
				_ = ssh.Unmarshal(req.Payload, &p)
				var reply [4]byte
				reply[0], reply[1], reply[2], reply[3] = 0, 0, 0xA1, 0x12 // 41234
				_ = req.Reply(true, reply[:])
				if !forwardActive {
					forwardActive = true
					go func(bindAddr string) {
						select {
						case <-triggerForward:
						case <-time.After(5 * time.Second):
							forwardedErr <- fmt.Errorf("timeout waiting to trigger forwarded-tcpip")
							return
						}
						fwdPayload := ssh.Marshal(struct {
							Addr       string
							Port       uint32
							OriginAddr string
							OriginPort uint32
						}{
							Addr:       bindAddr,
							Port:       41234,
							OriginAddr: "127.0.0.1",
							OriginPort: 50000,
						})
						ch, reqs2, err := sConn.OpenChannel("forwarded-tcpip", fwdPayload)
						if err != nil {
							forwardedErr <- err
							return
						}
						go ssh.DiscardRequests(reqs2)
						defer ch.Close()
						_, _ = ch.Write([]byte("pinggy-hello"))
						_ = ch.CloseWrite()
						got, rErr := io.ReadAll(ch)
						if rErr != nil {
							forwardedErr <- rErr
							return
						}
						forwardedDone <- string(got)
					}(p.BindAddr)
				}
			} else if req.WantReply {
				_ = req.Reply(true, nil)
			}
		}
	}()

	pm := NewPinggyManager()
	st, err := pm.startWithCandidates([]pinggyEndpoint{{Region: "EU", Host: sshLn.Addr().String()}}, "", localPort)
	if err != nil {
		t.Fatalf("PinggyManager.startWithCandidates failed: %v", err)
	}
	defer pm.Stop()

	if !st.Running || st.Address != "mock.a.pinggy.link:41234" || st.Region != "EU" || st.ExpiresAt <= st.StartedAt {
		t.Fatalf("unexpected PinggyState: %+v", st)
	}

	// Only open forwarded-tcpip AFTER startWithCandidates has registered its acceptLoop!
	close(triggerForward)

	select {
	case got := <-forwardedDone:
		if got != "pinggy-hello" {
			t.Fatalf("expected echoed payload 'pinggy-hello', got %q", got)
		}
	case fErr := <-forwardedErr:
		t.Fatalf("forwarded stream error: %v", fErr)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Pinggy forwarded payload")
	}

	if err := pm.Stop(); err != nil {
		t.Fatal(err)
	}
	if pm.Status().Running {
		t.Fatal("expected PinggyManager to be stopped")
	}
}

func TestNormalizePinggyPublicAddr(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"tcp://zhtmo-178-241-135-60.run.pinggy-free.link:41407", "zhtmo-178-241-135-60.run.pinggy-free.link:41407"},
		{"mock.a.pinggy.link:41234/", "mock.a.pinggy.link:41234"},
		{"http://foo.a.pinggy.link:80", ""},
		{"https://foo.a.pinggy.link", ""},
		{"tcp://bad.link:0", ""},
		{"tcp://bad.link/path", ""},
	}
	for _, tc := range cases {
		if got := normalizePinggyPublicAddr(tc.in); got != tc.want {
			t.Fatalf("normalizePinggyPublicAddr(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
