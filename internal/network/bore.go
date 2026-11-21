package network

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

const defaultBoreControlPort = "7835"

// BoreManager implements a pure-Go ekzhang/bore TCP tunnel client requiring zero external binaries or accounts.
type BoreManager struct {
	mu       sync.Mutex
	ctrlConn net.Conn
	state    models.BoreState
	stopSeq  uint64
}

func NewBoreManager() *BoreManager {
	return &BoreManager{}
}

func (bm *BoreManager) Status() models.BoreState {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return bm.state
}

type boreServerEnvelope struct {
	Hello      *int    `json:"Hello,omitempty"`
	Challenge  *string `json:"Challenge,omitempty"`
	Connection *string `json:"Connection,omitempty"`
	Error      *string `json:"Error,omitempty"`
}

func (bm *BoreManager) Start(serverHost string, localPort int, secret string) (models.BoreState, error) {
	if localPort <= 0 || localPort > 65535 {
		return bm.Status(), fmt.Errorf("gecersiz yerel port: %d", localPort)
	}
	serverHost = strings.TrimSpace(serverHost)
	if serverHost == "" {
		serverHost = "bore.pub"
	}

	dialAddr := serverHost
	displayHost := serverHost
	if host, _, err := net.SplitHostPort(serverHost); err == nil {
		displayHost = host
	} else {
		dialAddr = net.JoinHostPort(serverHost, defaultBoreControlPort)
	}

	_ = bm.Stop()

	conn, err := net.DialTimeout("tcp", dialAddr, 5*time.Second)
	if err != nil {
		bm.mu.Lock()
		bm.state = models.BoreState{
			ServerHost: displayHost,
			LocalPort:  localPort,
			LastError:  err.Error(),
		}
		bm.mu.Unlock()
		return bm.Status(), fmt.Errorf("bore sunucusuna baglanilamadi (%s): %w", dialAddr, err)
	}

	// Send optional auth or initial Hello
	if secret != "" {
		// Wait for Challenge first when secret is provided
		frame, err := readNullFrameRaw(conn, 5*time.Second)
		if err != nil {
			conn.Close()
			return bm.failState(displayHost, localPort, err)
		}
		var env boreServerEnvelope
		if err := json.Unmarshal(frame, &env); err != nil {
			conn.Close()
			return bm.failState(displayHost, localPort, err)
		}
		if env.Error != nil {
			conn.Close()
			return bm.failState(displayHost, localPort, fmt.Errorf("%s", *env.Error))
		}
		if env.Challenge != nil {
			if err := sendBoreAuth(conn, secret, *env.Challenge); err != nil {
				conn.Close()
				return bm.failState(displayHost, localPort, err)
			}
		}
	}

	if err := writeNullFrame(conn, map[string]int{"Hello": 0}); err != nil {
		conn.Close()
		return bm.failState(displayHost, localPort, err)
	}

	var remotePort int
	for {
		frame, err := readNullFrameRaw(conn, 5*time.Second)
		if err != nil {
			conn.Close()
			return bm.failState(displayHost, localPort, err)
		}
		trimmed := strings.TrimSpace(string(frame))
		if trimmed == `"Heartbeat"` {
			continue
		}
		var env boreServerEnvelope
		if err := json.Unmarshal(frame, &env); err != nil {
			conn.Close()
			return bm.failState(displayHost, localPort, err)
		}
		if env.Error != nil {
			conn.Close()
			return bm.failState(displayHost, localPort, fmt.Errorf("bore sunucu hatasi: %s", *env.Error))
		}
		if env.Challenge != nil {
			if secret == "" {
				conn.Close()
				return bm.failState(displayHost, localPort, fmt.Errorf("bore sunucusu gizli anahtar gerektiriyor"))
			}
			if err := sendBoreAuth(conn, secret, *env.Challenge); err != nil {
				conn.Close()
				return bm.failState(displayHost, localPort, err)
			}
			continue
		}
		if env.Hello != nil {
			remotePort = *env.Hello
			break
		}
	}

	bm.mu.Lock()
	bm.stopSeq++
	seq := bm.stopSeq
	bm.ctrlConn = conn
	bm.state = models.BoreState{
		Running:    true,
		ServerHost: displayHost,
		LocalPort:  localPort,
		RemotePort: remotePort,
		Address:    net.JoinHostPort(displayHost, strconv.Itoa(remotePort)),
	}
	st := bm.state
	bm.mu.Unlock()

	go bm.controlLoop(conn, seq, dialAddr, localPort, secret)
	return st, nil
}

