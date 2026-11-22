package network

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"github.com/quic-go/quic-go"
)

const (
	p2pALPN             = "sekailink-p2p-v1"
	p2pStreamControl    = byte(0x00)
	p2pStreamJavaTCP    = byte(0x01)
	p2pStreamBedrockUDP = byte(0x02)

	p2pPunchPingByte = byte(0x21)
	p2pPunchPongByte = byte(0x22)
)

var defaultSTUNServers = []string{
	"stun.l.google.com:19302",
	"stun.cloudflare.com:3478",
	"stun1.l.google.com:19302",
}

type P2PHostOptions struct {
	RoomCode         string
	ServerName       string
	LocalJavaPort    int
	LocalBedrockPort int
	WantJava         bool
	WantBedrock      bool

	bus         p2pSignalBus
	stunServers []string
	disableSTUN bool
}

type P2PJoinOptions struct {
	RoomCode             string
	PeerName             string
	PreferredJavaPort    int
	PreferredBedrockPort int

	bus                 p2pSignalBus
	stunServers         []string
	disableSTUN         bool
	lanBroadcastAddr    string
	disableLANBroadcast bool
}

type p2pPeerEntry struct {
	info  models.P2PPeerInfo
	qConn *quic.Conn
}

type p2pSession struct {
	mux         *p2pUDPMux
	cancel      context.CancelFunc
	tr          *quic.Transport
	udpConn     *net.UDPConn
	listener    *quic.Listener
	localJavaLn net.Listener
	localBedUDP *net.UDPConn

	connMu     sync.RWMutex
	activeConn *quic.Conn
}

func (s *p2pSession) getActiveConn() *quic.Conn {
	if s == nil {
		return nil
	}
	s.connMu.RLock()
	c := s.activeConn
	s.connMu.RUnlock()
	return c
}

func (s *p2pSession) setActiveConn(c *quic.Conn) {
	if s == nil {
		return
	}
	s.connMu.Lock()
	s.activeConn = c
	s.connMu.Unlock()
}

// P2PManager provides pure-Go cross-platform NAT hole-punched P2P rooms
// over QUIC + UDP datagrams/streams for both Java (TCP) and Bedrock (UDP).
type P2PManager struct {
	mu      sync.Mutex
	session *p2pSession
	peers   map[string]*p2pPeerEntry
	state   models.P2PState
	stopSeq uint64
}

func NewP2PManager() *P2PManager {
	return &P2PManager{
		peers: make(map[string]*p2pPeerEntry),
	}
}

func (pm *P2PManager) Status() models.P2PState {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	cp := pm.state
	if len(pm.peers) > 0 {
		peers := make([]models.P2PPeerInfo, 0, len(pm.peers))
		for _, p := range pm.peers {
			peers = append(peers, p.info)
		}
		cp.Peers = peers
		cp.PeerCount = len(peers)
	} else {
		cp.PeerCount = 0
		cp.Peers = nil
	}
	return cp
}

func (pm *P2PManager) Stop() error {
	pm.mu.Lock()
	pm.stopSeq++
	sess := pm.session
	pm.session = nil
	peers := pm.peers
	pm.peers = make(map[string]*p2pPeerEntry)
	pm.state = models.P2PState{
		Running: false,
		Phase:   "idle",
	}
	pm.mu.Unlock()

	if sess != nil {
		closeP2PSession(sess, peers)
	}
	return nil
}

func closeP2PSession(sess *p2pSession, peers map[string]*p2pPeerEntry) {
	if sess.cancel != nil {
		sess.cancel()
	}
	if active := sess.getActiveConn(); active != nil {
		_ = active.CloseWithError(0, "p2p stopped")
	}
	for _, p := range peers {
		if p.qConn != nil {
			_ = p.qConn.CloseWithError(0, "p2p stopped")
		}
	}
	if sess.localJavaLn != nil {
		_ = sess.localJavaLn.Close()
	}
	if sess.localBedUDP != nil {
		_ = sess.localBedUDP.Close()
	}
	if sess.listener != nil {
		_ = sess.listener.Close()
	}
	if sess.tr != nil {
		_ = sess.tr.Close()
	}
	if sess.udpConn != nil {
		_ = sess.udpConn.Close()
	}
}

func p2pQuicConfig() *quic.Config {
	return &quic.Config{
		MaxIncomingStreams:             512,
		MaxIdleTimeout:                 60 * time.Second,
		HandshakeIdleTimeout:           10 * time.Second,
		KeepAlivePeriod:                3 * time.Second,
		DisablePathMTUDiscovery:        true,
		InitialPacketSize:              1200,
		EnableDatagrams:                true,
		InitialStreamReceiveWindow:     2 * 1024 * 1024,
		MaxStreamReceiveWindow:         8 * 1024 * 1024,
		InitialConnectionReceiveWindow: 4 * 1024 * 1024,
		MaxConnectionReceiveWindow:     16 * 1024 * 1024,
	}
}

