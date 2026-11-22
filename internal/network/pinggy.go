package network

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"golang.org/x/crypto/ssh"
)

var (
	pinggyTCPURLRe  = regexp.MustCompile(`tcp://([a-zA-Z0-9.\-]+:\d+)`)
	pinggyHostPortRe = regexp.MustCompile(`([a-zA-Z0-9.\-]+\.(?:pinggy\.link|pinggy\.io):\d+)`)
)

type pinggyEndpoint struct {
	Region string
	Host   string
}

type pinggyPortConfig struct {
	ConfigTcp            int `json:"ConfigTcp"`
	UsageContinuousTcp   int `json:"UsageContinuousTcp"`
	UsageOnceLongPollTcp int `json:"UsageOnceLongPollTcp"`
	UsageTcp             int `json:"UsageTcp"`
	UrlTcp               int `json:"UrlTcp"`
	StatusPort           int `json:"StatusPort"`
	GreetingMsgTCPPort   int `json:"GreetingMsgTCP"`
}

type pinggyConnStatus struct {
	Success       bool   `json:"Success"`
	Authenticated bool   `json:"Authenticated"`
	Error         string `json:"Error"`
}

// PinggyManager implements a pure-Go Pinggy SSH TCP reverse tunnel client
// requiring zero external SSH binaries across Windows, Linux, and Android.
type PinggyManager struct {
	mu      sync.Mutex
	client  *ssh.Client
	ln      net.Listener
	cancel  context.CancelFunc
	state   models.PinggyState
	stopSeq uint64
}

func NewPinggyManager() *PinggyManager {
	return &PinggyManager{}
}

func (pm *PinggyManager) Status() models.PinggyState {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.state
}

func (pm *PinggyManager) Stop() error {
	pm.mu.Lock()
	pm.stopSeq++
	cancel := pm.cancel
	ln := pm.ln
	client := pm.client
	pm.cancel = nil
	pm.ln = nil
	pm.client = nil
	pm.state.Running = false
	pm.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if ln != nil {
		_ = ln.Close()
	}
	if client != nil {
		_ = client.Close()
	}
	return nil
}

// Start opens a raw TCP tunnel on Pinggy (preferring low-RTT EU nodes by default).
func (pm *PinggyManager) Start(region, token string, localPort int) (models.PinggyState, error) {
	candidates := buildPinggyEndpoints(region, token)
	sorted := sortPinggyEndpointsByRTT(context.Background(), candidates)
	return pm.startWithCandidates(sorted, token, localPort)
}

func buildPinggyEndpoints(region, token string) []pinggyEndpoint {
	raw := strings.TrimSpace(region)
	lower := strings.ToLower(raw)
	hasToken := strings.TrimSpace(token) != ""

	switch lower {
	case "", "auto", "eu":
		if hasToken {
			return []pinggyEndpoint{
				{Region: "EU", Host: "eu.a.pinggy.io:443"},
				{Region: "AUTO", Host: "a.pinggy.io:443"},
				{Region: "EU", Host: "eu.free.pinggy.io:443"},
				{Region: "AUTO", Host: "free.pinggy.io:443"},
			}
		}
		return []pinggyEndpoint{
			{Region: "EU", Host: "eu.free.pinggy.io:443"},
			{Region: "EU", Host: "eu.a.pinggy.io:443"},
			{Region: "AUTO", Host: "free.pinggy.io:443"},
			{Region: "AUTO", Host: "a.pinggy.io:443"},
		}
	case "us":
		if hasToken {
			return []pinggyEndpoint{
				{Region: "US", Host: "us.a.pinggy.io:443"},
				{Region: "AUTO", Host: "a.pinggy.io:443"},
				{Region: "AUTO", Host: "free.pinggy.io:443"},
			}
		}
		return []pinggyEndpoint{
			{Region: "US", Host: "us.a.pinggy.io:443"},
			{Region: "AUTO", Host: "free.pinggy.io:443"},
		}
	case "ap":
		if hasToken {
			return []pinggyEndpoint{
				{Region: "AP", Host: "ap.a.pinggy.io:443"},
				{Region: "AUTO", Host: "a.pinggy.io:443"},
				{Region: "AUTO", Host: "free.pinggy.io:443"},
			}
		}
		return []pinggyEndpoint{
			{Region: "AP", Host: "ap.a.pinggy.io:443"},
			{Region: "AUTO", Host: "free.pinggy.io:443"},
		}
	default:
		if strings.Contains(raw, ".") {
			host := raw
			if _, _, err := net.SplitHostPort(host); err != nil {
				host = net.JoinHostPort(host, "443")
			}
			return []pinggyEndpoint{{Region: "CUSTOM", Host: host}}
		}
		return []pinggyEndpoint{
			{Region: "EU", Host: "eu.free.pinggy.io:443"},
			{Region: "EU", Host: "eu.a.pinggy.io:443"},
			{Region: "AUTO", Host: "free.pinggy.io:443"},
		}
	}
}