func (bm *BoreManager) failState(host string, localPort int, err error) (models.BoreState, error) {
	bm.mu.Lock()
	bm.state = models.BoreState{
		Running:    false,
		ServerHost: host,
		LocalPort:  localPort,
		LastError:  err.Error(),
	}
	st := bm.state
	bm.mu.Unlock()
	return st, err
}

func (bm *BoreManager) Stop() error {
	bm.mu.Lock()
	bm.stopSeq++
	conn := bm.ctrlConn
	bm.ctrlConn = nil
	bm.state.Running = false
	bm.mu.Unlock()

	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (bm *BoreManager) controlLoop(conn net.Conn, seq uint64, dialAddr string, localPort int, secret string) {
	defer conn.Close()
	for {
		frame, err := readNullFrameRaw(conn, 0)
		if err != nil {
			bm.mu.Lock()
			if bm.stopSeq == seq {
				bm.ctrlConn = nil
				bm.state.Running = false
				if err != io.EOF {
					bm.state.LastError = err.Error()
				}
			}
			bm.mu.Unlock()
			return
		}
		trimmed := strings.TrimSpace(string(frame))
		if trimmed == `"Heartbeat"` || trimmed == "" {
			continue
		}
		var env boreServerEnvelope
		if err := json.Unmarshal(frame, &env); err != nil {
			continue
		}
		if env.Error != nil {
			bm.mu.Lock()
			if bm.stopSeq == seq {
				bm.ctrlConn = nil
				bm.state.Running = false
				bm.state.LastError = *env.Error
			}
			bm.mu.Unlock()
			return
		}
		if env.Connection != nil {
			connID := *env.Connection
			go handleBoreForward(dialAddr, localPort, secret, connID)
		}
	}
}

func handleBoreForward(dialAddr string, localPort int, secret, connID string) {
	remoteConn, err := net.DialTimeout("tcp", dialAddr, 5*time.Second)
	if err != nil {
		return
	}

	if secret != "" {
		frame, err := readNullFrameRaw(remoteConn, 5*time.Second)
		if err != nil {
			remoteConn.Close()
			return
		}
		var env boreServerEnvelope
		if err := json.Unmarshal(frame, &env); err != nil || env.Challenge == nil {
			remoteConn.Close()
			return
		}
		if err := sendBoreAuth(remoteConn, secret, *env.Challenge); err != nil {
			remoteConn.Close()
			return
		}
	}

	if err := writeNullFrame(remoteConn, map[string]string{"Accept": connID}); err != nil {
		remoteConn.Close()
		return
	}

	localConn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)), 5*time.Second)
	if err != nil {
		remoteConn.Close()
		return
	}

	pipeBidirectional(localConn, remoteConn)
}

func sendBoreAuth(conn net.Conn, secret, challengeUUID string) error {
	keyHash := sha256.Sum256([]byte(secret))
	mac := hmac.New(sha256.New, keyHash[:])
	mac.Write([]byte(challengeUUID))
	sig := hex.EncodeToString(mac.Sum(nil))
	return writeNullFrame(conn, map[string]string{"Authenticate": sig})
}

func writeNullFrame(conn net.Conn, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	payload = append(payload, 0)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetWriteDeadline(time.Time{})
	_, err = conn.Write(payload)
	return err
}

// readNullFrameRaw reads exactly up to the next \x00 delimiter without buffering ahead,
// keeping the socket clean for raw bidirectional proxying immediately afterward.
func readNullFrameRaw(conn net.Conn, timeout time.Duration) ([]byte, error) {
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		defer conn.SetReadDeadline(time.Time{})
	} else {
		_ = conn.SetReadDeadline(time.Time{})
	}
	var buf []byte
	b := make([]byte, 1)
	for len(buf) < 512 {
		n, err := conn.Read(b)
		if n > 0 {
			if b[0] == 0 {
				return buf, nil
			}
			buf = append(buf, b[0])
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("bore frame too long")
}

func pipeBidirectional(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	copySide := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}
	go copySide(a, b)
	go copySide(b, a)
	wg.Wait()
	_ = a.Close()
	_ = b.Close()
}
