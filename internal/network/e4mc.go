package network

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"github.com/quic-go/quic-go"
)

const defaultE4mcBrokerURL = "https://broker.e4mc.link/getBestRelay"

type e4mcRelayInfo struct {
	ID   string `json:"id"`
	Host string `json:"host"`
	Port int    `json:"port"`
}

type e4mcControlMsg struct {
	Kind    string   `json:"kind"`
	Domain  string   `json:"domain,omitempty"`
	Message string   `json:"message,omitempty"`
	Caps    []string `json:"caps,omitempty"`
}

// E4mcManager implements a pure-Go QUIClime tunnel client compatible with broker.e4mc.link.
// It requires zero external binaries or user accounts and automatically picks the nearest regional relay.
type E4mcManager struct {
	mu         sync.Mutex
	cancel     context.CancelFunc
	qConn      *quic.Conn
	ctrlStream *quic.Stream
	state      models.E4mcState
	stopSeq    uint64
}

func NewE4mcManager() *E4mcManager {
	return &E4mcManager{}
}

func (em *E4mcManager) Status() models.E4mcState {
	em.mu.Lock()
	defer em.mu.Unlock()
	return em.state
}

func (em *E4mcManager) Start(brokerURL string, localPort int) (models.E4mcState, error) {
	return em.startWithTLS(brokerURL, localPort, nil)
}

func (em *E4mcManager) startWithTLS(brokerURL string, localPort int, tlsConf *tls.Config) (models.E4mcState, error) {
	if localPort <= 0 || localPort > 65535 {
		return em.Status(), fmt.Errorf("gecersiz yerel port: %d", localPort)
	}
	brokerURL = strings.TrimSpace(brokerURL)
	if brokerURL == "" {
		brokerURL = defaultE4mcBrokerURL
	}

	_ = em.Stop()

	relay, err := fetchE4mcRelay(brokerURL)
	if err != nil {
		return em.failState(localPort, "", "", fmt.Errorf("e4mc bolge seciciye ulasilamadi: %w", err))
	}

	var tlsCfg *tls.Config
	if tlsConf != nil {
		tlsCfg = tlsConf.Clone()
		if tlsCfg.ServerName == "" {
			tlsCfg.ServerName = relay.Host
		}
		if len(tlsCfg.NextProtos) == 0 {
			tlsCfg.NextProtos = []string{"quiclime"}
		}
	} else {
		tlsCfg = &tls.Config{
			ServerName: relay.Host,
			NextProtos: []string{"quiclime"},
			MinVersion: tls.VersionTLS13,
		}
	}

	dialCtx, dialCancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer dialCancel()

	t0 := time.Now()
	relayAddr := net.JoinHostPort(relay.Host, strconv.Itoa(relay.Port))
	qConn, err := quic.DialAddr(dialCtx, relayAddr, tlsCfg, &quic.Config{
		MaxIncomingStreams:             512,
		MaxIdleTimeout:                 30 * time.Second,
		KeepAlivePeriod:                5 * time.Second,
		InitialStreamReceiveWindow:     1250000,
		MaxStreamReceiveWindow:         6 * 1024 * 1024,
		InitialConnectionReceiveWindow: 2 * 1024 * 1024,
		MaxConnectionReceiveWindow:     15 * 1024 * 1024,
	})
	if err != nil {
		return em.failState(localPort, relay.ID, relay.Host, fmt.Errorf("e4mc QUIC baglantisi kurulamadi (%s): %w", relayAddr, err))
	}
	latencyMs := time.Since(t0).Milliseconds()

	streamCtx, streamCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer streamCancel()
	ctrlStream, err := qConn.OpenStreamSync(streamCtx)
	if err != nil {
		_ = qConn.CloseWithError(0, "stream open failed")
		return em.failState(localPort, relay.ID, relay.Host, fmt.Errorf("e4mc kontrol akisi acilamadi: %w", err))
	}

	_ = ctrlStream.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := writeQuiclimeControl(ctrlStream, e4mcControlMsg{Kind: "request_domain_assignment"}); err != nil {
		_ = ctrlStream.Close()
		_ = qConn.CloseWithError(0, "write failed")
		return em.failState(localPort, relay.ID, relay.Host, err)
	}
	_ = ctrlStream.SetWriteDeadline(time.Time{})

	var domain, notice string
	_ = ctrlStream.SetReadDeadline(time.Now().Add(10 * time.Second))
	for domain == "" {
		raw, err := readQuiclimeControl(ctrlStream)
		if err != nil {
			_ = ctrlStream.Close()
			_ = qConn.CloseWithError(0, "handshake failed")
			return em.failState(localPort, relay.ID, relay.Host, fmt.Errorf("e4mc alan adi yaniti alinamadi: %w", err))
		}
		var msg e4mcControlMsg
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		switch msg.Kind {
		case "domain_assignment_complete":
			domain = strings.TrimSpace(msg.Domain)
		case "request_message_broadcast":
			if strings.TrimSpace(msg.Message) != "" {
				notice = strings.TrimSpace(msg.Message)
			}
		}
	}
	_ = ctrlStream.SetReadDeadline(time.Time{})

	loopCtx, cancel := context.WithCancel(context.Background())

	em.mu.Lock()
	em.stopSeq++
	seq := em.stopSeq
	em.cancel = cancel
	em.qConn = qConn
	em.ctrlStream = ctrlStream
	em.state = models.E4mcState{
		Running:   true,
		Region:    strings.ToUpper(strings.TrimSpace(relay.ID)),
		RelayHost: relay.Host,
		LocalPort: localPort,
		Domain:    domain,
		LatencyMs: latencyMs,
		Notice:    notice,
	}
	st := em.state
	em.mu.Unlock()

	go em.controlLoop(loopCtx, seq, ctrlStream, qConn)
	go em.acceptLoop(loopCtx, seq, qConn, localPort)
	return st, nil
}

