package network

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
)

const (
	defaultRelayPort      = "25590"
	defaultRelayNodeID    = "SV-Relay"
	relayKeepalivePeriod  = 25 * time.Second
)

type relayControlMsg struct {
	Type        string `json:"type"`
	Secret      string `json:"secret,omitempty"`
	NodeID      string `json:"nodeId,omitempty"`
	Encrypted   bool   `json:"encrypted,omitempty"`
	WantJava    bool   `json:"wantJava,omitempty"`
	WantBedrock bool   `json:"wantBedrock,omitempty"`
	SessionID   string `json:"sessionId,omitempty"`
	PublicHost  string `json:"publicHost,omitempty"`
	JavaPort    int    `json:"javaPort,omitempty"`
	BedrockPort int    `json:"bedrockPort,omitempty"`
	ConnID      string `json:"connId,omitempty"`
	Error       string `json:"error,omitempty"`
}

// RelayServerOptions configures a self-hosted SekaiLink VDS bridge node.
type RelayServerOptions struct {
	NodeID     string
	PublicHost string
	Secret     string
	PortMin    int
	PortMax    int
	TLSConfig  *tls.Config
	AllowPlain bool
}

// RelayClientOptions configures an outbound bridge connection from MC SekaiLink.
type RelayClientOptions struct {
	RelayAddr        string
	Secret           string
	TLS              bool
	CertPin          string
	LocalJavaPort    int
	LocalBedrockPort int
	WantJava         bool
	WantBedrock      bool
}

// GenerateSelfSignedRelayTLS creates an in-memory Ed25519 TLS 1.3 certificate and returns
// its TLS config alongside its uppercase SHA-256 hex fingerprint for optional client pinning.
func GenerateSelfSignedRelayTLS() (*tls.Config, string, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	derBytes, err := buildSelfSignedRelayCertDER(priv)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(derBytes)
	pin := strings.ToUpper(hex.EncodeToString(sum[:]))
	cfg := &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{derBytes},
			PrivateKey:  priv,
		}},
		MinVersion: tls.VersionTLS13,
	}
	return cfg, pin, nil
}

// LoadOrCreateRelayTLS loads a persistent TLS keypair from certDir or creates and persists a new
// Ed25519 TLS 1.3 certificate so VDS restarts preserve the exact same CertPin.
func LoadOrCreateRelayTLS(certDir string) (*tls.Config, string, error) {
	if strings.TrimSpace(certDir) == "" {
		return GenerateSelfSignedRelayTLS()
	}
	certPath := filepath.Join(certDir, "relay-cert.pem")
	keyPath := filepath.Join(certDir, "relay-key.pem")
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil && len(cert.Certificate) > 0 {
		sum := sha256.Sum256(cert.Certificate[0])
		pin := strings.ToUpper(hex.EncodeToString(sum[:]))
		cfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS13,
		}
		return cfg, pin, nil
	}

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	derBytes, err := buildSelfSignedRelayCertDER(priv)
	if err != nil {
		return nil, "", err
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(certDir, 0o700); err != nil {
		return nil, "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})
	_ = os.WriteFile(certPath, certPEM, 0o644)
	_ = os.WriteFile(keyPath, keyPEM, 0o600)

	sum := sha256.Sum256(derBytes)
	pin := strings.ToUpper(hex.EncodeToString(sum[:]))
	_ = os.WriteFile(filepath.Join(certDir, "relay.pin"), []byte(pin+"\n"), 0o644)
	cfg := &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{derBytes},
			PrivateKey:  priv,
		}},
		MinVersion: tls.VersionTLS13,
	}
	return cfg, pin, nil
}

func buildSelfSignedRelayCertDER(priv ed25519.PrivateKey) ([]byte, error) {
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "MC-SekaiLink-Relay",
			Organization: []string{"MC SekaiLink"},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	return x509.CreateCertificate(rand.Reader, tmpl, tmpl, priv.Public(), priv)
}

// --- RelayServer (Open-Source Self-Hostable Encrypted TCP + UDP Bridge) ---

type relaySession struct {
	id          string
	encrypted   bool
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
	nodeID     string
	publicHost string
	secret     string
	portMin    int
	portMax    int
	tlsConfig  *tls.Config
	allowPlain bool

	mu       sync.Mutex
	ln       net.Listener
	sessions map[string]*relaySession
}

