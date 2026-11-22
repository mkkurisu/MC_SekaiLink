package network

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const p2pRoomAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

type p2pSignalMsg struct {
	MsgID        string   `json:"msgId"`
	Type         string   `json:"type"` // "join_req", "host_offer"
	PeerID       string   `json:"peerId,omitempty"`
	PeerName     string   `json:"peerName,omitempty"`
	TargetPeerID string   `json:"targetPeerId,omitempty"`
	Candidates   []string `json:"candidates,omitempty"`
	NATType      string   `json:"natType,omitempty"`
	PortDelta    int      `json:"portDelta,omitempty"`
	JavaPort     int      `json:"javaPort,omitempty"`
	BedrockPort  int      `json:"bedrockPort,omitempty"`
	WantJava     bool     `json:"wantJava,omitempty"`
	WantBedrock  bool     `json:"wantBedrock,omitempty"`
	CertSHA256   string   `json:"certSha256,omitempty"`
	Timestamp    int64    `json:"ts"`
}

type p2pSignalBus interface {
	Subscribe(ctx context.Context, topicHash string) (<-chan string, error)
	Publish(ctx context.Context, topicHash string, payload string) error
	Status() string
}

// GenerateRoomCode returns an 8-character ambiguity-free room code formatted as XXXX-XXXX.
func GenerateRoomCode() string {
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	var b strings.Builder
	b.Grow(9)
	for i := 0; i < 8; i++ {
		if i == 4 {
			b.WriteByte('-')
		}
		b.WriteByte(p2pRoomAlphabet[int(raw[i])%len(p2pRoomAlphabet)])
	}
	return b.String()
}

// NormalizeRoomCode validates and formats a room code into canonical XXXX-XXXX form.
func NormalizeRoomCode(code string) (string, error) {
	cleaned := strings.ToUpper(strings.TrimSpace(code))
	var stripped strings.Builder
	for i := 0; i < len(cleaned); i++ {
		ch := cleaned[i]
		if ch == '-' || ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			continue
		}
		if strings.IndexByte(p2pRoomAlphabet, ch) < 0 {
			return "", fmt.Errorf("gecersiz oda kodu karakteri '%c' (ornek: K7M4-X9P2)", ch)
		}
		stripped.WriteByte(ch)
	}
	s := stripped.String()
	if len(s) != 8 {
		return "", fmt.Errorf("oda kodu 8 karakter olmalidir (ornek: K7M4-X9P2)")
	}
	return s[:4] + "-" + s[4:], nil
}

func deriveRoomKeys(normCode string) (topicHash string, aesKey [32]byte, punchKey [32]byte) {
	topicDigest := sha256.Sum256([]byte("mcsekailink-p2p-topic-v1:" + normCode))
	topicHash = hex.EncodeToString(topicDigest[:])[:24]
	aesKey = sha256.Sum256([]byte("mcsekailink-p2p-aes-v1:" + normCode))
	punchKey = sha256.Sum256([]byte("mcsekailink-p2p-punch-v1:" + normCode))
	return topicHash, aesKey, punchKey
}

