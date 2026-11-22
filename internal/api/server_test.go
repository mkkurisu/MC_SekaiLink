package api

import (
	"bytes"
	"encoding/json"
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

func TestAPIP2PAndPinggyStatusRoutes(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := engine.NewManager(store)
	playit := network.NewPlayitManager(store.DataDir(), store.BinDir())
	srv := NewServer(store, mgr, playit, fstest.MapFS{"index.html": {Data: []byte("<html>ok</html>")}})

	// 1. P2P idle status
	reqP2P := httptest.NewRequest(http.MethodGet, "/api/network/p2p/status", nil)
	reqP2P.RemoteAddr = "127.0.0.1:54321"
	recP2P := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recP2P, reqP2P)
	if recP2P.Code != http.StatusOK {
		t.Fatalf("GET p2p status failed: %d %s", recP2P.Code, recP2P.Body.String())
	}
	var p2pSt models.P2PState
	if err := json.Unmarshal(recP2P.Body.Bytes(), &p2pSt); err != nil || p2pSt.Running {
		t.Fatalf("unexpected p2p status: %+v (%v)", p2pSt, err)
	}

	// 2. Pinggy idle status
	reqPinggy := httptest.NewRequest(http.MethodGet, "/api/network/pinggy/status", nil)
	reqPinggy.RemoteAddr = "127.0.0.1:54321"
	recPinggy := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recPinggy, reqPinggy)
	if recPinggy.Code != http.StatusOK {
		t.Fatalf("GET pinggy status failed: %d %s", recPinggy.Code, recPinggy.Body.String())
	}
	var pgSt models.PinggyState
	if err := json.Unmarshal(recPinggy.Body.Bytes(), &pgSt); err != nil || pgSt.Running {
		t.Fatalf("unexpected pinggy status: %+v (%v)", pgSt, err)
	}

	// 3. Reject invalid P2P room code immediately with 400
	reqBadJoin := httptest.NewRequest(http.MethodPost, "/api/network/p2p/join", bytes.NewReader([]byte(`{"roomCode":"BAD"}`)))
	reqBadJoin.RemoteAddr = "127.0.0.1:54321"
	recBadJoin := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recBadJoin, reqBadJoin)
	if recBadJoin.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on invalid roomCode, got %d: %s", recBadJoin.Code, recBadJoin.Body.String())
	}

	// 4. P2P stop works cleanly when idle
	reqStop := httptest.NewRequest(http.MethodPost, "/api/network/p2p/stop", nil)
	reqStop.RemoteAddr = "127.0.0.1:54321"
	recStop := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recStop, reqStop)
	if recStop.Code != http.StatusOK {
		t.Fatalf("POST p2p stop failed: %d", recStop.Code)
	}
}