type pinggyProbedEndpoint struct {
	ep    pinggyEndpoint
	rttMs int64
	order int
}

func sortPinggyEndpointsByRTT(ctx context.Context, candidates []pinggyEndpoint) []pinggyEndpoint {
	if len(candidates) <= 1 {
		return candidates
	}
	probed := make([]pinggyProbedEndpoint, len(candidates))
	var wg sync.WaitGroup
	for i, ep := range candidates {
		wg.Add(1)
		go func(idx int, e pinggyEndpoint) {
			defer wg.Done()
			pctx, cancel := context.WithTimeout(ctx, 1200*time.Millisecond)
			defer cancel()
			d := net.Dialer{Timeout: 1200 * time.Millisecond}
			t0 := time.Now()
			conn, err := d.DialContext(pctx, "tcp", e.Host)
			var rtt int64
			if err == nil {
				rtt = time.Since(t0).Milliseconds()
				if rtt < 1 {
					rtt = 1
				}
				_ = conn.Close()
			}
			probed[idx] = pinggyProbedEndpoint{ep: e, rttMs: rtt, order: idx}
		}(i, ep)
	}
	wg.Wait()

	sort.SliceStable(probed, func(i, j int) bool {
		aOK := probed[i].rttMs > 0
		bOK := probed[j].rttMs > 0
		if aOK != bOK {
			return aOK
		}
		if aOK && probed[i].rttMs != probed[j].rttMs {
			return probed[i].rttMs < probed[j].rttMs
		}
		return probed[i].order < probed[j].order
	})

	out := make([]pinggyEndpoint, 0, len(probed))
	for _, p := range probed {
		out = append(out, p.ep)
	}
	return out
}

