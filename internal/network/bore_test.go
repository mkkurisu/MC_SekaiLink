package network

import (
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

func TestBoreManagerForwarding(t *testing.T) {
	// 1. Local TCP echo service simulating Minecraft Java server
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

	// 2. Mock ekzhang/bore server (control + public listener)
	ctrlLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ctrlLn.Close()

	pubLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pubLn.Close()
	pubPort := pubLn.Addr().(*net.TCPAddr).Port

	pendingAccept := make(chan net.Conn, 1)
	go func() {
		for {
			c, err := ctrlLn.Accept()
			if err != nil {
				return
			}
			frame, err := readNullFrameRaw(c, 3*time.Second)
			if err != nil {
				c.Close()
				continue
			}
			var msg map[string]any
			_ = json.Unmarshal(frame, &msg)
			if _, isHello := msg["Hello"]; isHello {
				_ = writeNullFrame(c, map[string]int{"Hello": pubPort})
				go func(ctrl net.Conn) {
					player, err := pubLn.Accept()
					if err != nil {
						return
					}
					_ = writeNullFrame(ctrl, map[string]string{"Connection": "conn-123"})
					dataConn := <-pendingAccept
					pipeBidirectional(player, dataConn)
				}(c)
			} else if _, isAccept := msg["Accept"]; isAccept {
				pendingAccept <- c
			}
		}
	}()

	// 3. Connect BoreManager and round-trip bytes through public port
	bm := NewBoreManager()
	defer bm.Stop()

	st, err := bm.Start(ctrlLn.Addr().String(), localPort, "")
	if err != nil {
		t.Fatalf("BoreManager.Start failed: %v", err)
	}
	if !st.Running || st.RemotePort != pubPort {
		t.Fatalf("unexpected BoreState: %+v", st)
	}

	clientConn, err := net.DialTimeout("tcp", st.Address, 3*time.Second)
	if err != nil {
		t.Fatalf("dial public bore address failed: %v", err)
	}
	defer clientConn.Close()

	want := "hello-mc-sekailink"
	if _, err := clientConn.Write([]byte(want)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(want))
	_ = clientConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(clientConn, buf); err != nil {
		t.Fatalf("read echoed bytes failed: %v", err)
	}
	if string(buf) != want {
		t.Fatalf("echo mismatch: got %q, want %q", string(buf), want)
	}
}
