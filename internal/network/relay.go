package network

import (
	"crypto/rand"
	"encoding/binary"
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

const defaultRelayPort = "25590"

type relayControlMsg struct {
	Type        string `json:"type"`
	Secret      string `json:"secret,omitempty"`
	WantJava    bool   `json:"wantJava,omitempty"`
	WantBedrock bool   `json:"wantBedrock,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	PublicHost  string `json:"publicHost,omitempty"`
	JavaPort    int    `json:"javaPort,omitempty"`
	BedrockPort int    `json:"bedrockPort,omitempty"`
	ConnID      string `json:"connId,omitempty"`
	Error       string `json:"error,omitempty"`
}

// --- RelayServer (Open-Source Self-Hostable TCP + UDP Relay) ---

type relaySession struct {
	id          string
	ctrlConn    net.Conn
	ctrlMu      sync.Mutex
	tcpListener net.Listener
	udpConn     *net.UDPConn
	udpStream   net.Conn
	udpWriteMu  sync.Mutex

	pendingMu  sync.Mutex
	pendingTCP map[string]net.Conn
	closeOnce  sync.Once
}

func (rs *relaySession) sendControl(msg relayControlMsg) error {
	rs.ctrlMu.Lock()
	defer rs.ctrlMu.Unlock()
	return writeLineJSON(rs.ctrlConn, msg)
}

func (rs *relaySession) close(srv *RelayServer) {
	rs.closeOnce.Do(func() {
		if rs.tcpListener != nil {
			_ = rs.tcpListener.Close()
		}
		if rs.udpConn != nil {
			_ = rs.udpConn.Close()
		}
		if rs.udpStream != nil {
			_ = rs.udpStream.Close()
		}
		if rs.ctrlConn != nil {
			_ = rs.ctrlConn.Close()
		}
		rs.pendingMu.Lock()
		for _, c := range rs.pendingTCP {
			_ = c.Close()
		}
		rs.pendingTCP = nil
		rs.pendingMu.Unlock()

		srv.mu.Lock()
		delete(srv.sessions, rs.id)
		srv.mu.Unlock()
	})
}

type RelayServer struct {
	publicHost string
	secret     string

	mu       sync.Mutex
	ln       net.Listener
	sessions map[string]*relaySession
}

func NewRelayServer(publicHost, secret string) *RelayServer {
	return &RelayServer{
		publicHost: strings.TrimSpace(publicHost),
		secret:     secret,
		sessions:   make(map[string]*relaySession),
	}
}

func (s *RelayServer) ListenAndServe(listenAddr string) error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

func (s *RelayServer) Serve(ln net.Listener) error {
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn)
	}
}

func (s *RelayServer) Close() error {
	s.mu.Lock()
	ln := s.ln
	sessions := make([]*relaySession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	s.mu.Unlock()
	for _, sess := range sessions {
		sess.close(s)
	}
	if ln != nil {
		return ln.Close()
	}
	return nil
}

func (s *RelayServer) handleConn(conn net.Conn) {
	line, err := readLineRaw(conn, 10*time.Second)
	if err != nil {
		conn.Close()
		return
	}
	var msg relayControlMsg
	if err := json.Unmarshal(line, &msg); err != nil {
		conn.Close()
		return
	}

	switch msg.Type {
	case "hello":
		s.handleHello(conn, msg)
	case "tcp_accept":
		s.handleTCPAccept(conn, msg)
	case "udp_attach":
		s.handleUDPAttach(conn, msg)
	default:
		conn.Close()
	}
}

func (s *RelayServer) handleHello(conn net.Conn, hello relayControlMsg) {
	if s.secret != "" && hello.Secret != s.secret {
		_ = writeLineJSON(conn, relayControlMsg{Type: "error", Error: "gecersiz relay gizli anahtari"})
		conn.Close()
		return
	}

	sess := &relaySession{
		id:         randomHexID(8),
		ctrlConn:   conn,
		pendingTCP: make(map[string]net.Conn),
	}

	var javaPort, bedrockPort int
	if hello.WantJava {
		ln, err := net.Listen("tcp", ":0")
		if err != nil {
			_ = writeLineJSON(conn, relayControlMsg{Type: "error", Error: err.Error()})
			conn.Close()
			return
		}
		sess.tcpListener = ln
		javaPort = ln.Addr().(*net.TCPAddr).Port
	}
	if hello.WantBedrock {
		uc, err := net.ListenUDP("udp", &net.UDPAddr{Port: 0})
		if err != nil {
			if sess.tcpListener != nil {
				_ = sess.tcpListener.Close()
			}
			_ = writeLineJSON(conn, relayControlMsg{Type: "error", Error: err.Error()})
			conn.Close()
			return
		}
		sess.udpConn = uc
		bedrockPort = uc.LocalAddr().(*net.UDPAddr).Port
	}

	host := s.publicHost
	if host == "" {
		if h, _, err := net.SplitHostPort(conn.LocalAddr().String()); err == nil && h != "" && h != "::" && h != "0.0.0.0" {
			host = h
		} else {
			host = "127.0.0.1"
		}
	}

	s.mu.Lock()
	s.sessions[sess.id] = sess
	s.mu.Unlock()

	if err := sess.sendControl(relayControlMsg{
		Type:        "ready",
		SessionID:   sess.id,
		PublicHost:  host,
		JavaPort:    javaPort,
		BedrockPort: bedrockPort,
	}); err != nil {
		sess.close(s)
		return
	}

	if sess.tcpListener != nil {
		go s.acceptJavaLoop(sess)
	}
	if sess.udpConn != nil {
		go s.acceptBedrockUDPLoop(sess)
	}

	// Keep control connection alive until closed
	for {
		if _, err := readLineRaw(conn, 0); err != nil {
			sess.close(s)
			return
		}
	}
}

func (s *RelayServer) acceptJavaLoop(sess *relaySession) {
	for {
		playerConn, err := sess.tcpListener.Accept()
		if err != nil {
			return
		}
		connID := randomHexID(6)
		sess.pendingMu.Lock()
		sess.pendingTCP[connID] = playerConn
		sess.pendingMu.Unlock()

		if err := sess.sendControl(relayControlMsg{
			Type:   "tcp_open",
			ConnID: connID,
		}); err != nil {
			sess.pendingMu.Lock()
			delete(sess.pendingTCP, connID)
			sess.pendingMu.Unlock()
			playerConn.Close()
			return
		}
	}
}

func (s *RelayServer) handleTCPAccept(stream net.Conn, msg relayControlMsg) {
	s.mu.Lock()
	sess := s.sessions[msg.SessionID]
	s.mu.Unlock()
	if sess == nil {
		stream.Close()
		return
	}
	sess.pendingMu.Lock()
	playerConn := sess.pendingTCP[msg.ConnID]
	delete(sess.pendingTCP, msg.ConnID)
	sess.pendingMu.Unlock()
	if playerConn == nil {
		stream.Close()
		return
	}
	pipeBidirectional(playerConn, stream)
}

func (s *RelayServer) acceptBedrockUDPLoop(sess *relaySession) {
	buf := make([]byte, 64*1024)
	for {
		n, peerAddr, err := sess.udpConn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		sess.ctrlMu.Lock()
		udpStream := sess.udpStream
		sess.ctrlMu.Unlock()
		if udpStream == nil {
			continue
		}
		sess.udpWriteMu.Lock()
		err = writeUDPFrame(udpStream, peerAddr.String(), buf[:n])
		sess.udpWriteMu.Unlock()
		if err != nil {
			return
		}
	}
}

func (s *RelayServer) handleUDPAttach(stream net.Conn, msg relayControlMsg) {
	s.mu.Lock()
	sess := s.sessions[msg.SessionID]
	s.mu.Unlock()
	if sess == nil || sess.udpConn == nil {
		stream.Close()
		return
	}

	sess.ctrlMu.Lock()
	if sess.udpStream != nil {
		_ = sess.udpStream.Close()
	}
	sess.udpStream = stream
	sess.ctrlMu.Unlock()

	defer stream.Close()
	for {
		peerStr, payload, err := readUDPFrame(stream)
		if err != nil {
			return
		}
		addr, err := net.ResolveUDPAddr("udp", peerStr)
		if err != nil {
			continue
		}
		_, _ = sess.udpConn.WriteToUDP(payload, addr)
	}
}

// --- RelayManager (Client embedded inside MC SekaiLink) ---

type RelayManager struct {
	mu        sync.Mutex
	ctrlConn  net.Conn
	udpStream net.Conn
	state     models.RelayState
	stopSeq   uint64
}

func NewRelayManager() *RelayManager {
	return &RelayManager{}
}

func (rm *RelayManager) Status() models.RelayState {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	return rm.state
}

func (rm *RelayManager) Start(relayAddr, secret string, localJavaPort, localBedrockPort int, wantJava, wantBedrock bool) (models.RelayState, error) {
	relayAddr = strings.TrimSpace(relayAddr)
	if relayAddr == "" {
		return rm.Status(), fmt.Errorf("relay sunucu adresi gerekli (orn: relay.example.com:25590)")
	}
	if _, _, err := net.SplitHostPort(relayAddr); err != nil {
		relayAddr = net.JoinHostPort(relayAddr, defaultRelayPort)
	}
	if !wantJava && !wantBedrock {
		return rm.Status(), fmt.Errorf("en az Java veya Bedrock aktarimi secilmelidir")
	}

	_ = rm.Stop()

	conn, err := net.DialTimeout("tcp", relayAddr, 5*time.Second)
	if err != nil {
		return rm.failState(relayAddr, localJavaPort, localBedrockPort, err)
	}

	if err := writeLineJSON(conn, relayControlMsg{
		Type:        "hello",
		Secret:      secret,
		WantJava:    wantJava,
		WantBedrock: wantBedrock,
	}); err != nil {
		conn.Close()
		return rm.failState(relayAddr, localJavaPort, localBedrockPort, err)
	}

	line, err := readLineRaw(conn, 5*time.Second)
	if err != nil {
		conn.Close()
		return rm.failState(relayAddr, localJavaPort, localBedrockPort, err)
	}
	var ready relayControlMsg
	if err := json.Unmarshal(line, &ready); err != nil {
		conn.Close()
		return rm.failState(relayAddr, localJavaPort, localBedrockPort, err)
	}
	if ready.Type == "error" {
		conn.Close()
		return rm.failState(relayAddr, localJavaPort, localBedrockPort, fmt.Errorf("%s", ready.Error))
	}
	if ready.Type != "ready" {
		conn.Close()
		return rm.failState(relayAddr, localJavaPort, localBedrockPort, fmt.Errorf("beklenmeyen relay yaniti: %s", ready.Type))
	}

	var javaAddr, bedrockAddr string
	if wantJava && ready.JavaPort > 0 {
		javaAddr = net.JoinHostPort(ready.PublicHost, strconv.Itoa(ready.JavaPort))
	}
	if wantBedrock && ready.BedrockPort > 0 {
		bedrockAddr = net.JoinHostPort(ready.PublicHost, strconv.Itoa(ready.BedrockPort))
	}

	var udpStream net.Conn
	if wantBedrock && ready.BedrockPort > 0 {
		us, err := net.DialTimeout("tcp", relayAddr, 5*time.Second)
		if err != nil {
			conn.Close()
			return rm.failState(relayAddr, localJavaPort, localBedrockPort, err)
		}
		if err := writeLineJSON(us, relayControlMsg{
			Type:      "udp_attach",
			SessionID: ready.SessionID,
		}); err != nil {
			us.Close()
			conn.Close()
			return rm.failState(relayAddr, localJavaPort, localBedrockPort, err)
		}
		udpStream = us
	}

	rm.mu.Lock()
	rm.stopSeq++
	seq := rm.stopSeq
	rm.ctrlConn = conn
	rm.udpStream = udpStream
	rm.state = models.RelayState{
		Running:          true,
		RelayAddr:        relayAddr,
		LocalJavaPort:    localJavaPort,
		LocalBedrockPort: localBedrockPort,
		JavaAddress:      javaAddr,
		BedrockAddress:   bedrockAddr,
	}
	st := rm.state
	rm.mu.Unlock()

	go rm.controlLoop(conn, seq, relayAddr, ready.SessionID, localJavaPort)
	if udpStream != nil {
		go rm.udpLoop(udpStream, seq, localBedrockPort)
	}
	return st, nil
}

func (rm *RelayManager) failState(relayAddr string, jPort, bPort int, err error) (models.RelayState, error) {
	rm.mu.Lock()
	rm.state = models.RelayState{
		Running:          false,
		RelayAddr:        relayAddr,
		LocalJavaPort:    jPort,
		LocalBedrockPort: bPort,
		LastError:        err.Error(),
	}
	st := rm.state
	rm.mu.Unlock()
	return st, err
}

func (rm *RelayManager) Stop() error {
	rm.mu.Lock()
	rm.stopSeq++
	c := rm.ctrlConn
	u := rm.udpStream
	rm.ctrlConn = nil
	rm.udpStream = nil
	rm.state.Running = false
	rm.mu.Unlock()

	if u != nil {
		_ = u.Close()
	}
	if c != nil {
		_ = c.Close()
	}
	return nil
}

func (rm *RelayManager) controlLoop(conn net.Conn, seq uint64, relayAddr, sessionID string, localJavaPort int) {
	defer conn.Close()
	for {
		line, err := readLineRaw(conn, 0)
		if err != nil {
			rm.mu.Lock()
			if rm.stopSeq == seq {
				rm.ctrlConn = nil
				if rm.udpStream != nil {
					_ = rm.udpStream.Close()
					rm.udpStream = nil
				}
				rm.state.Running = false
				if err != io.EOF {
					rm.state.LastError = err.Error()
				}
			}
			rm.mu.Unlock()
			return
		}
		var msg relayControlMsg
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.Type == "tcp_open" && msg.ConnID != "" {
			go forwardRelayTCP(relayAddr, sessionID, msg.ConnID, localJavaPort)
		}
	}
}

func forwardRelayTCP(relayAddr, sessionID, connID string, localPort int) {
	stream, err := net.DialTimeout("tcp", relayAddr, 5*time.Second)
	if err != nil {
		return
	}
	if err := writeLineJSON(stream, relayControlMsg{
		Type:      "tcp_accept",
		SessionID: sessionID,
		ConnID:    connID,
	}); err != nil {
		stream.Close()
		return
	}
	localConn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)), 5*time.Second)
	if err != nil {
		stream.Close()
		return
	}
	pipeBidirectional(localConn, stream)
}

func (rm *RelayManager) udpLoop(udpStream net.Conn, seq uint64, localBedrockPort int) {
	defer udpStream.Close()
	var (
		peersMu   sync.Mutex
		peers     = make(map[string]*net.UDPConn)
		writeMu   sync.Mutex
		targetUDP = &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: localBedrockPort}
	)
	defer func() {
		peersMu.Lock()
		for _, c := range peers {
			_ = c.Close()
		}
		peersMu.Unlock()
	}()

	for {
		peerStr, payload, err := readUDPFrame(udpStream)
		if err != nil {
			rm.mu.Lock()
			if rm.stopSeq == seq {
				rm.udpStream = nil
			}
			rm.mu.Unlock()
			return
		}

		peersMu.Lock()
		pConn := peers[peerStr]
		if pConn == nil {
			dialed, dialErr := net.DialUDP("udp", nil, targetUDP)
			if dialErr == nil {
				pConn = dialed
				peers[peerStr] = pConn
				go func(peerKey string, uc *net.UDPConn) {
					defer func() {
						uc.Close()
						peersMu.Lock()
						delete(peers, peerKey)
						peersMu.Unlock()
					}()
					buf := make([]byte, 64*1024)
					for {
						_ = uc.SetReadDeadline(time.Now().Add(90 * time.Second))
						n, err := uc.Read(buf)
						if err != nil {
							return
						}
						writeMu.Lock()
						wErr := writeUDPFrame(udpStream, peerKey, buf[:n])
						writeMu.Unlock()
						if wErr != nil {
							return
						}
					}
				}(peerStr, pConn)
			}
		}
		peersMu.Unlock()

		if pConn != nil {
			_, _ = pConn.Write(payload)
		}
	}
}

// --- Framing Helpers ---

func writeLineJSON(conn net.Conn, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	defer conn.SetWriteDeadline(time.Time{})
	_, err = conn.Write(b)
	return err
}

func readLineRaw(conn net.Conn, timeout time.Duration) ([]byte, error) {
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		defer conn.SetReadDeadline(time.Time{})
	} else {
		_ = conn.SetReadDeadline(time.Time{})
	}
	var buf []byte
	b := make([]byte, 1)
	for len(buf) < 4096 {
		n, err := conn.Read(b)
		if n > 0 {
			if b[0] == '\n' {
				return buf, nil
			}
			if b[0] != '\r' {
				buf = append(buf, b[0])
			}
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("relay control line too long")
}

func writeUDPFrame(w io.Writer, peer string, payload []byte) error {
	if len(peer) > 255 || len(payload) > 65535 {
		return fmt.Errorf("udp frame exceeds limit")
	}
	hdr := make([]byte, 4+len(peer))
	binary.BigEndian.PutUint16(hdr[0:2], uint16(len(peer)))
	copy(hdr[2:2+len(peer)], peer)
	binary.BigEndian.PutUint16(hdr[2+len(peer):], uint16(len(payload)))
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(payload)
	return err
}

func readUDPFrame(r io.Reader) (string, []byte, error) {
	var lenBuf [2]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return "", nil, err
	}
	peerLen := int(binary.BigEndian.Uint16(lenBuf[:]))
	if peerLen == 0 || peerLen > 255 {
		return "", nil, fmt.Errorf("invalid peer length %d", peerLen)
	}
	peerBytes := make([]byte, peerLen)
	if _, err := io.ReadFull(r, peerBytes); err != nil {
		return "", nil, err
	}
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return "", nil, err
	}
	payLen := int(binary.BigEndian.Uint16(lenBuf[:]))
	payload := make([]byte, payLen)
	if _, err := io.ReadFull(r, payload); err != nil {
		return "", nil, err
	}
	return string(peerBytes), payload, nil
}

func randomHexID(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