func NewRelayServer(publicHost, secret string) *RelayServer {
	return NewRelayServerWithOptions(RelayServerOptions{
		NodeID:     defaultRelayNodeID,
		PublicHost: publicHost,
		Secret:     secret,
		AllowPlain: true,
	})
}

func NewRelayServerWithOptions(opts RelayServerOptions) *RelayServer {
	nodeID := strings.TrimSpace(opts.NodeID)
	if nodeID == "" {
		nodeID = defaultRelayNodeID
	}
	allowPlain := opts.AllowPlain
	if opts.TLSConfig == nil {
		allowPlain = true
	}
	return &RelayServer{
		nodeID:     nodeID,
		publicHost: strings.TrimSpace(opts.PublicHost),
		secret:     opts.Secret,
		portMin:    opts.PortMin,
		portMax:    opts.PortMax,
		tlsConfig:  opts.TLSConfig,
		allowPlain: allowPlain,
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

type peekConn struct {
	net.Conn
	prefix  byte
	hasByte bool
}

func (c *peekConn) Read(p []byte) (int, error) {
	if c.hasByte && len(p) > 0 {
		p[0] = c.prefix
		c.hasByte = false
		return 1, nil
	}
	return c.Conn.Read(p)
}

func (s *RelayServer) acceptMaybeTLS(raw net.Conn) (net.Conn, bool, error) {
	tuneTCPConn(raw)
	if s.tlsConfig == nil {
		return raw, false, nil
	}
	var first [1]byte
	_ = raw.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, err := io.ReadFull(raw, first[:])
	_ = raw.SetReadDeadline(time.Time{})
	if err != nil {
		raw.Close()
		return nil, false, err
	}
	pc := &peekConn{Conn: raw, prefix: first[0], hasByte: true}
	if first[0] == 0x16 {
		tlsConn := tls.Server(pc, s.tlsConfig)
		_ = tlsConn.SetDeadline(time.Now().Add(10 * time.Second))
		if err := tlsConn.Handshake(); err != nil {
			tlsConn.Close()
			return nil, false, err
		}
		_ = tlsConn.SetDeadline(time.Time{})
		return tlsConn, true, nil
	}
	if !s.allowPlain {
		if first[0] == '{' {
			_ = writeLineJSON(pc, relayControlMsg{
				Type:  "error",
				Error: "bu VDS koprusu TLS 1.3 sifreleme gerektirir",
			})
		}
		pc.Close()
		return nil, false, fmt.Errorf("plaintext rejected by TLS relay")
	}
	return pc, false, nil
}

func (s *RelayServer) handleConn(raw net.Conn) {
	conn, isTLS, err := s.acceptMaybeTLS(raw)
	if err != nil {
		return
	}

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
	case "ping":
		_ = writeLineJSON(conn, relayControlMsg{
			Type:      "pong",
			NodeID:    s.nodeID,
			Encrypted: isTLS,
		})
		conn.Close()
	case "hello":
		s.handleHello(conn, msg, isTLS)
	case "tcp_accept":
		s.handleTCPAccept(conn, msg)
	case "udp_attach":
		s.handleUDPAttach(conn, msg)
	default:
		conn.Close()
	}
}

func (s *RelayServer) handleHello(conn net.Conn, hello relayControlMsg, isTLS bool) {
	if s.secret != "" && subtle.ConstantTimeCompare([]byte(hello.Secret), []byte(s.secret)) != 1 {
		_ = writeLineJSON(conn, relayControlMsg{Type: "error", Error: "gecersiz relay gizli anahtari"})
		conn.Close()
		return
	}

	sess := &relaySession{
		id:         randomHexID(8),
		encrypted:  isTLS,
		ctrlConn:   conn,
		pendingTCP: make(map[string]net.Conn),
	}

	var javaPort, bedrockPort int
	if hello.WantJava {
		ln, err := listenTCPInRange(s.portMin, s.portMax)
		if err != nil {
			_ = writeLineJSON(conn, relayControlMsg{Type: "error", Error: err.Error()})
			conn.Close()
			return
		}
		sess.tcpListener = ln
		javaPort = ln.Addr().(*net.TCPAddr).Port
	}
	if hello.WantBedrock {
		uc, err := listenUDPInRange(s.portMin, s.portMax)
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
		NodeID:      s.nodeID,
		Encrypted:   isTLS,
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

	// Keep control connection alive and reply to NAT keepalives until closed.
	for {
		rawLine, err := readLineRaw(conn, 90*time.Second)
		if err != nil {
			sess.close(s)
			return
		}
		var ctrl relayControlMsg
		if json.Unmarshal(rawLine, &ctrl) == nil && ctrl.Type == "keepalive" {
			_ = sess.sendControl(relayControlMsg{Type: "keepalive_ack"})
		}
	}
}

func (s *RelayServer) acceptJavaLoop(sess *relaySession) {
	for {
		playerConn, err := sess.tcpListener.Accept()
		if err != nil {
			return
		}
		tuneTCPConn(playerConn)
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
	mu          sync.Mutex
	ctrlWriteMu sync.Mutex
	ctrlConn    net.Conn
	udpStream   net.Conn
	state       models.RelayState
	stopSeq     uint64
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
	return rm.StartWithOptions(RelayClientOptions{
		RelayAddr:        relayAddr,
		Secret:           secret,
		TLS:              false,
		LocalJavaPort:    localJavaPort,
		LocalBedrockPort: localBedrockPort,
		WantJava:         wantJava,
		WantBedrock:      wantBedrock,
	})
}

func (rm *RelayManager) StartWithOptions(opts RelayClientOptions) (models.RelayState, error) {
	relayAddr := normalizeRelayAddr(opts.RelayAddr)
	if relayAddr == "" {
		return rm.Status(), fmt.Errorf("relay sunucu adresi gerekli (orn: vds.example.com:25590)")
	}
	if !opts.WantJava && !opts.WantBedrock {
		return rm.Status(), fmt.Errorf("en az Java veya Bedrock aktarimi secilmelidir")
	}

	_ = rm.Stop()

	conn, isTLS, err := dialRelayConn(relayAddr, opts.TLS, opts.CertPin, 5*time.Second)
	if err != nil {
		return rm.failState(relayAddr, opts.LocalJavaPort, opts.LocalBedrockPort, err)
	}

	rttStart := time.Now()
	if err := writeLineJSON(conn, relayControlMsg{
		Type:        "hello",
		Secret:      opts.Secret,
		WantJava:    opts.WantJava,
		WantBedrock: opts.WantBedrock,
	}); err != nil {
		conn.Close()
		return rm.failState(relayAddr, opts.LocalJavaPort, opts.LocalBedrockPort, err)
	}

	line, err := readLineRaw(conn, 5*time.Second)
	if err != nil {
		conn.Close()
		return rm.failState(relayAddr, opts.LocalJavaPort, opts.LocalBedrockPort, err)
	}
	latencyMs := time.Since(rttStart).Milliseconds()

	var ready relayControlMsg
	if err := json.Unmarshal(line, &ready); err != nil {
		conn.Close()
		return rm.failState(relayAddr, opts.LocalJavaPort, opts.LocalBedrockPort, err)
	}
	if ready.Type == "error" {
		conn.Close()
		return rm.failState(relayAddr, opts.LocalJavaPort, opts.LocalBedrockPort, fmt.Errorf("%s", ready.Error))
	}
	if ready.Type != "ready" {
		conn.Close()
		return rm.failState(relayAddr, opts.LocalJavaPort, opts.LocalBedrockPort, fmt.Errorf("beklenmeyen relay yaniti: %s", ready.Type))
	}

	var javaAddr, bedrockAddr string
	if opts.WantJava && ready.JavaPort > 0 {
		javaAddr = net.JoinHostPort(ready.PublicHost, strconv.Itoa(ready.JavaPort))
	}
	if opts.WantBedrock && ready.BedrockPort > 0 {
		bedrockAddr = net.JoinHostPort(ready.PublicHost, strconv.Itoa(ready.BedrockPort))
	}

	var udpStream net.Conn
	if opts.WantBedrock && ready.BedrockPort > 0 {
		us, _, err := dialRelayConn(relayAddr, opts.TLS, opts.CertPin, 5*time.Second)
		if err != nil {
			conn.Close()
			return rm.failState(relayAddr, opts.LocalJavaPort, opts.LocalBedrockPort, err)
		}
		if err := writeLineJSON(us, relayControlMsg{
			Type:      "udp_attach",
			SessionID: ready.SessionID,
		}); err != nil {
			us.Close()
			conn.Close()
			return rm.failState(relayAddr, opts.LocalJavaPort, opts.LocalBedrockPort, err)
		}
		udpStream = us
	}

	nodeID := strings.TrimSpace(ready.NodeID)
	if nodeID == "" {
		if h, _, err := net.SplitHostPort(relayAddr); err == nil && h != "" {
			nodeID = h
		} else {
			nodeID = defaultRelayNodeID
		}
	}

	rm.mu.Lock()
	rm.stopSeq++
	seq := rm.stopSeq
	rm.ctrlConn = conn
	rm.udpStream = udpStream
	rm.state = models.RelayState{
		Running:          true,
		NodeID:           nodeID,
		Encrypted:        isTLS || ready.Encrypted,
		RelayAddr:        relayAddr,
		LocalJavaPort:    opts.LocalJavaPort,
		LocalBedrockPort: opts.LocalBedrockPort,
		JavaAddress:      javaAddr,
		BedrockAddress:   bedrockAddr,
		LatencyMs:        latencyMs,
	}
	st := rm.state
	rm.mu.Unlock()

	go rm.controlLoop(conn, seq, relayAddr, opts.TLS, opts.CertPin, ready.SessionID, opts.LocalJavaPort)
	if udpStream != nil {
		go rm.udpLoop(udpStream, seq, opts.LocalBedrockPort)
	}
	return st, nil
}

// StartBestNode probes all enabled VDS bridge nodes in parallel and connects to the lowest-latency online node.
func (rm *RelayManager) StartBestNode(
	nodes []models.RelayNode,
	localJavaPort, localBedrockPort int,
	wantJava, wantBedrock bool,
) (models.RelayState, []models.RelayNodeProbe, error) {
	probes := ProbeRelayNodes(nodes)
	if len(probes) == 0 {
		return rm.Status(), probes, fmt.Errorf("etkinlestirilmis VDS koprusu yok")
	}

	byID := make(map[string]models.RelayNode, len(nodes))
	for _, n := range nodes {
		if n.Enabled && strings.TrimSpace(n.Address) != "" {
			byID[normalizeRelayAddr(n.Address)] = n
		}
	}

	var lastErr error
	for _, p := range probes {
		if !p.Online {
			if lastErr == nil && p.Error != "" {
				lastErr = fmt.Errorf("%s: %s", p.Address, p.Error)
			}
			continue
		}
		node, ok := byID[normalizeRelayAddr(p.Address)]
		if !ok {
			continue
		}
		st, err := rm.StartWithOptions(RelayClientOptions{
			RelayAddr:        node.Address,
			Secret:           node.Secret,
			TLS:              node.TLS,
			CertPin:          node.CertPin,
			LocalJavaPort:    localJavaPort,
			LocalBedrockPort: localBedrockPort,
			WantJava:         wantJava,
			WantBedrock:      wantBedrock,
		})
		if err == nil {
			if st.NodeID == "" && strings.TrimSpace(node.ID) != "" {
				rm.mu.Lock()
				rm.state.NodeID = node.ID
				st = rm.state
				rm.mu.Unlock()
			}
			return st, probes, nil
		}
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("hicbir VDS koprusune ulasilamadi")
	}
	return rm.Status(), probes, lastErr
}

// ProbeRelayNodes tests all enabled relay nodes concurrently and sorts online lowest-ping nodes first.
func ProbeRelayNodes(nodes []models.RelayNode) []models.RelayNodeProbe {
	var enabled []models.RelayNode
	for _, n := range nodes {
		if n.Enabled && strings.TrimSpace(n.Address) != "" {
			enabled = append(enabled, n)
		}
	}
	if len(enabled) == 0 {
		return nil
	}
	results := make([]models.RelayNodeProbe, len(enabled))
	var wg sync.WaitGroup
	for i, n := range enabled {
		wg.Add(1)
		go func(idx int, node models.RelayNode) {
			defer wg.Done()
			results[idx] = ProbeRelayNode(node, 3*time.Second)
		}(i, n)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Online != results[j].Online {
			return results[i].Online
		}
		return results[i].LatencyMs < results[j].LatencyMs
	})
	return results
}

// ProbeRelayNode dials a VDS bridge, checks optional CertPin/TLS, and measures 1-RTT control ping.
func ProbeRelayNode(node models.RelayNode, timeout time.Duration) models.RelayNodeProbe {
	addr := normalizeRelayAddr(node.Address)
	out := models.RelayNodeProbe{
		ID:      node.ID,
		Address: addr,
	}
	if addr == "" {
		out.Error = "bos adres"
		return out
	}
	conn, isTLS, err := dialRelayConn(addr, node.TLS, node.CertPin, timeout)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	defer conn.Close()

	t0 := time.Now()
	if err := writeLineJSON(conn, relayControlMsg{Type: "ping"}); err != nil {
		out.Error = err.Error()
		return out
	}
	line, err := readLineRaw(conn, timeout)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	rtt := time.Since(t0).Milliseconds()
	if rtt < 1 {
		rtt = 1
	}

	var pong relayControlMsg
	if err := json.Unmarshal(line, &pong); err != nil {
		out.Error = err.Error()
		return out
	}
	if pong.Type == "error" {
		out.Error = pong.Error
		return out
	}
	out.Online = true
	out.LatencyMs = rtt
	out.Encrypted = isTLS || pong.Encrypted
	out.NodeID = pong.NodeID
	if out.NodeID == "" {
		out.NodeID = node.ID
	}
	return out
}

func dialRelayConn(addr string, useTLS bool, certPin string, timeout time.Duration) (net.Conn, bool, error) {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	raw, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, false, err
	}
	tuneTCPConn(raw)
	if !useTLS {
		return raw, false, nil
	}

	cleanPin := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(certPin), ":", ""), "-", ""))
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, // Verified via CertPin when provided; allows zero-config self-signed VDS certs.
		MinVersion:         tls.VersionTLS13,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if cleanPin == "" {
				return nil
			}
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("sunucu sertifikasi sunmadi")
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			gotPin := strings.ToUpper(hex.EncodeToString(sum[:]))
			if subtle.ConstantTimeCompare([]byte(gotPin), []byte(cleanPin)) != 1 {
				return fmt.Errorf("TLS sertifika parmak izi (CertPin) uyusmuyor")
			}
			return nil
		},
	}
	tlsConn := tls.Client(raw, tlsCfg)
	_ = tlsConn.SetDeadline(time.Now().Add(timeout))
	if err := tlsConn.Handshake(); err != nil {
		tlsConn.Close()
		return nil, false, err
	}
	_ = tlsConn.SetDeadline(time.Time{})
	return tlsConn, true, nil
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