// StartHost opens a P2P host room and waits for friends using a short XXXX-XXXX room code.
func (pm *P2PManager) StartHost(opts P2PHostOptions) (models.P2PState, error) {
	if !opts.WantJava && !opts.WantBedrock {
		opts.WantJava = true
	}
	if opts.WantJava && (opts.LocalJavaPort <= 0 || opts.LocalJavaPort > 65535) {
		opts.LocalJavaPort = 25565
	}
	if opts.WantBedrock && (opts.LocalBedrockPort <= 0 || opts.LocalBedrockPort > 65535) {
		opts.LocalBedrockPort = 19132
	}

	roomCode := strings.TrimSpace(opts.RoomCode)
	if roomCode == "" {
		roomCode = GenerateRoomCode()
	} else {
		norm, err := NormalizeRoomCode(roomCode)
		if err != nil {
			return pm.Status(), err
		}
		roomCode = norm
	}

	_ = pm.Stop()

	topicHash, aesKey, punchKey := deriveRoomKeys(roomCode)
	tlsCfg, certSHA, err := generateEphemeralP2PTLS()
	if err != nil {
		return pm.failState("host", roomCode, err)
	}

	udpConn, tr, mux, err := startP2PMux(punchKey)
	if err != nil {
		return pm.failState("host", roomCode, err)
	}

	listener, err := tr.Listen(tlsCfg, p2pQuicConfig())
	if err != nil {
		_ = tr.Close()
		_ = udpConn.Close()
		return pm.failState("host", roomCode, fmt.Errorf("QUIC P2P dinleyici baslatilamadi: %w", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	sess := &p2pSession{
		mux:      mux,
		cancel:   cancel,
		tr:       tr,
		udpConn:  udpConn,
		listener: listener,
	}

	pm.mu.Lock()
	pm.stopSeq++
	seq := pm.stopSeq
	pm.session = sess
	pm.peers = make(map[string]*p2pPeerEntry)
	pm.state = models.P2PState{
		Running:          true,
		Mode:             "host",
		Phase:            "discovering",
		RoomCode:         roomCode,
		ServerName:       strings.TrimSpace(opts.ServerName),
		NATType:          "unknown",
		LocalJavaPort:    opts.LocalJavaPort,
		LocalBedrockPort: opts.LocalBedrockPort,
		WantJava:         opts.WantJava,
		WantBedrock:      opts.WantBedrock,
	}
	pm.mu.Unlock()

	go mux.run(ctx)

	// Discover public STUN reflexive endpoint + NAT type (non-fatal if offline; LAN still works).
	stunRes := stunResult{NATType: "unknown"}
	if !opts.disableSTUN {
		servers := opts.stunServers
		if len(servers) == 0 {
			servers = defaultSTUNServers
		}
		stunRes = mux.discoverSTUN(ctx, servers)
	}
	candidates := buildP2PCandidates(udpConn.LocalAddr().(*net.UDPAddr).Port, stunRes.PublicEndpoint, opts.disableSTUN)

	bus := opts.bus
	if bus == nil {
		bus = newDefaultSignalBus()
	}
	sigCh, err := bus.Subscribe(ctx, topicHash)
	if err != nil {
		_ = pm.Stop()
		return pm.failState("host", roomCode, err)
	}

	pm.mu.Lock()
	if pm.stopSeq == seq {
		pm.state.Phase = "waiting"
		pm.state.NATType = stunRes.NATType
		pm.state.PublicEndpoint = stunRes.PublicEndpoint
		pm.state.SignalingStatus = bus.Status()
	}
	st := pm.state
	pm.mu.Unlock()

	// Periodic STUN keepalive so CGNAT preserves the UDP mapping while room is open.
	if !opts.disableSTUN {
		servers := opts.stunServers
		if len(servers) == 0 {
			servers = defaultSTUNServers
		}
		go mux.keepAliveLoop(ctx, servers[0])
	}

	// Signal responder loop
	go pm.hostSignalLoop(ctx, seq, bus, topicHash, aesKey, mux, stunRes, candidates, opts, certSHA, sigCh)

	// QUIC peer accept loop
	go pm.hostAcceptLoop(ctx, seq, listener, opts)

	return st, nil
}

func (pm *P2PManager) hostSignalLoop(
	ctx context.Context,
	seq uint64,
	bus p2pSignalBus,
	topicHash string,
	aesKey [32]byte,
	mux *p2pUDPMux,
	stunRes stunResult,
	initialCands []string,
	opts P2PHostOptions,
	certSHA string,
	sigCh <-chan string,
) {
	servers := opts.stunServers
	if len(servers) == 0 {
		servers = defaultSTUNServers
	}

	var candMu sync.Mutex
	latestStun := stunRes
	latestCands := initialCands
	lastStunAt := time.Now()
	serverName := strings.TrimSpace(opts.ServerName)

	for {
		select {
		case <-ctx.Done():
			return
		case rawEnc, ok := <-sigCh:
			if !ok {
				return
			}
			plain, err := decryptSignal(aesKey, rawEnc)
			if err != nil {
				continue
			}
			var msg p2pSignalMsg
			if json.Unmarshal(plain, &msg) != nil || msg.Type != "join_req" || msg.PeerID == "" {
				continue
			}

			// Refresh STUN if unknown or stale (>25s) so NAT port rebinds are picked up fast
			candMu.Lock()
			needRefresh := !opts.disableSTUN && (latestStun.PublicEndpoint == "" || time.Since(lastStunAt) > 25*time.Second)
			if needRefresh {
				candMu.Unlock()
				stunCtx, cancelStun := context.WithTimeout(ctx, 2*time.Second)
				fresh := mux.discoverSTUN(stunCtx, servers)
				cancelStun()
				candMu.Lock()
				lastStunAt = time.Now()
				if fresh.PublicEndpoint != "" {
					latestStun = fresh
					latestCands = buildP2PCandidates(mux.localPort(), fresh.PublicEndpoint, opts.disableSTUN)
					pm.mu.Lock()
					if pm.stopSeq == seq {
						pm.state.NATType = fresh.NATType
						pm.state.PublicEndpoint = fresh.PublicEndpoint
					}
					pm.mu.Unlock()
				}
			}
			offerCands := append([]string{}, latestCands...)
			natType := latestStun.NATType
			portDelta := latestStun.PortDelta
			candMu.Unlock()

			offer := p2pSignalMsg{
				MsgID:        randomHex(6),
				Type:         "host_offer",
				ServerName:   serverName,
				TargetPeerID: msg.PeerID,
				Candidates:   offerCands,
				NATType:      natType,
				PortDelta:    portDelta,
				JavaPort:     opts.LocalJavaPort,
				BedrockPort:  opts.LocalBedrockPort,
				WantJava:     opts.WantJava,
				WantBedrock:  opts.WantBedrock,
				CertSHA256:   certSHA,
				Timestamp:    time.Now().Unix(),
			}
			offerRaw, err := json.Marshal(offer)
			if err == nil {
				if enc, err2 := encryptSignal(aesKey, offerRaw); err2 == nil {
					_ = bus.Publish(ctx, topicHash, enc)
				}
			}

			pm.mu.Lock()
			if pm.stopSeq == seq {
				pm.state.SignalingStatus = bus.Status()
			}
			pm.mu.Unlock()

			// Punch concurrently toward friend's candidate endpoints (+ delta prediction for mild symmetric NAT)
			peerTag, _ := hex.DecodeString(msg.PeerID)
			targets := expandSymmetricCandidates(msg.Candidates, msg.NATType, msg.PortDelta)
			if len(targets) > 0 && len(peerTag) == 8 {
				var tagArr [8]byte
				copy(tagArr[:], peerTag)
				go mux.burstPunch(ctx, tagArr, targets, 6*time.Second)
			}
		}
	}
}

func (pm *P2PManager) hostAcceptLoop(ctx context.Context, seq uint64, ln *quic.Listener, opts P2PHostOptions) {
	for {
		qConn, err := ln.Accept(ctx)
		if err != nil {
			pm.mu.Lock()
			if pm.stopSeq == seq && ctx.Err() == nil {
				pm.state.Running = false
				if pm.state.LastError == "" {
					pm.state.LastError = err.Error()
				}
			}
			pm.mu.Unlock()
			return
		}
		go pm.handleHostPeer(ctx, seq, qConn, opts)
	}
}

func (pm *P2PManager) handleHostPeer(ctx context.Context, seq uint64, qConn *quic.Conn, opts P2PHostOptions) {
	peerID := randomHex(4)
	remoteStr := qConn.RemoteAddr().String()
	pm.mu.Lock()
	if pm.stopSeq != seq {
		pm.mu.Unlock()
		_ = qConn.CloseWithError(0, "session closed")
		return
	}
	pm.peers[peerID] = &p2pPeerEntry{
		info: models.P2PPeerInfo{
			ID:          peerID,
			Name:        "Arkadas",
			RemoteAddr:  remoteStr,
			ConnectedAt: time.Now().Unix(),
		},
		qConn: qConn,
	}
	pm.state.Phase = "connected"
	pm.mu.Unlock()

	defer func() {
		pm.mu.Lock()
		if pm.stopSeq == seq {
			delete(pm.peers, peerID)
			if len(pm.peers) == 0 && pm.state.Running {
				pm.state.Phase = "waiting"
			}
		}
		pm.mu.Unlock()
		_ = qConn.CloseWithError(0, "peer done")
	}()

	peerCtx, cancelPeer := context.WithCancel(ctx)
	defer cancelPeer()

	var bedTable *p2pHostBedrockTable
	if opts.WantBedrock && opts.LocalBedrockPort > 0 {
		bedTable = newP2PHostBedrockTable(peerCtx, qConn, opts.LocalBedrockPort)
		defer bedTable.CloseAll()
		go bedTable.receiveDatagramsLoop()
	}

	for {
		stream, err := qConn.AcceptStream(peerCtx)
		if err != nil {
			return
		}
		go pm.handleHostStream(peerCtx, seq, peerID, stream, opts, bedTable)
	}
}

func (pm *P2PManager) handleHostStream(
	ctx context.Context,
	seq uint64,
	peerID string,
	stream *quic.Stream,
	opts P2PHostOptions,
	bedTable *p2pHostBedrockTable,
) {
	var kind [1]byte
	if _, err := io.ReadFull(stream, kind[:]); err != nil {
		_ = stream.Close()
		return
	}

	switch kind[0] {
	case p2pStreamControl:
		pm.handleHostControlStream(seq, peerID, stream)
	case p2pStreamJavaTCP:
		if !opts.WantJava || opts.LocalJavaPort <= 0 {
			stream.CancelRead(0)
			stream.CancelWrite(0)
			_ = stream.Close()
			return
		}
		handleE4mcStream(stream, opts.LocalJavaPort)
	case p2pStreamBedrockUDP:
		if bedTable != nil {
			bedTable.serveFallbackStream(ctx, stream)
		} else {
			_ = stream.Close()
		}
	default:
		_ = stream.Close()
	}
}

func (pm *P2PManager) handleHostControlStream(seq uint64, peerID string, stream *quic.Stream) {
	defer stream.Close()

	// Hello frame: [2B len][JSON {"peerName": ...}]
	var lenBuf [2]byte
	if _, err := io.ReadFull(stream, lenBuf[:]); err != nil {
		return
	}
	helloLen := int(binary.BigEndian.Uint16(lenBuf[:]))
	if helloLen > 0 && helloLen <= 1024 {
		helloRaw := make([]byte, helloLen)
		if _, err := io.ReadFull(stream, helloRaw); err != nil {
			return
		}
		var hello struct {
			PeerName string `json:"peerName"`
		}
		if json.Unmarshal(helloRaw, &hello) == nil && strings.TrimSpace(hello.PeerName) != "" {
			pm.mu.Lock()
			if pm.stopSeq == seq {
				if p, ok := pm.peers[peerID]; ok {
					p.info.Name = strings.TrimSpace(hello.PeerName)
				}
			}
			pm.mu.Unlock()
		}
	}

	// Ping/Pong loop: client sends [1B op=0x01][8B ts][8B clientMeasuredRttMs], host replies [1B op=0x01][8B ts]
	var pkt [17]byte
	for {
		if _, err := io.ReadFull(stream, pkt[:]); err != nil {
			return
		}
		if pkt[0] != 0x01 {
			continue
		}
		rttMs := int64(binary.BigEndian.Uint64(pkt[9:17]))
		if rttMs > 0 {
			pm.mu.Lock()
			if pm.stopSeq == seq {
				if p, ok := pm.peers[peerID]; ok {
					p.info.LatencyMs = rttMs
				}
			}
			pm.mu.Unlock()
		}
		var reply [9]byte
		reply[0] = 0x01
		copy(reply[1:9], pkt[1:9])
		if _, err := stream.Write(reply[:]); err != nil {
			return
		}
	}
}

// StartJoin joins a friend's P2P room using an XXXX-XXXX room code and exposes local loopback/LAN ports.
func (pm *P2PManager) StartJoin(opts P2PJoinOptions) (models.P2PState, error) {
	normCode, err := NormalizeRoomCode(opts.RoomCode)
	if err != nil {
		return pm.Status(), err
	}
	peerName := strings.TrimSpace(opts.PeerName)
	if peerName == "" {
		if hn, hErr := os.Hostname(); hErr == nil && strings.TrimSpace(hn) != "" {
			peerName = strings.TrimSpace(hn)
		} else {
			peerName = "Oyuncu"
		}
	}
	if opts.PreferredJavaPort <= 0 || opts.PreferredJavaPort > 65535 {
		opts.PreferredJavaPort = 25565
	}
	if opts.PreferredBedrockPort <= 0 || opts.PreferredBedrockPort > 65535 {
		opts.PreferredBedrockPort = 19132
	}

	_ = pm.Stop()

	topicHash, aesKey, punchKey := deriveRoomKeys(normCode)
	udpConn, tr, mux, err := startP2PMux(punchKey)
	if err != nil {
		return pm.failState("join", normCode, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	sess := &p2pSession{
		mux:     mux,
		cancel:  cancel,
		tr:      tr,
		udpConn: udpConn,
	}

	pm.mu.Lock()
	pm.stopSeq++
	seq := pm.stopSeq
	pm.session = sess
	pm.peers = make(map[string]*p2pPeerEntry)
	pm.state = models.P2PState{
		Running:  true,
		Mode:     "join",
		Phase:    "discovering",
		RoomCode: normCode,
		NATType:  "unknown",
	}
	pm.mu.Unlock()

	go mux.run(ctx)

	if !opts.disableSTUN {
		servers := opts.stunServers
		if len(servers) == 0 {
			servers = defaultSTUNServers
		}
		go mux.keepAliveLoop(ctx, servers[0])
	}

	bus := opts.bus
	if bus == nil {
		bus = newDefaultSignalBus()
	}
	sigCh, err := bus.Subscribe(ctx, topicHash)
	if err != nil {
		_ = pm.Stop()
		return pm.failState("join", normCode, err)
	}

	qConn, offer, stunRes, err := pm.dialHostOverSignaling(ctx, seq, bus, topicHash, aesKey, mux, tr, sigCh, peerName, opts, false)
	if err != nil {
		_ = pm.Stop()
		return pm.failState("join", normCode, err)
	}

	var localJavaAddr string
	var localJavaPortNum int
	var localJavaLn net.Listener
	if offer.WantJava {
		ln, bindErr := listenLocalTCPWithFallback(opts.PreferredJavaPort)
		if bindErr != nil {
			_ = qConn.CloseWithError(0, "local bind failed")
			_ = pm.Stop()
			return pm.failState("join", normCode, fmt.Errorf("yerel Java portu acilamadi: %w", bindErr))
		}
		localJavaLn = ln
		_, portStr, _ := net.SplitHostPort(ln.Addr().String())
		localJavaPortNum, _ = strconv.Atoi(portStr)
		localJavaAddr = "localhost:" + portStr
	}

	var localBedrockAddr string
	var localBedrockPortNum int
	var localBedUDP *net.UDPConn
	if offer.WantBedrock {
		uc, bindErr := listenLocalUDPWithFallback(opts.PreferredBedrockPort)
		if bindErr != nil {
			if localJavaLn != nil {
				_ = localJavaLn.Close()
			}
			_ = qConn.CloseWithError(0, "local udp bind failed")
			_ = pm.Stop()
			return pm.failState("join", normCode, fmt.Errorf("yerel Bedrock portu acilamadi: %w", bindErr))
		}
		localBedUDP = uc
		_, portStr, _ := net.SplitHostPort(uc.LocalAddr().String())
		localBedrockPortNum, _ = strconv.Atoi(portStr)
		localBedrockAddr = "127.0.0.1:" + portStr
	}

	serverName := strings.TrimSpace(offer.ServerName)

	pm.mu.Lock()
	if pm.stopSeq != seq {
		pm.mu.Unlock()
		if localJavaLn != nil {
			_ = localJavaLn.Close()
		}
		if localBedUDP != nil {
			_ = localBedUDP.Close()
		}
		_ = qConn.CloseWithError(0, "stopped")
		return pm.Status(), fmt.Errorf("oturum durduruldu")
	}
	sess.localJavaLn = localJavaLn
	sess.localBedUDP = localBedUDP
	sess.setActiveConn(qConn)
	pm.peers["host"] = &p2pPeerEntry{
		info: models.P2PPeerInfo{
			ID:          "host",
			Name:        "Sunucu Sahibi",
			RemoteAddr:  qConn.RemoteAddr().String(),
			ConnectedAt: time.Now().Unix(),
		},
		qConn: qConn,
	}
	pm.state.Running = true
	pm.state.Phase = "connected"
	pm.state.ServerName = serverName
	pm.state.NATType = stunRes.NATType
	pm.state.PublicEndpoint = stunRes.PublicEndpoint
	pm.state.SignalingStatus = bus.Status()
	pm.state.WantJava = offer.WantJava
	pm.state.WantBedrock = offer.WantBedrock
	pm.state.LocalJavaPort = localJavaPortNum
	pm.state.LocalBedrockPort = localBedrockPortNum
	pm.state.LocalJavaAddr = localJavaAddr
	pm.state.LocalBedrockAddr = localBedrockAddr
	pm.state.LatencyMs = 1
	pm.state.LastError = ""
	st := pm.state
	pm.mu.Unlock()

	var bedTable *p2pJoinBedrockTable
	if localBedUDP != nil {
		bedTable = newP2PJoinBedrockTable()
	}

	go pm.joinSupervisorLoop(ctx, seq, sess, bus, topicHash, aesKey, mux, tr, sigCh, peerName, opts, qConn, bedTable)
	if localJavaLn != nil {
		go pm.joinJavaAcceptLoop(ctx, seq, sess, localJavaLn)
		if !opts.disableLANBroadcast && localJavaPortNum > 0 {
			lanName := "MC SekaiLink P2P (" + normCode + ")"
			if serverName != "" {
				lanName = serverName + " (P2P)"
			}
			go engine.RunJavaLANBroadcaster(ctx, lanName, localJavaPortNum, func() bool {
				return sess.getActiveConn() != nil
			}, opts.lanBroadcastAddr)
		}
	}
	if localBedUDP != nil && bedTable != nil {
		go pm.joinBedrockForwardLoop(ctx, seq, sess, localBedUDP, bedTable)
	}

	return st, nil
}

func (pm *P2PManager) dialHostOverSignaling(
	ctx context.Context,
	seq uint64,
	bus p2pSignalBus,
	topicHash string,
	aesKey [32]byte,
	mux *p2pUDPMux,
	tr *quic.Transport,
	sigCh <-chan string,
	peerName string,
	opts P2PJoinOptions,
	isReconnect bool,
) (*quic.Conn, *p2pSignalMsg, stunResult, error) {
	if ctx.Err() != nil {
		return nil, nil, stunResult{NATType: "unknown"}, fmt.Errorf("baglanti iptal edildi")
	}

	stunRes := stunResult{NATType: "unknown"}
	if !opts.disableSTUN {
		servers := opts.stunServers
		if len(servers) == 0 {
			servers = defaultSTUNServers
		}
		stunCtx, cancelStun := context.WithTimeout(ctx, 2500*time.Millisecond)
		stunRes = mux.discoverSTUN(stunCtx, servers)
		cancelStun()
	}
	myCands := buildP2PCandidates(mux.localPort(), stunRes.PublicEndpoint, opts.disableSTUN)

	var peerTag [8]byte
	_, _ = rand.Read(peerTag[:])
	peerID := hex.EncodeToString(peerTag[:])

	pm.mu.Lock()
	if pm.stopSeq == seq {
		if stunRes.PublicEndpoint != "" || pm.state.PublicEndpoint == "" {
			pm.state.NATType = stunRes.NATType
			pm.state.PublicEndpoint = stunRes.PublicEndpoint
		}
		pm.state.SignalingStatus = bus.Status()
	}
	pm.mu.Unlock()

	reqMsg := p2pSignalMsg{
		MsgID:      randomHex(6),
		Type:       "join_req",
		PeerID:     peerID,
		PeerName:   peerName,
		Candidates: myCands,
		NATType:    stunRes.NATType,
		PortDelta:  stunRes.PortDelta,
		Timestamp:  time.Now().Unix(),
	}
	reqPlain, _ := json.Marshal(reqMsg)
	encReq, err := encryptSignal(aesKey, reqPlain)
	if err != nil {
		return nil, nil, stunRes, err
	}

	publishCtx, cancelPub := context.WithCancel(ctx)
	defer cancelPub()
	go func() {
		defer cancelPub()
		_ = bus.Publish(publishCtx, topicHash, encReq)
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; i < 4; i++ {
			select {
			case <-publishCtx.Done():
				return
			case <-ticker.C:
				_ = bus.Publish(publishCtx, topicHash, encReq)
			}
		}
	}()

	var offer *p2pSignalMsg
	offerDeadline := time.NewTimer(9 * time.Second)
	defer offerDeadline.Stop()

waitOffer:
	for {
		select {
		case <-ctx.Done():
			return nil, nil, stunRes, fmt.Errorf("baglanti iptal edildi")
		case <-offerDeadline.C:
			return nil, nil, stunRes, fmt.Errorf("oda bulunamadi: oda kodunu kontrol edin ve arkadasinizin P2P odasinin acik oldugundan emin olun")
		case encMsg, ok := <-sigCh:
			if !ok {
				return nil, nil, stunRes, fmt.Errorf("sinyallesme kanali kapandi")
			}
			plain, dErr := decryptSignal(aesKey, encMsg)
			if dErr != nil {
				continue
			}
			var m p2pSignalMsg
			if json.Unmarshal(plain, &m) != nil || m.Type != "host_offer" {
				continue
			}
			if m.TargetPeerID == peerID {
				offer = &m
				break waitOffer
			}
		}
	}
	cancelPub()

	if !isReconnect {
		pm.mu.Lock()
		if pm.stopSeq == seq {
			pm.state.Phase = "punching"
		}
		pm.mu.Unlock()
	}

	punchCtx, cancelPunch := context.WithCancel(ctx)
	defer cancelPunch()
	verifiedCh := mux.watchVerified(peerTag)
	defer mux.unwatchVerified(peerTag)

	hostTargets := expandSymmetricCandidates(offer.Candidates, offer.NATType, offer.PortDelta)
	go mux.burstPunch(punchCtx, peerTag, hostTargets, 5*time.Second)

	var targetAddr *net.UDPAddr
	punchWait := time.NewTimer(3200 * time.Millisecond)
	select {
	case addr := <-verifiedCh:
		targetAddr = addr
	case <-punchWait.C:
	case <-ctx.Done():
		punchWait.Stop()
		return nil, nil, stunRes, fmt.Errorf("baglanti iptal edildi")
	}
	punchWait.Stop()

	pinnedTLS := buildPinnedP2PClientTLS(offer.CertSHA256)
	var qConn *quic.Conn
	var dialErr error

	if targetAddr != nil {
		dialCtx, cancelDial := context.WithTimeout(ctx, 5*time.Second)
		qConn, dialErr = tr.Dial(dialCtx, targetAddr, pinnedTLS, p2pQuicConfig())
		cancelDial()
	}

	if qConn == nil {
		for _, candStr := range offer.Candidates {
			udpAddr, rErr := net.ResolveUDPAddr("udp4", candStr)
			if rErr != nil {
				continue
			}
			dialCtx, cancelDial := context.WithTimeout(ctx, 2500*time.Millisecond)
			c, err2 := tr.Dial(dialCtx, udpAddr, pinnedTLS, p2pQuicConfig())
			cancelDial()
			if err2 == nil {
				qConn = c
				dialErr = nil
				break
			}
			dialErr = err2
		}
	}

	if qConn == nil {
		if stunRes.NATType == "symmetric" || offer.NATType == "symmetric" {
			return nil, nil, stunRes, fmt.Errorf("simetrik CGNAT (mobil veri veya kisitli ISS) direkt P2P delmeyi engelledi. Ev Wi-Fi deneyin veya e4mc / pinggy.io / playit.gg koprusunu kullanin")
		}
		if dialErr != nil {
			return nil, nil, stunRes, fmt.Errorf("direkt P2P baglantisi kurulamadi: %w", dialErr)
		}
		return nil, nil, stunRes, fmt.Errorf("direkt P2P zaman asimi (guvenlik duvari UDP paketlerini engelliyor olabilir)")
	}

	return qConn, offer, stunRes, nil
}

func (pm *P2PManager) joinSupervisorLoop(
	ctx context.Context,
	seq uint64,
	sess *p2pSession,
	bus p2pSignalBus,
	topicHash string,
	aesKey [32]byte,
	mux *p2pUDPMux,
	tr *quic.Transport,
	sigCh <-chan string,
	peerName string,
	opts P2PJoinOptions,
	initialConn *quic.Conn,
	bedTable *p2pJoinBedrockTable,
) {
	curConn := initialConn
	backoffs := []time.Duration{250 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second}

	for {
		connCtx, cancelConn := context.WithCancel(ctx)
		if sess.localBedUDP != nil && bedTable != nil {
			go pm.joinBedrockReceiveLoop(connCtx, curConn, sess.localBedUDP, bedTable)
		}

		pm.runJoinControlSession(connCtx, seq, curConn, peerName)
		cancelConn()
		sess.setActiveConn(nil)
		_ = curConn.CloseWithError(0, "p2p reconnecting")

		if ctx.Err() != nil {
			return
		}

		pm.mu.Lock()
		if pm.stopSeq != seq {
			pm.mu.Unlock()
			return
		}
		delete(pm.peers, "host")
		pm.state.Running = true
		pm.state.Phase = "reconnecting"
		pm.state.LatencyMs = 0
		pm.state.LastError = ""
		pm.mu.Unlock()

		attempt := 0
		for {
			wait := backoffs[len(backoffs)-1]
			if attempt < len(backoffs) {
				wait = backoffs[attempt]
			}
			attempt++

			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}

			newConn, newOffer, newStun, dialErr := pm.dialHostOverSignaling(ctx, seq, bus, topicHash, aesKey, mux, tr, sigCh, peerName, opts, true)
			if ctx.Err() != nil {
				if newConn != nil {
					_ = newConn.CloseWithError(0, "stopped")
				}
				return
			}
			if dialErr != nil || newConn == nil {
				pm.mu.Lock()
				if pm.stopSeq == seq && dialErr != nil {
					pm.state.LastError = dialErr.Error()
				}
				pm.mu.Unlock()
				continue
			}

			pm.mu.Lock()
			if pm.stopSeq != seq || ctx.Err() != nil {
				pm.mu.Unlock()
				_ = newConn.CloseWithError(0, "stopped")
				return
			}
			sess.setActiveConn(newConn)
			pm.peers["host"] = &p2pPeerEntry{
				info: models.P2PPeerInfo{
					ID:          "host",
					Name:        "Sunucu Sahibi",
					RemoteAddr:  newConn.RemoteAddr().String(),
					ConnectedAt: time.Now().Unix(),
				},
				qConn: newConn,
			}
			pm.state.Running = true
			pm.state.Phase = "connected"
			pm.state.LatencyMs = 1
			pm.state.LastError = ""
			pm.state.SignalingStatus = bus.Status()
			if newStun.PublicEndpoint != "" {
				pm.state.NATType = newStun.NATType
				pm.state.PublicEndpoint = newStun.PublicEndpoint
			}
			if strings.TrimSpace(newOffer.ServerName) != "" {
				pm.state.ServerName = strings.TrimSpace(newOffer.ServerName)
			}
			pm.mu.Unlock()

			curConn = newConn
			break
		}
	}
}

func (pm *P2PManager) runJoinControlSession(ctx context.Context, seq uint64, qConn *quic.Conn, peerName string) {
	doneWatch := make(chan struct{})
	defer close(doneWatch)
	go func() {
		select {
		case <-ctx.Done():
			_ = qConn.CloseWithError(0, "context cancelled")
		case <-qConn.Context().Done():
		case <-doneWatch:
		}
	}()

	openControlStream := func() (*quic.Stream, error) {
		streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		s, err := qConn.OpenStreamSync(streamCtx)
		cancel()
		if err != nil {
			return nil, err
		}
		helloRaw, _ := json.Marshal(map[string]string{"peerName": peerName})
		hdr := make([]byte, 1+2+len(helloRaw))
		hdr[0] = p2pStreamControl
		binary.BigEndian.PutUint16(hdr[1:3], uint16(len(helloRaw)))
		copy(hdr[3:], helloRaw)
		if _, err := s.Write(hdr); err != nil {
			_ = s.Close()
			return nil, err
		}
		return s, nil
	}

	var stream *quic.Stream
	defer func() {
		if stream != nil {
			_ = stream.Close()
		}
	}()

	var lastRttMs int64
	sendPing := func(s *quic.Stream) error {
		var pkt [17]byte
		pkt[0] = 0x01
		binary.BigEndian.PutUint64(pkt[1:9], uint64(time.Now().UnixNano()))
		binary.BigEndian.PutUint64(pkt[9:17], uint64(lastRttMs))
		if _, err := s.Write(pkt[:]); err != nil {
			return err
		}
		var reply [9]byte
		_ = s.SetReadDeadline(time.Now().Add(7 * time.Second))
		_, err := io.ReadFull(s, reply[:])
		_ = s.SetReadDeadline(time.Time{})
		if err != nil {
			return err
		}
		sentNs := int64(binary.BigEndian.Uint64(reply[1:9]))
		ms := time.Since(time.Unix(0, sentNs)).Milliseconds()
		if ms < 1 {
			ms = 1
		}
		lastRttMs = ms
		pm.mu.Lock()
		if pm.stopSeq == seq {
			pm.state.LatencyMs = ms
			if h, ok := pm.peers["host"]; ok {
				h.info.LatencyMs = ms
			}
		}
		pm.mu.Unlock()
		return nil
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	misses := 0
	for {
		if ctx.Err() != nil || qConn.Context().Err() != nil {
			return
		}
		if stream == nil {
			var openErr error
			stream, openErr = openControlStream()
			if openErr != nil {
				if ctx.Err() != nil || qConn.Context().Err() != nil {
					return
				}
				misses++
				if misses >= 3 {
					return
				}
				select {
				case <-ctx.Done():
					return
				case <-qConn.Context().Done():
					return
				case <-time.After(800 * time.Millisecond):
					continue
				}
			}
		}

		if err := sendPing(stream); err != nil {
			_ = stream.Close()
			stream = nil
			if ctx.Err() != nil || qConn.Context().Err() != nil {
				return
			}
			misses++
			if misses >= 3 {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-qConn.Context().Done():
				return
			case <-time.After(800 * time.Millisecond):
				continue
			}
		}
		misses = 0

		select {
		case <-ctx.Done():
			return
		case <-qConn.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func (pm *P2PManager) joinJavaAcceptLoop(ctx context.Context, seq uint64, sess *p2pSession, ln net.Listener) {
	for {
		localConn, err := ln.Accept()
		if err != nil {
			pm.mu.Lock()
			if pm.stopSeq == seq && ctx.Err() == nil {
				pm.state.Running = false
			}
			pm.mu.Unlock()
			return
		}
		tuneTCPConn(localConn)
		go func(lc net.Conn) {
			qConn := sess.getActiveConn()
			if qConn == nil {
				_ = lc.Close()
				return
			}
			sCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			stream, sErr := qConn.OpenStreamSync(sCtx)
			cancel()
			if sErr != nil {
				_ = lc.Close()
				return
			}
			if _, wErr := stream.Write([]byte{p2pStreamJavaTCP}); wErr != nil {
				_ = stream.Close()
				_ = lc.Close()
				return
			}
			pipeStreamAndTCP(stream, lc)
		}(localConn)
	}
}

func pipeStreamAndTCP(stream *quic.Stream, localConn net.Conn) {
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

// --- Bedrock UDP Datagram + Oversized Stream Relay ---

type p2pJoinBedrockTable struct {
	mu       sync.Mutex
	byAddr   map[string]uint16
	byID     map[uint16]*net.UDPAddr
	lastSeen map[uint16]time.Time
	nextID   uint16
}

func newP2PJoinBedrockTable() *p2pJoinBedrockTable {
	return &p2pJoinBedrockTable{
		byAddr:   make(map[string]uint16),
		byID:     make(map[uint16]*net.UDPAddr),
		lastSeen: make(map[uint16]time.Time),
		nextID:   1,
	}
}

func (t *p2pJoinBedrockTable) getOrCreate(addr *net.UDPAddr) uint16 {
	key := addr.String()
	t.mu.Lock()
	defer t.mu.Unlock()
	if sid, ok := t.byAddr[key]; ok {
		t.lastSeen[sid] = time.Now()
		return sid
	}
	if len(t.byID) > 256 {
		now := time.Now()
		for id, ts := range t.lastSeen {
			if now.Sub(ts) > 90*time.Second {
				if a := t.byID[id]; a != nil {
					delete(t.byAddr, a.String())
				}
				delete(t.byID, id)
				delete(t.lastSeen, id)
			}
		}
	}
	for i := 0; i < 65535; i++ {
		sid := t.nextID
		t.nextID++
		if t.nextID == 0 {
			t.nextID = 1
		}
		if _, exists := t.byID[sid]; !exists {
			cp := &net.UDPAddr{IP: append([]byte{}, addr.IP...), Port: addr.Port, Zone: addr.Zone}
			t.byAddr[key] = sid
			t.byID[sid] = cp
			t.lastSeen[sid] = time.Now()
			return sid
		}
	}
	return 1
}

func (t *p2pJoinBedrockTable) lookup(sid uint16) *net.UDPAddr {
	t.mu.Lock()
	defer t.mu.Unlock()
	a := t.byID[sid]
	if a != nil {
		t.lastSeen[sid] = time.Now()
	}
	return a
}

func (pm *P2PManager) joinBedrockReceiveLoop(ctx context.Context, qConn *quic.Conn, udpLn *net.UDPConn, table *p2pJoinBedrockTable) {
	for {
		dgram, err := qConn.ReceiveDatagram(ctx)
		if err != nil {
			return
		}
		if len(dgram) < 3 {
			continue
		}
		sid := binary.BigEndian.Uint16(dgram[0:2])
		if addr := table.lookup(sid); addr != nil {
			_, _ = udpLn.WriteToUDP(dgram[2:], addr)
		}
	}
}

func (pm *P2PManager) joinBedrockForwardLoop(ctx context.Context, _ uint64, sess *p2pSession, udpLn *net.UDPConn, table *p2pJoinBedrockTable) {
	var fallbackMu sync.Mutex
	var fallbackConn *quic.Conn
	var fallbackStream *quic.Stream

	sendViaFallback := func(qConn *quic.Conn, sid uint16, payload []byte) {
		fallbackMu.Lock()
		defer fallbackMu.Unlock()
		if fallbackStream != nil && fallbackConn != qConn {
			_ = fallbackStream.Close()
			fallbackStream = nil
			fallbackConn = nil
		}
		if fallbackStream == nil {
			sCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			s, err := qConn.OpenStreamSync(sCtx)
			cancel()
			if err != nil {
				return
			}
			if _, err := s.Write([]byte{p2pStreamBedrockUDP}); err != nil {
				_ = s.Close()
				return
			}
			fallbackStream = s
			fallbackConn = qConn
			go func(fs *quic.Stream) {
				for {
					rSid, rPayload, rErr := readBedrockFallbackFrame(fs)
					if rErr != nil {
						return
					}
					if addr := table.lookup(rSid); addr != nil {
						_, _ = udpLn.WriteToUDP(rPayload, addr)
					}
				}
			}(fallbackStream)
		}
		if err := writeBedrockFallbackFrame(fallbackStream, sid, payload); err != nil {
			_ = fallbackStream.Close()
			fallbackStream = nil
			fallbackConn = nil
		}
	}

	// Writer: local Bedrock client -> QUIC Datagram on current active QUIC connection
	buf := make([]byte, 4096)
	for {
		n, remoteAddr, err := udpLn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if n <= 0 {
			continue
		}
		qConn := sess.getActiveConn()
		if qConn == nil {
			continue
		}
		sid := table.getOrCreate(remoteAddr)
		dgram := make([]byte, 2+n)
		binary.BigEndian.PutUint16(dgram[0:2], sid)
		copy(dgram[2:], buf[:n])
		if err := qConn.SendDatagram(dgram); err != nil {
			var tooLarge *quic.DatagramTooLargeError
			if errors.As(err, &tooLarge) {
				sendViaFallback(qConn, sid, buf[:n])
			} else if ctx.Err() != nil {
				return
			}
		}
	}
}

type p2pHostBedrockTable struct {
	ctx        context.Context
	qConn      *quic.Conn
	targetPort int

	mu       sync.Mutex
	sessions map[uint16]*net.UDPConn
	lastSeen map[uint16]time.Time
	fbMu     sync.Mutex
	fbStream *quic.Stream
}

func newP2PHostBedrockTable(ctx context.Context, qConn *quic.Conn, targetPort int) *p2pHostBedrockTable {
	return &p2pHostBedrockTable{
		ctx:        ctx,
		qConn:      qConn,
		targetPort: targetPort,
		sessions:   make(map[uint16]*net.UDPConn),
		lastSeen:   make(map[uint16]time.Time),
	}
}

func (t *p2pHostBedrockTable) CloseAll() {
	t.mu.Lock()
	for id, c := range t.sessions {
		_ = c.Close()
		delete(t.sessions, id)
	}
	t.mu.Unlock()
	t.fbMu.Lock()
	if t.fbStream != nil {
		_ = t.fbStream.Close()
		t.fbStream = nil
	}
	t.fbMu.Unlock()
}

func (t *p2pHostBedrockTable) forwardToServer(sid uint16, payload []byte) {
	t.mu.Lock()
	conn, exists := t.sessions[sid]
	if exists {
		t.lastSeen[sid] = time.Now()
		t.mu.Unlock()
		_, _ = conn.Write(payload)
		return
	}

	if len(t.sessions) > 256 {
		now := time.Now()
		for id, ts := range t.lastSeen {
			if now.Sub(ts) > 90*time.Second {
				if old := t.sessions[id]; old != nil {
					_ = old.Close()
				}
				delete(t.sessions, id)
				delete(t.lastSeen, id)
			}
		}
	}

	rAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: t.targetPort}
	newConn, err := net.DialUDP("udp4", nil, rAddr)
	if err != nil {
		t.mu.Unlock()
		return
	}
	t.sessions[sid] = newConn
	t.lastSeen[sid] = time.Now()
	t.mu.Unlock()

	go t.readServerReplies(sid, newConn)
	_, _ = newConn.Write(payload)
}

func (t *p2pHostBedrockTable) readServerReplies(sid uint16, conn *net.UDPConn) {
	defer func() {
		_ = conn.Close()
		t.mu.Lock()
		if cur, ok := t.sessions[sid]; ok && cur == conn {
			delete(t.sessions, sid)
			delete(t.lastSeen, sid)
		}
		t.mu.Unlock()
	}()

	buf := make([]byte, 4096)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(95 * time.Second))
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		if n <= 0 {
			continue
		}
		t.mu.Lock()
		t.lastSeen[sid] = time.Now()
		t.mu.Unlock()

		dgram := make([]byte, 2+n)
		binary.BigEndian.PutUint16(dgram[0:2], sid)
		copy(dgram[2:], buf[:n])
		if err := t.qConn.SendDatagram(dgram); err != nil {
			var tooLarge *quic.DatagramTooLargeError
			if errors.As(err, &tooLarge) {
				t.fbMu.Lock()
				fs := t.fbStream
				if fs != nil {
					if wErr := writeBedrockFallbackFrame(fs, sid, buf[:n]); wErr != nil {
						_ = fs.Close()
						t.fbStream = nil
					}
				}
				t.fbMu.Unlock()
			} else if t.ctx.Err() != nil {
				return
			}
		}
	}
}

func (t *p2pHostBedrockTable) receiveDatagramsLoop() {
	for {
		dgram, err := t.qConn.ReceiveDatagram(t.ctx)
		if err != nil {
			return
		}
		if len(dgram) < 3 {
			continue
		}
		sid := binary.BigEndian.Uint16(dgram[0:2])
		t.forwardToServer(sid, dgram[2:])
	}
}

func (t *p2pHostBedrockTable) serveFallbackStream(ctx context.Context, stream *quic.Stream) {
	t.fbMu.Lock()
	if t.fbStream != nil {
		_ = t.fbStream.Close()
	}
	t.fbStream = stream
	t.fbMu.Unlock()

	defer stream.Close()
	for {
		if ctx.Err() != nil {
			return
		}
		sid, payload, err := readBedrockFallbackFrame(stream)
		if err != nil {
			return
		}
		t.forwardToServer(sid, payload)
	}
}

func writeBedrockFallbackFrame(w io.Writer, sid uint16, payload []byte) error {
	hdr := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint16(hdr[0:2], sid)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(len(payload)))
	copy(hdr[4:], payload)
	_, err := w.Write(hdr)
	return err
}

func readBedrockFallbackFrame(r io.Reader) (uint16, []byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	sid := binary.BigEndian.Uint16(hdr[0:2])
	length := int(binary.BigEndian.Uint16(hdr[2:4]))
	if length <= 0 || length > 8192 {
		return 0, nil, fmt.Errorf("invalid udp frame len: %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return 0, nil, err
	}
	return sid, buf, nil
}

// --- Non-QUIC Multiplexer (RFC 5389 STUN + HMAC UDP Hole Punching) ---

type p2pUDPMux struct {
	tr       *quic.Transport
	udpConn  *net.UDPConn
	punchKey [32]byte

	mu          sync.Mutex
	stunWaiters map[[12]byte]chan string
	punchWatch  map[[8]byte]chan *net.UDPAddr
}

func startP2PMux(punchKey [32]byte) (*net.UDPConn, *quic.Transport, *p2pUDPMux, error) {
	udpConn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("UDP P2P soketi acilamadi: %w", err)
	}
	_ = udpConn.SetReadBuffer(1024 * 1024)
	_ = udpConn.SetWriteBuffer(1024 * 1024)

	tr := &quic.Transport{Conn: udpConn}
	mux := &p2pUDPMux{
		tr:          tr,
		udpConn:     udpConn,
		punchKey:    punchKey,
		stunWaiters: make(map[[12]byte]chan string),
		punchWatch:  make(map[[8]byte]chan *net.UDPAddr),
	}
	return udpConn, tr, mux, nil
}

func (m *p2pUDPMux) localPort() int {
	if addr, ok := m.udpConn.LocalAddr().(*net.UDPAddr); ok {
		return addr.Port
	}
	return 0
}

func (m *p2pUDPMux) watchVerified(peerTag [8]byte) <-chan *net.UDPAddr {
	ch := make(chan *net.UDPAddr, 8)
	m.mu.Lock()
	m.punchWatch[peerTag] = ch
	m.mu.Unlock()
	return ch
}

func (m *p2pUDPMux) unwatchVerified(peerTag [8]byte) {
	m.mu.Lock()
	delete(m.punchWatch, peerTag)
	m.mu.Unlock()
}

func (m *p2pUDPMux) run(ctx context.Context) {
	buf := make([]byte, 2048)
	for {
		n, addr, err := m.tr.ReadNonQUICPacket(ctx, buf)
		if err != nil {
			return
		}
		if n < 20 {
			continue
		}
		pkt := buf[:n]
		udpAddr, ok := addr.(*net.UDPAddr)
		if !ok {
			if resolved, rErr := net.ResolveUDPAddr("udp4", addr.String()); rErr == nil {
				udpAddr = resolved
			}
		}

		// STUN Binding Success Response: type 0x0101, magic cookie 0x2112A442
		if pkt[0] == 0x01 && pkt[1] == 0x01 &&
			pkt[4] == 0x21 && pkt[5] == 0x12 && pkt[6] == 0xA4 && pkt[7] == 0x42 {
			var txID [12]byte
			copy(txID[:], pkt[8:20])
			mapped, mErr := parseSTUNXorMappedAddr(pkt)
			if mErr == nil {
				m.mu.Lock()
				ch := m.stunWaiters[txID]
				m.mu.Unlock()
				if ch != nil {
					select {
					case ch <- mapped:
					default:
					}
				}
			}
			continue
		}

		// P2P Hole Punch packet (28 bytes): [0x21|0x22, 'S', 'K', 'P', peerTag(8), hmac16(16)]
		if n == 28 && (pkt[0] == p2pPunchPingByte || pkt[0] == p2pPunchPongByte) &&
			pkt[1] == 'S' && pkt[2] == 'K' && pkt[3] == 'P' {
			mac := hmac.New(sha256.New, m.punchKey[:])
			mac.Write(pkt[:12])
			expected := mac.Sum(nil)[:16]
			if !hmac.Equal(expected, pkt[12:28]) {
				continue
			}

			var peerTag [8]byte
			copy(peerTag[:], pkt[4:12])

			if pkt[0] == p2pPunchPingByte && udpAddr != nil {
				pong := m.buildPunchPacket(p2pPunchPongByte, peerTag)
				_, _ = m.tr.WriteTo(pong, udpAddr)
			}

			if udpAddr != nil {
				m.mu.Lock()
				ch := m.punchWatch[peerTag]
				m.mu.Unlock()
				if ch != nil {
					cp := &net.UDPAddr{IP: append([]byte{}, udpAddr.IP...), Port: udpAddr.Port}
					select {
					case ch <- cp:
					default:
					}
				}
			}
		}
	}
}

func (m *p2pUDPMux) buildPunchPacket(msgType byte, peerTag [8]byte) []byte {
	pkt := make([]byte, 28)
	pkt[0] = msgType
	pkt[1] = 'S'
	pkt[2] = 'K'
	pkt[3] = 'P'
	copy(pkt[4:12], peerTag[:])
	mac := hmac.New(sha256.New, m.punchKey[:])
	mac.Write(pkt[:12])
	copy(pkt[12:28], mac.Sum(nil)[:16])
	return pkt
}

func (m *p2pUDPMux) burstPunch(ctx context.Context, peerTag [8]byte, targets []string, duration time.Duration) {
	resolved := make([]*net.UDPAddr, 0, len(targets))
	seen := make(map[string]bool)
	for _, t := range targets {
		ua, err := net.ResolveUDPAddr("udp4", t)
		if err != nil {
			continue
		}
		k := ua.String()
		if !seen[k] {
			seen[k] = true
			resolved = append(resolved, ua)
		}
	}
	if len(resolved) == 0 {
		return
	}

	pkt := m.buildPunchPacket(p2pPunchPingByte, peerTag)
	sendAll := func() {
		for _, ua := range resolved {
			_, _ = m.tr.WriteTo(pkt, ua)
		}
	}

	sendAll()
	deadline := time.NewTimer(duration)
	defer deadline.Stop()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-ticker.C:
			sendAll()
		}
	}
}

type stunResult struct {
	PublicEndpoint string
	NATType        string
	PortDelta      int
}

func (m *p2pUDPMux) querySTUNServer(ctx context.Context, server string, timeout time.Duration) (string, error) {
	addr, err := net.ResolveUDPAddr("udp4", server)
	if err != nil {
		return "", err
	}
	var txID [12]byte
	_, _ = rand.Read(txID[:])

	req := make([]byte, 20)
	req[0], req[1] = 0x00, 0x01 // Binding Request
	req[2], req[3] = 0x00, 0x00
	req[4], req[5], req[6], req[7] = 0x21, 0x12, 0xA4, 0x42
	copy(req[8:20], txID[:])

	ch := make(chan string, 1)
	m.mu.Lock()
	m.stunWaiters[txID] = ch
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.stunWaiters, txID)
		m.mu.Unlock()
	}()

	_, err = m.tr.WriteTo(req, addr)
	if err != nil {
		return "", err
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	retry := time.NewTicker(600 * time.Millisecond)
	defer retry.Stop()

	for {
		select {
		case mapped := <-ch:
			return mapped, nil
		case <-retry.C:
			_, _ = m.tr.WriteTo(req, addr)
		case <-timer.C:
			return "", fmt.Errorf("STUN timeout (%s)", server)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func (m *p2pUDPMux) discoverSTUN(ctx context.Context, servers []string) stunResult {
	if len(servers) == 0 {
		return stunResult{NATType: "unknown"}
	}

	// Probe up to 3 STUN servers concurrently for fast NAT classification
	limit := len(servers)
	if limit > 3 {
		limit = 3
	}
	results := make([]string, limit)
	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func(idx int, srv string) {
			defer wg.Done()
			mapped, err := m.querySTUNServer(ctx, srv, 2200*time.Millisecond)
			if err == nil {
				results[idx] = mapped
			}
		}(i, servers[i])
	}
	wg.Wait()

	var valid []string
	for _, r := range results {
		if r != "" {
			valid = append(valid, r)
		}
	}
	if len(valid) == 0 {
		return stunResult{NATType: "unknown"}
	}

	primary := valid[0]
	natType := "cone"
	portDelta := 0
	if len(valid) >= 2 {
		h1, p1Str, _ := net.SplitHostPort(valid[0])
		h2, p2Str, _ := net.SplitHostPort(valid[1])
		p1, _ := strconv.Atoi(p1Str)
		p2, _ := strconv.Atoi(p2Str)
		if h1 == h2 && p1 != p2 {
			natType = "symmetric"
			delta := p2 - p1
			if delta < 0 {
				delta = -delta
			}
			portDelta = delta
		}
	} else {
		natType = "unknown"
	}

	return stunResult{
		PublicEndpoint: primary,
		NATType:        natType,
		PortDelta:      portDelta,
	}
}

func (m *p2pUDPMux) keepAliveLoop(ctx context.Context, stunServer string) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			addr, err := net.ResolveUDPAddr("udp4", stunServer)
			if err != nil {
				continue
			}
			var txID [12]byte
			_, _ = rand.Read(txID[:])
			req := make([]byte, 20)
			req[0], req[1] = 0x00, 0x01
			req[4], req[5], req[6], req[7] = 0x21, 0x12, 0xA4, 0x42
			copy(req[8:20], txID[:])
			_, _ = m.tr.WriteTo(req, addr)
		}
	}
}

func parseSTUNXorMappedAddr(pkt []byte) (string, error) {
	if len(pkt) < 20 {
		return "", fmt.Errorf("short STUN packet")
	}
	msgLen := int(binary.BigEndian.Uint16(pkt[2:4]))
	end := 20 + msgLen
	if end > len(pkt) {
		end = len(pkt)
	}
	var fallback string
	offset := 20
	for offset+4 <= end {
		attrType := binary.BigEndian.Uint16(pkt[offset : offset+2])
		attrLen := int(binary.BigEndian.Uint16(pkt[offset+2 : offset+4]))
		valStart := offset + 4
		valEnd := valStart + attrLen
		if valEnd > end {
			break
		}
		val := pkt[valStart:valEnd]
		if attrType == 0x0020 && len(val) >= 8 && val[1] == 0x01 {
			xPort := binary.BigEndian.Uint16(val[2:4]) ^ 0x2112
			ip := net.IPv4(val[4]^0x21, val[5]^0x12, val[6]^0xA4, val[7]^0x42)
			return net.JoinHostPort(ip.String(), strconv.Itoa(int(xPort))), nil
		}
		if attrType == 0x0001 && len(val) >= 8 && val[1] == 0x01 {
			port := binary.BigEndian.Uint16(val[2:4])
			ip := net.IPv4(val[4], val[5], val[6], val[7])
			fallback = net.JoinHostPort(ip.String(), strconv.Itoa(int(port)))
		}
		pad := (4 - (attrLen % 4)) % 4
		offset = valEnd + pad
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("XOR-MAPPED-ADDRESS not found")
}

func buildP2PCandidates(udpPort int, publicEndpoint string, includeLoopback bool) []string {
	var out []string
	seen := make(map[string]bool)
	add := func(addr string) {
		addr = strings.TrimSpace(addr)
		if addr != "" && !seen[addr] {
			seen[addr] = true
			out = append(out, addr)
		}
	}

	add(publicEndpoint)

	ifaces, err := net.Interfaces()
	if err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, err2 := iface.Addrs()
			if err2 != nil {
				continue
			}
			for _, a := range addrs {
				ipNet, ok := a.(*net.IPNet)
				if !ok {
					continue
				}
				ip4 := ipNet.IP.To4()
				if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() {
					continue
				}
				add(net.JoinHostPort(ip4.String(), strconv.Itoa(udpPort)))
			}
		}
	}

	if includeLoopback || len(out) == 0 {
		add(net.JoinHostPort("127.0.0.1", strconv.Itoa(udpPort)))
	}
	return out
}