func (pm *PinggyManager) startWithCandidates(candidates []pinggyEndpoint, token string, localPort int) (models.PinggyState, error) {
	if localPort <= 0 || localPort > 65535 {
		return pm.Status(), fmt.Errorf("gecersiz yerel port: %d", localPort)
	}
	token = strings.TrimSpace(token)
	_ = pm.Stop()

	users := []string{"auth+tcp", "tcp"}
	if token != "" {
		users = []string{token + "+auth+tcp", token + "+tcp"}
	}

	var lastErr error
	var lastEp pinggyEndpoint
	for _, ep := range candidates {
		lastEp = ep
		for _, sshUser := range users {
			sshClient, ln, pubAddr, latencyMs, dialErr := dialPinggyTCP(ep.Host, sshUser)
			if dialErr != nil {
				lastErr = dialErr
				continue
			}

			now := time.Now()
			var expiresAt int64
			if token == "" {
				expiresAt = now.Add(60 * time.Minute).Unix()
			}

			ctx, cancel := context.WithCancel(context.Background())

			pm.mu.Lock()
			pm.stopSeq++
			seq := pm.stopSeq
			pm.cancel = cancel
			pm.client = sshClient
			pm.ln = ln
			pm.state = models.PinggyState{
				Running:   true,
				Region:    ep.Region,
				Server:    ep.Host,
				LocalPort: localPort,
				Address:   pubAddr,
				LatencyMs: latencyMs,
				HasToken:  token != "",
				StartedAt: now.Unix(),
				ExpiresAt: expiresAt,
			}
			st := pm.state
			pm.mu.Unlock()

			go pm.keepAliveLoop(ctx, seq, sshClient)
			go pm.acceptLoop(ctx, seq, ln, sshClient, localPort)
			return st, nil
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("pinggy.io sunucusuna baglanilamadi")
	}
	return pm.failState(lastEp.Region, lastEp.Host, localPort, token != "", lastErr)
}

func dialPinggyTCP(host, sshUser string) (*ssh.Client, net.Listener, string, int64, error) {
	t0 := time.Now()
	rawConn, err := net.DialTimeout("tcp", host, 8*time.Second)
	if err != nil {
		return nil, nil, "", 0, fmt.Errorf("pinggy baglantisi kurulamadi (%s): %w", host, err)
	}
	latencyMs := time.Since(t0).Milliseconds()
	if latencyMs < 1 {
		latencyMs = 1
	}
	tuneTCPConn(rawConn)

	sshCfg := &ssh.ClientConfig{
		User: sshUser,
		Auth: []ssh.AuthMethod{
			ssh.Password("nopass"),
			ssh.Password(""),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				ans := make([]string, len(questions))
				for i := range ans {
					ans[i] = "nopass"
				}
				return ans, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         8 * time.Second,
	}

	_ = rawConn.SetDeadline(time.Now().Add(10 * time.Second))
	c, chans, reqs, err := ssh.NewClientConn(rawConn, host, sshCfg)
	_ = rawConn.SetDeadline(time.Time{})
	if err != nil {
		_ = rawConn.Close()
		return nil, nil, "", 0, fmt.Errorf("pinggy SSH el sikismasi basarisiz (%s / %s): %w", host, sshUser, err)
	}
	sshClient := ssh.NewClient(c, chans, reqs)

	ln, err := sshClient.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, "", 0, fmt.Errorf("pinggy TCP yonlendirme acilamadi: %w", err)
	}

	// Step 1: Query internal port config & auth status over Pinggy's control channels if available.
	if portCfg, cfgErr := queryPinggyPortConfig(sshClient); cfgErr == nil && portCfg.StatusPort > 0 {
		if st, stErr := queryPinggyConnStatus(sshClient, portCfg.StatusPort); stErr == nil {
			if !st.Success && strings.TrimSpace(st.Error) != "" {
				_ = ln.Close()
				_ = sshClient.Close()
				return nil, nil, "", 0, fmt.Errorf("pinggy sunucu reddi: %s", strings.TrimSpace(st.Error))
			}
		}
	}

	// Step 2: Query public tunnel address from localhost:4300/urls
	pubAddr := ""
	if urls, uErr := queryPinggyURLs(sshClient); uErr == nil {
		for _, u := range urls {
			if parsed := normalizePinggyPublicAddr(u); parsed != "" {
				pubAddr = parsed
				break
			}
		}
	}

	// Step 3: Fallback to interactive PTY/Shell greeting banner if localhost:4300 didn't return a URL
	if pubAddr == "" {
		pubAddr = queryPinggyURLFromShell(sshClient)
	}

	// Step 4: Final fallback to Listener Addr if non-loopback non-zero port
	if pubAddr == "" && ln.Addr() != nil {
		if h, p, err := net.SplitHostPort(ln.Addr().String()); err == nil && p != "" && p != "0" && h != "" && h != "0.0.0.0" {
			pubAddr = net.JoinHostPort(h, p)
		}
	}

	if pubAddr == "" {
		_ = ln.Close()
		_ = sshClient.Close()
		return nil, nil, "", 0, fmt.Errorf("pinggy genel TCP adresi alinamadi (%s)", host)
	}

	return sshClient, ln, pubAddr, latencyMs, nil
}

func queryPinggyPortConfig(client *ssh.Client) (pinggyPortConfig, error) {
	conn, err := dialSSHWithTimeout(client, "primaryHost:4", 4*time.Second)
	if err != nil {
		return pinggyPortConfig{}, err
	}
	defer conn.Close()

	_, _ = conn.Write([]byte("hello"))
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
	data := readWithShortTimeout(conn, 4*time.Second)
	var cfg pinggyPortConfig
	if err := json.Unmarshal(bytes.TrimSpace(data), &cfg); err != nil {
		return pinggyPortConfig{}, err
	}
	return cfg, nil
}

func queryPinggyConnStatus(client *ssh.Client, statusPort int) (pinggyConnStatus, error) {
	conn, err := dialSSHWithTimeout(client, fmt.Sprintf("localhost:%d", statusPort), 4*time.Second)
	if err != nil {
		return pinggyConnStatus{}, err
	}
	defer conn.Close()

	_, _ = conn.Write([]byte("hello"))
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
	data := readWithShortTimeout(conn, 4*time.Second)
	var st pinggyConnStatus
	if err := json.Unmarshal(bytes.TrimSpace(data), &st); err != nil {
		return pinggyConnStatus{}, err
	}
	return st, nil
}

func queryPinggyURLs(client *ssh.Client) ([]string, error) {
	conn, err := dialSSHWithTimeout(client, "localhost:4300", 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req, err := http.NewRequest(http.MethodGet, "http://localhost:4300/urls", nil)
	if err != nil {
		return nil, err
	}
	req.Close = true
	if err := req.Write(conn); err != nil {
		return nil, err
	}

	type urlResp struct {
		urls []string
		err  error
	}
	ch := make(chan urlResp, 1)
	go func() {
		resp, err := http.ReadResponse(bufio.NewReader(conn), req)
		if err != nil {
			ch <- urlResp{err: err}
			return
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		if err != nil {
			ch <- urlResp{err: err}
			return
		}
		var parsed struct {
			URLs []string `json:"urls"`
		}
		if err := json.Unmarshal(body, &parsed); err != nil {
			ch <- urlResp{err: err}
			return
		}
		ch <- urlResp{urls: parsed.URLs}
	}()

	select {
	case res := <-ch:
		return res.urls, res.err
	case <-time.After(4 * time.Second):
		_ = conn.Close()
		return nil, fmt.Errorf("pinggy /urls yanit zaman asimi")
	}
}

func queryPinggyURLFromShell(client *ssh.Client) string {
	sess, err := client.NewSession()
	if err != nil {
		return ""
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return ""
	}
	_ = sess.RequestPty("xterm", 40, 120, ssh.TerminalModes{})
	if err := sess.Shell(); err != nil {
		return ""
	}

	foundCh := make(chan string, 1)
	go func() {
		buf := make([]byte, 8192)
		var acc string
		for len(acc) < 16384 {
			n, rErr := stdout.Read(buf)
			if n > 0 {
				acc += string(buf[:n])
				if m := pinggyTCPURLRe.FindStringSubmatch(acc); len(m) >= 2 {
					foundCh <- m[1]
					return
				}
				if m := pinggyHostPortRe.FindStringSubmatch(acc); len(m) >= 2 {
					foundCh <- m[1]
					return
				}
			}
			if rErr != nil {
				foundCh <- ""
				return
			}
		}
		foundCh <- ""
	}()

	select {
	case u := <-foundCh:
		return u
	case <-time.After(3500 * time.Millisecond):
		return ""
	}
}

func dialSSHWithTimeout(client *ssh.Client, addr string, timeout time.Duration) (net.Conn, error) {
	type res struct {
		c   net.Conn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := client.Dial("tcp", addr)
		ch <- res{c: c, err: err}
	}()
	select {
	case r := <-ch:
		return r.c, r.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("ssh channel dial timeout (%s)", addr)
	}
}

func readWithShortTimeout(r io.Reader, timeout time.Duration) []byte {
	ch := make(chan []byte, 1)
	go func() {
		b, _ := io.ReadAll(io.LimitReader(r, 16*1024))
		ch <- b
	}()
	select {
	case b := <-ch:
		return b
	case <-time.After(timeout):
		return nil
	}
}

func normalizePinggyPublicAddr(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "tcp://")
	s = strings.TrimSuffix(s, "/")
	if m := pinggyTCPURLRe.FindStringSubmatch(raw); len(m) >= 2 {
		return m[1]
	}
	if h, p, err := net.SplitHostPort(s); err == nil && h != "" && p != "" && p != "0" {
		return net.JoinHostPort(h, p)
	}
	return ""
}

func (pm *PinggyManager) failState(region, server string, localPort int, hasToken bool, err error) (models.PinggyState, error) {
	pm.mu.Lock()
	pm.state = models.PinggyState{
		Running:   false,
		Region:    strings.ToUpper(strings.TrimSpace(region)),
		Server:    server,
		LocalPort: localPort,
		HasToken:  hasToken,
		LastError: err.Error(),
	}
	st := pm.state
	pm.mu.Unlock()
	return st, err
}

func (pm *PinggyManager) keepAliveLoop(ctx context.Context, seq uint64, client *ssh.Client) {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _, err := client.SendRequest("keepalive@openssh.com", true, nil)
			if err != nil {
				pm.mu.Lock()
				if pm.stopSeq == seq && ctx.Err() == nil {
					pm.state.Running = false
					if pm.state.LastError == "" {
						pm.state.LastError = err.Error()
					}
				}
				pm.mu.Unlock()
				_ = client.Close()
				return
			}
		}
	}
}

func (pm *PinggyManager) acceptLoop(ctx context.Context, seq uint64, ln net.Listener, client *ssh.Client, localPort int) {
	for {
		remoteConn, err := ln.Accept()
		if err != nil {
			pm.mu.Lock()
			if pm.stopSeq == seq && ctx.Err() == nil {
				pm.state.Running = false
				if err != io.EOF && pm.state.LastError == "" {
					pm.state.LastError = err.Error()
				}
			}
			pm.mu.Unlock()
			_ = client.Close()
			return
		}
		go handlePinggyForward(remoteConn, localPort)
	}
}

func handlePinggyForward(remoteConn net.Conn, localPort int) {
	localConn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(localPort)), 5*time.Second)
	if err != nil {
		_ = remoteConn.Close()
		return
	}
	tuneTCPConn(localConn)
	pipeBidirectional(localConn, remoteConn)
}