func (rm *RelayManager) controlLoop(conn net.Conn, seq uint64, relayAddr string, useTLS bool, certPin, sessionID string, localJavaPort int) {
	defer conn.Close()

	stopKeepalive := make(chan struct{})
	defer close(stopKeepalive)
	go func() {
		ticker := time.NewTicker(relayKeepalivePeriod)
		defer ticker.Stop()
		for {
			select {
			case <-stopKeepalive:
				return
			case <-ticker.C:
				rm.ctrlWriteMu.Lock()
				err := writeLineJSON(conn, relayControlMsg{Type: "keepalive"})
				rm.ctrlWriteMu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

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
			go forwardRelayTCP(relayAddr, useTLS, certPin, sessionID, msg.ConnID, localJavaPort)
		}
	}
}

func forwardRelayTCP(relayAddr string, useTLS bool, certPin, sessionID, connID string, localPort int) {
	stream, _, err := dialRelayConn(relayAddr, useTLS, certPin, 5*time.Second)
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
	tuneTCPConn(localConn)
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

// --- Network & Port Helpers ---

func normalizeRelayAddr(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(trimmed); err != nil {
		return net.JoinHostPort(trimmed, defaultRelayPort)
	}
	return trimmed
}

func listenTCPInRange(minPort, maxPort int) (net.Listener, error) {
	if minPort <= 0 || maxPort < minPort {
		return net.Listen("tcp", ":0")
	}
	span := maxPort - minPort + 1
	start := cryptoRandInt(span)
	for i := 0; i < span; i++ {
		port := minPort + ((start + i) % span)
		if ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port)); err == nil {
			return ln, nil
		}
	}
	return nil, fmt.Errorf("TCP port araliginda (%d-%d) bos port yok", minPort, maxPort)
}

func listenUDPInRange(minPort, maxPort int) (*net.UDPConn, error) {
	if minPort <= 0 || maxPort < minPort {
		return net.ListenUDP("udp", &net.UDPAddr{Port: 0})
	}
	span := maxPort - minPort + 1
	start := cryptoRandInt(span)
	for i := 0; i < span; i++ {
		port := minPort + ((start + i) % span)
		if uc, err := net.ListenUDP("udp", &net.UDPAddr{Port: port}); err == nil {
			return uc, nil
		}
	}
	return nil, fmt.Errorf("UDP port araliginda (%d-%d) bos port yok", minPort, maxPort)
}

func cryptoRandInt(max int) int {
	if max <= 1 {
		return 0
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0
	}
	return int(n.Int64())
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