func expandSymmetricCandidates(cands []string, natType string, delta int) []string {
	out := append([]string{}, cands...)
	if natType != "symmetric" || len(cands) == 0 {
		return out
	}
	host, portStr, err := net.SplitHostPort(cands[0])
	if err != nil {
		return out
	}
	basePort, err := strconv.Atoi(portStr)
	if err != nil || basePort <= 0 {
		return out
	}
	seen := make(map[string]bool, len(out)+10)
	for _, c := range out {
		seen[c] = true
	}
	addPort := func(p int) {
		if p > 0 && p <= 65535 {
			s := net.JoinHostPort(host, strconv.Itoa(p))
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	if delta > 0 && delta <= 64 {
		addPort(basePort + delta)
		addPort(basePort - delta)
		addPort(basePort + 2*delta)
		addPort(basePort - 2*delta)
		addPort(basePort + 3*delta)
		addPort(basePort - 3*delta)
	}
	for d := 1; d <= 12; d++ {
		addPort(basePort + d)
		addPort(basePort - d)
	}
	return out
}

func generateEphemeralP2PTLS() (*tls.Config, string, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return nil, "", err
	}
	tpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "MC-SekaiLink-P2P"},
		NotBefore:    time.Now().Add(-10 * time.Minute),
		NotAfter:     time.Now().Add(30 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &tpl, &tpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(derBytes)
	pin := hex.EncodeToString(sum[:])

	cert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{p2pALPN},
		MinVersion:   tls.VersionTLS13,
	}, pin, nil
}

