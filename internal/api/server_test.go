package api

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"github.com/MC_SekaiLink/mc-sekailink/internal/network"
	"github.com/MC_SekaiLink/mc-sekailink/internal/storage"
)

func TestAPIHealthAuthAndServerCRUD(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := engine.NewManager(store)
	playit := network.NewPlayitManager(store.DataDir(), store.BinDir())
	staticFS := fstest.MapFS{
		"index.html": {Data: []byte("<html>ok</html>")},
	}
	srv := NewServer(store, mgr, playit, staticFS)

	// 1. Loopback health check
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 health, got %d", rec.Code)
	}

	// 2. Remote request without token must be rejected
	reqRemote := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	reqRemote.RemoteAddr = "192.168.1.50:54321"
	recRemote := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recRemote, reqRemote)
	if recRemote.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated remote request, got %d", recRemote.Code)
	}

	// 3. Remote request with valid token succeeds
	reqRemoteOK := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	reqRemoteOK.RemoteAddr = "192.168.1.50:54321"
	reqRemoteOK.Header.Set("X-Sekai-Token", store.GetConfig().PairingToken)
	recRemoteOK := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recRemoteOK, reqRemoteOK)
	if recRemoteOK.Code != http.StatusOK {
		t.Fatalf("expected 200 for authenticated remote request, got %d", recRemoteOK.Code)
	}

	// 4. Create and list server locally
	body, _ := json.Marshal(models.ServerInstance{
		Name:         "API Test Server",
		Engine:       models.EnginePaper,
		MCVersion:    "1.21.4",
		MemoryMB:     2048,
		EULAAccepted: true,
	})
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/servers", bytes.NewReader(body))
	reqCreate.RemoteAddr = "127.0.0.1:54321"
	recCreate := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recCreate, reqCreate)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("expected 201 create, got %d: %s", recCreate.Code, recCreate.Body.String())
	}
}

func TestAPIAutoStartTunnelWithVDSBridge(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := engine.NewManager(store)
	playit := network.NewPlayitManager(store.DataDir(), store.BinDir())
	srv := NewServer(store, mgr, playit, fstest.MapFS{"index.html": {Data: []byte("<html>ok</html>")}})

	// Start local TLS 1.3 VDS Relay (SV-Oracle)
	tlsCfg, pin, err := network.GenerateSelfSignedRelayTLS()
	if err != nil {
		t.Fatal(err)
	}
	relayLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	relaySrv := network.NewRelayServerWithOptions(network.RelayServerOptions{
		NodeID:     "SV-Oracle",
		PublicHost: "127.0.0.1",
		Secret:     "sec123",
		TLSConfig:  tlsCfg,
	})
	defer relaySrv.Close()
	go func() { _ = relaySrv.Serve(relayLn) }()

	// Save VDS node pool via PUT /api/network/relay/nodes
	nodesBody, _ := json.Marshal(map[string]any{
		"nodes": []models.RelayNode{{
			ID:      "SV-Oracle",
			Address: relayLn.Addr().String(),
			Secret:  "sec123",
			TLS:     true,
			CertPin: pin,
			Enabled: true,
		}},
	})
	reqSave := httptest.NewRequest(http.MethodPut, "/api/network/relay/nodes", bytes.NewReader(nodesBody))
	reqSave.RemoteAddr = "127.0.0.1:54321"
	recSave := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recSave, reqSave)
	if recSave.Code != http.StatusOK {
		t.Fatalf("PUT relay nodes failed: %d %s", recSave.Code, recSave.Body.String())
	}

	// Probe via GET /api/network/relay/probe
	reqProbe := httptest.NewRequest(http.MethodGet, "/api/network/relay/probe", nil)
	reqProbe.RemoteAddr = "127.0.0.1:54321"
	recProbe := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recProbe, reqProbe)
	if recProbe.Code != http.StatusOK {
		t.Fatalf("GET relay probe failed: %d", recProbe.Code)
	}
	var probes []models.RelayNodeProbe
	if err := json.Unmarshal(recProbe.Body.Bytes(), &probes); err != nil || len(probes) != 1 || !probes[0].Online || probes[0].NodeID != "SV-Oracle" {
		t.Fatalf("unexpected probes: %s (%v)", recProbe.Body.String(), err)
	}

	// Auto-start via POST /api/network/tunnel/auto-start
	autoBody := []byte(`{"localJavaPort":25565,"localBedrockPort":19132,"wantJava":true,"wantBedrock":true}`)
	reqAuto := httptest.NewRequest(http.MethodPost, "/api/network/tunnel/auto-start", bytes.NewReader(autoBody))
	reqAuto.RemoteAddr = "127.0.0.1:54321"
	recAuto := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recAuto, reqAuto)
	if recAuto.Code != http.StatusOK {
		t.Fatalf("POST auto-start failed: %d %s", recAuto.Code, recAuto.Body.String())
	}
	var autoRes models.AutoTunnelResult
	if err := json.Unmarshal(recAuto.Body.Bytes(), &autoRes); err != nil {
		t.Fatal(err)
	}
	if autoRes.Provider != "relay" || autoRes.NodeID != "SV-Oracle" || !autoRes.Encrypted || autoRes.JavaAddress == "" || autoRes.BedrockAddress == "" {
		t.Fatalf("unexpected auto-start response: %+v", autoRes)
	}
	_ = srv.relay.Stop()
}