func encryptSignal(aesKey [32]byte, plaintext []byte) (string, error) {
	block, err := aes.NewCipher(aesKey[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func decryptSignal(aesKey [32]byte, encoded string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(aesKey[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce := raw[:gcm.NonceSize()]
	cipherText := raw[gcm.NonceSize():]
	return gcm.Open(nil, nonce, cipherText, nil)
}

func newDefaultSignalBus() p2pSignalBus {
	return &multiSignalBus{
		ntfy: newNtfySignalTransport("https://ntfy.sh"),
		mqtt: newMQTTWssSignalTransport([]string{
			"wss://broker.hivemq.com:8884/mqtt",
			"wss://test.mosquitto.org:8081/mqtt",
		}),
	}
}

type multiSignalBus struct {
	ntfy *ntfySignalTransport
	mqtt *mqttWssSignalTransport
}

func (m *multiSignalBus) Status() string {
	n := m.ntfy.Status()
	q := m.mqtt.Status()
	switch {
	case n != "" && q != "":
		return n + " + " + q
	case n != "":
		return n
	case q != "":
		return q
	default:
		return "connecting"
	}
}

func (m *multiSignalBus) Subscribe(ctx context.Context, topicHash string) (<-chan string, error) {
	out := make(chan string, 64)

	var (
		subWG          sync.WaitGroup
		ntfyCh, mqttCh <-chan string
		ntfyErr        error
		mqttErr        error
	)
	subWG.Add(2)
	go func() {
		defer subWG.Done()
		ntfyCh, ntfyErr = m.ntfy.Subscribe(ctx, topicHash)
	}()
	go func() {
		defer subWG.Done()
		mqttCh, mqttErr = m.mqtt.Subscribe(ctx, topicHash)
	}()
	subWG.Wait()

	if ntfyErr != nil && mqttErr != nil {
		close(out)
		return nil, fmt.Errorf("sinyallesme kanali acilamadi: ntfy=(%v) mqtt=(%v)", ntfyErr, mqttErr)
	}

	var seenMu sync.Mutex
	seen := make(map[string]time.Time)

	forward := func(in <-chan string, wg *sync.WaitGroup) {
		defer wg.Done()
		if in == nil {
			return
		}
		for msg := range in {
			trimmed := strings.TrimSpace(msg)
			if trimmed == "" {
				continue
			}
			now := time.Now()
			seenMu.Lock()
			if len(seen) > 512 {
				for k, ts := range seen {
					if now.Sub(ts) > 2*time.Minute {
						delete(seen, k)
					}
				}
			}
			_, dup := seen[trimmed]
			if !dup {
				seen[trimmed] = now
			}
			seenMu.Unlock()
			if dup {
				continue
			}
			select {
			case out <- trimmed:
			case <-ctx.Done():
				return
			}
		}
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go forward(ntfyCh, &wg)
	go forward(mqttCh, &wg)
	go func() {
		wg.Wait()
		close(out)
	}()

	return out, nil
}

func (m *multiSignalBus) Publish(ctx context.Context, topicHash, payload string) error {
	var wg sync.WaitGroup
	errs := make(chan error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		errs <- m.ntfy.Publish(ctx, topicHash, payload)
	}()
	go func() {
		defer wg.Done()
		errs <- m.mqtt.Publish(ctx, topicHash, payload)
	}()
	wg.Wait()
	close(errs)

	var firstErr error
	for err := range errs {
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// --- ntfy.sh WSS + HTTPS transport ---

type ntfySignalTransport struct {
	baseURL string
	client  *http.Client

	mu     sync.Mutex
	status string
}

func newNtfySignalTransport(baseURL string) *ntfySignalTransport {
	return &ntfySignalTransport{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: 6 * time.Second},
	}
}

func (n *ntfySignalTransport) Status() string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.status
}

func (n *ntfySignalTransport) setStatus(s string) {
	n.mu.Lock()
	n.status = s
	n.mu.Unlock()
}

func (n *ntfySignalTransport) topicName(topicHash string) string {
	return "sklp2p-" + topicHash
}

func (n *ntfySignalTransport) Subscribe(ctx context.Context, topicHash string) (<-chan string, error) {
	wsBase := strings.Replace(n.baseURL, "https://", "wss://", 1)
	wsBase = strings.Replace(wsBase, "http://", "ws://", 1)
	wsURL := fmt.Sprintf("%s/%s/ws", wsBase, n.topicName(topicHash))

	out := make(chan string, 32)
	firstReady := make(chan error, 1)

	go func() {
		defer close(out)
		signaledFirst := false
		backoff := 500 * time.Millisecond

		for {
			if ctx.Err() != nil {
				if !signaledFirst {
					firstReady <- ctx.Err()
				}
				return
			}

			dialer := websocket.Dialer{HandshakeTimeout: 6 * time.Second}
			conn, _, err := dialer.DialContext(ctx, wsURL, nil)
			if err != nil {
				n.setStatus("")
				if !signaledFirst {
					signaledFirst = true
					firstReady <- err
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
					if backoff < 4*time.Second {
						backoff *= 2
					}
					continue
				}
			}

			backoff = 500 * time.Millisecond
			n.setStatus("ntfy.sh WSS")
			if !signaledFirst {
				signaledFirst = true
				firstReady <- nil
			}

			closeDone := make(chan struct{})
			go func(c *websocket.Conn) {
				select {
				case <-ctx.Done():
					_ = c.Close()
				case <-closeDone:
				}
			}(conn)

			for {
				_, raw, rErr := conn.ReadMessage()
				if rErr != nil {
					break
				}
				var ev struct {
					Event   string `json:"event"`
					Message string `json:"message"`
				}
				if json.Unmarshal(raw, &ev) == nil && ev.Event == "message" && ev.Message != "" {
					select {
					case out <- ev.Message:
					case <-ctx.Done():
						break
					}
				}
			}
			close(closeDone)
			_ = conn.Close()
			n.setStatus("")

			select {
			case <-ctx.Done():
				return
			case <-time.After(400 * time.Millisecond):
			}
		}
	}()

	select {
	case err := <-firstReady:
		if err != nil {
			// Keep channel open; reconnect loop runs in background if another bus succeeded
			return out, err
		}
		return out, nil
	case <-time.After(4 * time.Second):
		return out, nil
	case <-ctx.Done():
		return out, ctx.Err()
	}
}

func (n *ntfySignalTransport) Publish(ctx context.Context, topicHash, payload string) error {
	url := fmt.Sprintf("%s/%s", n.baseURL, n.topicName(topicHash))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("ntfy publish HTTP %d", resp.StatusCode)
	}
	return nil
}

// --- Minimal pure-Go MQTT 3.1.1 over WSS transport ---

type mqttWssSignalTransport struct {
	brokers []string

	mu       sync.Mutex
	active   *websocket.Conn
	writeMu  sync.Mutex
	status   string
}

func newMQTTWssSignalTransport(brokers []string) *mqttWssSignalTransport {
	return &mqttWssSignalTransport{brokers: brokers}
}

func (m *mqttWssSignalTransport) Status() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *mqttWssSignalTransport) setActive(conn *websocket.Conn, status string) {
	m.mu.Lock()
	m.active = conn
	m.status = status
	m.mu.Unlock()
}

func (m *mqttWssSignalTransport) getActive() *websocket.Conn {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}

func (m *mqttWssSignalTransport) mqttTopic(topicHash string) string {
	return "mcsekailink/p2p/v1/" + topicHash
}

func (m *mqttWssSignalTransport) Subscribe(ctx context.Context, topicHash string) (<-chan string, error) {
	out := make(chan string, 32)
	topic := m.mqttTopic(topicHash)
	firstReady := make(chan error, 1)

	go func() {
		defer close(out)
		signaledFirst := false
		idx := 0

		for {
			if ctx.Err() != nil {
				if !signaledFirst {
					firstReady <- ctx.Err()
				}
				return
			}
			broker := m.brokers[idx%len(m.brokers)]
			idx++

			conn, err := dialMQTTWSS(ctx, broker)
			if err != nil {
				m.setActive(nil, "")
				if !signaledFirst && idx >= len(m.brokers) {
					signaledFirst = true
					firstReady <- err
				}
				select {
				case <-ctx.Done():
					return
				case <-time.After(700 * time.Millisecond):
					continue
				}
			}

			subPkt := encodeMQTTSubscribe(1, topic)
			_ = conn.SetWriteDeadline(time.Now().Add(4 * time.Second))
			err = conn.WriteMessage(websocket.BinaryMessage, subPkt)
			_ = conn.SetWriteDeadline(time.Time{})
			if err != nil {
				_ = conn.Close()
				continue
			}

			m.setActive(conn, "MQTT WSS")
			if !signaledFirst {
				signaledFirst = true
				firstReady <- nil
			}

			pingCtx, cancelPing := context.WithCancel(ctx)
			go func(c *websocket.Conn) {
				ticker := time.NewTicker(25 * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-pingCtx.Done():
						return
					case <-ticker.C:
						m.writeMu.Lock()
						_ = c.SetWriteDeadline(time.Now().Add(4 * time.Second))
						pErr := c.WriteMessage(websocket.BinaryMessage, []byte{0xC0, 0x00})
						_ = c.SetWriteDeadline(time.Time{})
						m.writeMu.Unlock()
						if pErr != nil {
							return
						}
					}
				}
			}(conn)

			go func(c *websocket.Conn) {
				<-pingCtx.Done()
				_ = c.Close()
			}(conn)

			for {
				msgType, data, rErr := conn.ReadMessage()
				if rErr != nil {
					break
				}
				if msgType != websocket.BinaryMessage && msgType != websocket.TextMessage {
					continue
				}
				for _, payload := range decodeMQTTPublishBatch(data, topic) {
					select {
					case out <- payload:
					case <-ctx.Done():
						break
					}
				}
			}

			cancelPing()
			m.setActive(nil, "")
			_ = conn.Close()

			select {
			case <-ctx.Done():
				return
			case <-time.After(600 * time.Millisecond):
			}
		}
	}()

	select {
	case err := <-firstReady:
		return out, err
	case <-time.After(4 * time.Second):
		return out, nil
	case <-ctx.Done():
		return out, ctx.Err()
	}
}

func (m *mqttWssSignalTransport) Publish(ctx context.Context, topicHash, payload string) error {
	topic := m.mqttTopic(topicHash)
	pkt := encodeMQTTPublish(topic, []byte(payload))

	if active := m.getActive(); active != nil {
		m.writeMu.Lock()
		_ = active.SetWriteDeadline(time.Now().Add(4 * time.Second))
		err := active.WriteMessage(websocket.BinaryMessage, pkt)
		_ = active.SetWriteDeadline(time.Time{})
		m.writeMu.Unlock()
		if err == nil {
			return nil
		}
	}

	var lastErr error
	for _, broker := range m.brokers {
		conn, err := dialMQTTWSS(ctx, broker)
		if err != nil {
			lastErr = err
			continue
		}
		_ = conn.SetWriteDeadline(time.Now().Add(4 * time.Second))
		err = conn.WriteMessage(websocket.BinaryMessage, pkt)
		if err == nil {
			_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0xE0, 0x00}) // DISCONNECT
		}
		_ = conn.Close()
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return lastErr
}

func dialMQTTWSS(ctx context.Context, brokerURL string) (*websocket.Conn, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 6 * time.Second,
		Subprotocols:     []string{"mqtt"},
	}
	conn, _, err := dialer.DialContext(ctx, brokerURL, nil)
	if err != nil {
		return nil, err
	}

	var rnd [4]byte
	_, _ = rand.Read(rnd[:])
	clientID := "skl-" + hex.EncodeToString(rnd[:])
	connectPkt := encodeMQTTConnect(clientID)

	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := conn.WriteMessage(websocket.BinaryMessage, connectPkt); err != nil {
		_ = conn.Close()
		return nil, err
	}
	_ = conn.SetWriteDeadline(time.Time{})
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, ack, err := conn.ReadMessage()
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil || len(ack) < 4 || ack[0]&0xF0 != 0x20 || ack[3] != 0x00 {
		_ = conn.Close()
		if err == nil {
			err = fmt.Errorf("mqtt connack rejected")
		}
		return nil, err
	}
	return conn, nil
}

func encodeMQTTRemLength(length int) []byte {
	var out []byte
	for {
		enc := byte(length % 128)
		length /= 128
		if length > 0 {
			enc |= 0x80
		}
		out = append(out, enc)
		if length == 0 {
			break
		}
	}
	return out
}

func decodeMQTTRemLength(b []byte) (length int, consumed int, ok bool) {
	multiplier := 1
	for i := 0; i < 4 && i < len(b); i++ {
		length += int(b[i]&0x7F) * multiplier
		if b[i]&0x80 == 0 {
			return length, i + 1, true
		}
		multiplier *= 128
	}
	return 0, 0, false
}

func encodeMQTTString(s string) []byte {
	out := make([]byte, 2+len(s))
	binary.BigEndian.PutUint16(out[0:2], uint16(len(s)))
	copy(out[2:], s)
	return out
}

func encodeMQTTConnect(clientID string) []byte {
	var body bytes.Buffer
	body.Write(encodeMQTTString("MQTT"))
	body.WriteByte(0x04) // MQTT 3.1.1
	body.WriteByte(0x02) // Clean Session
	body.Write([]byte{0x00, 0x3C}) // KeepAlive 60s
	body.Write(encodeMQTTString(clientID))

	var pkt bytes.Buffer
	pkt.WriteByte(0x10)
	pkt.Write(encodeMQTTRemLength(body.Len()))
	pkt.Write(body.Bytes())
	return pkt.Bytes()
}

func encodeMQTTSubscribe(packetID uint16, topic string) []byte {
	var body bytes.Buffer
	var pid [2]byte
	binary.BigEndian.PutUint16(pid[:], packetID)
	body.Write(pid[:])
	body.Write(encodeMQTTString(topic))
	body.WriteByte(0x00) // QoS 0

	var pkt bytes.Buffer
	pkt.WriteByte(0x82)
	pkt.Write(encodeMQTTRemLength(body.Len()))
	pkt.Write(body.Bytes())
	return pkt.Bytes()
}

func encodeMQTTPublish(topic string, payload []byte) []byte {
	var body bytes.Buffer
	body.Write(encodeMQTTString(topic))
	body.Write(payload)

	var pkt bytes.Buffer
	pkt.WriteByte(0x30) // PUBLISH QoS 0
	pkt.Write(encodeMQTTRemLength(body.Len()))
	pkt.Write(body.Bytes())
	return pkt.Bytes()
}

func decodeMQTTPublishBatch(data []byte, wantTopic string) []string {
	var out []string
	for len(data) >= 2 {
		hdr := data[0]
		remLen, nBytes, ok := decodeMQTTRemLength(data[1:])
		if !ok {
			break
		}
		totalLen := 1 + nBytes + remLen
		if len(data) < totalLen {
			break
		}
		body := data[1+nBytes : totalLen]
		data = data[totalLen:]

		if hdr&0xF0 != 0x30 || len(body) < 2 {
			continue
		}
		topicLen := int(binary.BigEndian.Uint16(body[0:2]))
		if len(body) < 2+topicLen {
			continue
		}
		topic := string(body[2 : 2+topicLen])
		pos := 2 + topicLen
		qos := (hdr >> 1) & 0x03
		if qos > 0 {
			if len(body) < pos+2 {
				continue
			}
			pos += 2
		}
		if topic == wantTopic && pos <= len(body) {
			out = append(out, string(body[pos:]))
		}
	}
	return out
}