func buildPinnedP2PClientTLS(expectedSHA256 string) *tls.Config {
	expected := strings.ToLower(strings.TrimSpace(expectedSHA256))
	return &tls.Config{
		InsecureSkipVerify: true, // Pinning enforced manually in VerifyPeerCertificate
		NextProtos:         []string{p2pALPN},
		MinVersion:         tls.VersionTLS13,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			if len(rawCerts) == 0 {
				return fmt.Errorf("host TLS sertifikasi gondermedi")
			}
			sum := sha256.Sum256(rawCerts[0])
			got := hex.EncodeToString(sum[:])
			if expected != "" && got != expected {
				return fmt.Errorf("P2P TLS sertifika imzasi eslesmedi")
			}
			return nil
		},
	}
}

func listenLocalTCPWithFallback(preferredPort int) (net.Listener, error) {
	if preferredPort > 0 && preferredPort <= 65535 {
		if ln, err := net.Listen("tcp4", net.JoinHostPort("0.0.0.0", strconv.Itoa(preferredPort))); err == nil {
			return ln, nil
		}
		if ln, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(preferredPort))); err == nil {
			return ln, nil
		}
	}
	if ln, err := net.Listen("tcp4", "0.0.0.0:0"); err == nil {
		return ln, nil
	}
	return net.Listen("tcp4", "127.0.0.1:0")
}

func listenLocalUDPWithFallback(preferredPort int) (*net.UDPConn, error) {
	if preferredPort > 0 && preferredPort <= 65535 {
		if uc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: preferredPort}); err == nil {
			return uc, nil
		}
		if uc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: preferredPort}); err == nil {
			return uc, nil
		}
	}
	if uc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0}); err == nil {
		return uc, nil
	}
	return net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
}

func (pm *P2PManager) failState(mode, roomCode string, err error) (models.P2PState, error) {
	pm.mu.Lock()
	pm.state = models.P2PState{
		Running:   false,
		Mode:      mode,
		Phase:     "error",
		RoomCode:  roomCode,
		LastError: err.Error(),
	}
	st := pm.state
	pm.mu.Unlock()
	return st, err
}

func randomHex(numBytes int) string {
	b := make([]byte, numBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
