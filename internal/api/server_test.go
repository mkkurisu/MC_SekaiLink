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