func (em *E4mcManager) failState(localPort int, region, relayHost string, err error) (models.E4mcState, error) {
	em.mu.Lock()
	em.state = models.E4mcState{
		Running:   false,
		Region:    strings.ToUpper(strings.TrimSpace(region)),
		RelayHost: relayHost,
		LocalPort: localPort,
		LastError: err.Error(),
	}
	st := em.state
	em.mu.Unlock()
	return st, err
}

func (em *E4mcManager) Stop() error {
	em.mu.Lock()
	em.stopSeq++
	cancel := em.cancel
	ctrl := em.ctrlStream
	qc := em.qConn
	em.cancel = nil
	em.ctrlStream = nil
	em.qConn = nil
	em.state.Running = false
	em.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if ctrl != nil {
		_ = ctrl.Close()
	}
	if qc != nil {
		_ = qc.CloseWithError(0, "stopped")
	}
	return nil
}

func (em *E4mcManager) controlLoop(ctx context.Context, seq uint64, ctrl *quic.Stream, qc *quic.Conn) {
	for {
		raw, err := readQuiclimeControl(ctrl)
		if err != nil {
			em.mu.Lock()
			if em.stopSeq == seq && ctx.Err() == nil {
				em.state.Running = false
				if err != io.EOF {
					em.state.LastError = err.Error()
				}
			}
			em.mu.Unlock()
			_ = qc.CloseWithError(0, "control stream closed")
			return
		}
		var msg e4mcControlMsg
		if json.Unmarshal(raw, &msg) == nil && msg.Kind == "request_message_broadcast" && strings.TrimSpace(msg.Message) != "" {
			em.mu.Lock()
			if em.stopSeq == seq {
				em.state.Notice = strings.TrimSpace(msg.Message)
			}
			em.mu.Unlock()
		}
	}
}

func (em *E4mcManager) acceptLoop(ctx context.Context, seq uint64, qc *quic.Conn, localPort int) {
	for {
		stream, err := qc.AcceptStream(ctx)
		if err != nil {
			em.mu.Lock()
			if em.stopSeq == seq && ctx.Err() == nil {
				em.state.Running = false
				if em.state.LastError == "" {
					em.state.LastError = err.Error()
				}
			}
			em.mu.Unlock()
			return
		}
		go handleE4mcStream(stream, localPort)
	}
}

func handleE4mcStream(stream *quic.Stream, localPort int) {
	localConn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)), 5*time.Second)
	if err != nil {
		stream.CancelRead(0)
		stream.CancelWrite(0)
		_ = stream.Close()
		return
	}
	tuneTCPConn(localConn)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		bufPtr := copyBufPool.Get().(*[]byte)
		_, _ = io.CopyBuffer(localConn, stream, *bufPtr)
		copyBufPool.Put(bufPtr)
		if tc, ok := localConn.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		} else {
			_ = localConn.Close()
		}
	}()
	go func() {
		defer wg.Done()
		bufPtr := copyBufPool.Get().(*[]byte)
		_, _ = io.CopyBuffer(stream, localConn, *bufPtr)
		copyBufPool.Put(bufPtr)
		_ = stream.Close()
	}()
	wg.Wait()
	_ = localConn.Close()
	_ = stream.Close()
}

func fetchE4mcRelay(brokerURL string) (e4mcRelayInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, brokerURL, nil)
	if err != nil {
		return e4mcRelayInfo{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MC-SekaiLink")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return e4mcRelayInfo{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return e4mcRelayInfo{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var info e4mcRelayInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&info); err != nil {
		return e4mcRelayInfo{}, err
	}
	if strings.TrimSpace(info.Host) == "" || info.Port <= 0 {
		return e4mcRelayInfo{}, fmt.Errorf("gecersiz e4mc relay yaniti")
	}
	return info, nil
}

func writeQuiclimeControl(w io.Writer, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return err
	}
	frame := binary.AppendUvarint(nil, uint64(len(payload)))
	frame = append(frame, payload...)
	_, err = w.Write(frame)
	return err
}

func readQuiclimeControl(r io.Reader) ([]byte, error) {
	var size uint64
	var shift uint
	var b [1]byte
	for i := 0; i < 10; i++ {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		if b[0] < 0x80 {
			size |= uint64(b[0]) << shift
			break
		}
		size |= uint64(b[0]&0x7F) << shift
		shift += 7
		if i == 9 {
			return nil, fmt.Errorf("varint overflow")
		}
	}
	if size == 0 {
		return nil, io.EOF
	}
	if size > 64*1024 {
		return nil, fmt.Errorf("control message too large: %d", size)
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
