package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/addons"
	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"github.com/MC_SekaiLink/mc-sekailink/internal/network"
	"github.com/MC_SekaiLink/mc-sekailink/internal/storage"
	"github.com/MC_SekaiLink/mc-sekailink/internal/worlds"
	"github.com/gorilla/websocket"
)

type Server struct {
	store    *storage.Store
	manager  *engine.Manager
	playit   *network.PlayitManager
	bore     *network.BoreManager
	e4mc     *network.E4mcManager
	pinggy   *network.PinggyManager
	p2p      *network.P2PManager
	staticFS fs.FS

	upgrader websocket.Upgrader
	handler  http.Handler

	srvMu      sync.Mutex
	httpServer *http.Server
	actualAddr string
}

func NewServer(store *storage.Store, manager *engine.Manager, playit *network.PlayitManager, staticFS fs.FS) *Server {
	s := &Server{
		store:    store,
		manager:  manager,
		playit:   playit,
		bore:     network.NewBoreManager(),
		e4mc:     network.NewE4mcManager(),
		pinggy:   network.NewPinggyManager(),
		p2p:      network.NewP2PManager(),
		staticFS: staticFS,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
			CheckOrigin:     func(_ *http.Request) bool { return true },
		},
	}
	s.handler = s.withMiddleware(s.routes())
	return s
}

func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) ActualURL() string {
	s.srvMu.Lock()
	defer s.srvMu.Unlock()
	if s.actualAddr == "" {
		cfg := s.store.GetConfig()
		return fmt.Sprintf("http://127.0.0.1:%d", cfg.APIPort)
	}
	_, port, err := net.SplitHostPort(s.actualAddr)
	if err != nil {
		return "http://" + s.actualAddr
	}
	return "http://127.0.0.1:" + port
}

func (s *Server) StartListening() error {
	cfg := s.store.GetConfig()
	bindHost := cfg.BindAddress
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	port := cfg.APIPort
	if port <= 0 {
		port = 25580
	}
	addr := fmt.Sprintf("%s:%d", bindHost, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 15 * time.Second,
	}

	s.srvMu.Lock()
	s.httpServer = srv
	s.actualAddr = ln.Addr().String()
	s.srvMu.Unlock()

	go func() {
		_ = srv.Serve(ln)
	}()
	return nil
}

func (s *Server) RestartListening() error {
	_ = s.Shutdown(context.Background())
	return s.StartListening()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if s.p2p != nil {
		_ = s.p2p.Stop()
	}
	if s.e4mc != nil {
		_ = s.e4mc.Stop()
	}
	if s.pinggy != nil {
		_ = s.pinggy.Stop()
	}
	if s.bore != nil {
		_ = s.bore.Stop()
	}
	s.srvMu.Lock()
	srv := s.httpServer
	s.httpServer = nil
	s.srvMu.Unlock()
	if srv != nil {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return srv.Shutdown(cctx)
	}
	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	// Health & System
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/system", s.handleSystem)
	mux.HandleFunc("POST /api/system/install-java", s.handleInstallJava)

	// App Config & Pairing
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handleUpdateConfig)
	mux.HandleFunc("POST /api/config/regenerate-token", s.handleRegenerateToken)

	// Catalog
	mux.HandleFunc("GET /api/catalog/engines", s.handleCatalogEngines)
	mux.HandleFunc("GET /api/catalog/versions/{engine}", s.handleCatalogVersions)

	// Servers CRUD & Lifecycle
	mux.HandleFunc("GET /api/servers", s.handleListServers)
	mux.HandleFunc("POST /api/servers", s.handleCreateServer)
	mux.HandleFunc("GET /api/servers/{id}", s.handleGetServer)
	mux.HandleFunc("PUT /api/servers/{id}", s.handleUpdateServer)
	mux.HandleFunc("DELETE /api/servers/{id}", s.handleDeleteServer)
	mux.HandleFunc("POST /api/servers/{id}/install", s.handleInstallServer)
	mux.HandleFunc("POST /api/servers/{id}/start", s.handleStartServer)
	mux.HandleFunc("POST /api/servers/{id}/stop", s.handleStopServer)
	mux.HandleFunc("POST /api/servers/{id}/restart", s.handleRestartServer)
	mux.HandleFunc("POST /api/servers/{id}/command", s.handleSendCommand)
	mux.HandleFunc("GET /api/servers/{id}/metrics", s.handleGetMetrics)
	mux.HandleFunc("GET /api/servers/{id}/players", s.handleGetPlayers)
	mux.HandleFunc("GET /api/servers/{id}/properties", s.handleGetProperties)
	mux.HandleFunc("PUT /api/servers/{id}/properties", s.handleUpdateProperties)
	mux.HandleFunc("POST /api/servers/{id}/optimize-lag", s.handleOptimizeServerLag)

	// Modrinth & Addons
	mux.HandleFunc("GET /api/modrinth/search", s.handleModrinthSearch)
	mux.HandleFunc("GET /api/modrinth/project/{projectId}/versions", s.handleModrinthVersions)
	mux.HandleFunc("POST /api/servers/{id}/addons/install-modrinth", s.handleInstallModrinth)
	mux.HandleFunc("GET /api/servers/{id}/addons", s.handleListAddons)
	mux.HandleFunc("POST /api/servers/{id}/addons/upload", s.handleUploadAddon)
	mux.HandleFunc("POST /api/servers/{id}/addons/toggle", s.handleToggleAddon)
	mux.HandleFunc("DELETE /api/servers/{id}/addons", s.handleDeleteAddon)

	// Packs & Shaders
	mux.HandleFunc("GET /api/servers/{id}/packs", s.handleListPacks)
	mux.HandleFunc("POST /api/servers/{id}/packs/upload", s.handleUploadPack)
	mux.HandleFunc("POST /api/servers/{id}/packs/apply", s.handleApplyPack)
	mux.HandleFunc("GET /api/servers/{id}/packs/export-mrpack", s.handleExportMRPack)
	mux.HandleFunc("GET /packs/{id}/{filename}", s.handleServePackFile)

	// Worlds & Backups
	mux.HandleFunc("GET /api/servers/{id}/worlds", s.handleListWorlds)
	mux.HandleFunc("POST /api/servers/{id}/worlds/import", s.handleImportWorld)
	mux.HandleFunc("POST /api/servers/{id}/worlds/switch", s.handleSwitchWorld)
	mux.HandleFunc("GET /api/servers/{id}/worlds/{name}/export", s.handleExportWorld)
	mux.HandleFunc("DELETE /api/servers/{id}/worlds/{name}", s.handleDeleteWorld)
	mux.HandleFunc("GET /api/servers/{id}/backups", s.handleListBackups)
	mux.HandleFunc("POST /api/servers/{id}/backups", s.handleCreateBackup)
	mux.HandleFunc("POST /api/servers/{id}/backups/{filename}/restore", s.handleRestoreBackup)
	mux.HandleFunc("DELETE /api/servers/{id}/backups/{filename}", s.handleDeleteBackup)

	// Network, UPnP, Direct P2P, e4mc, pinggy.io, bore.pub & playit.gg
	mux.HandleFunc("GET /api/network/diagnostics", s.handleNetworkDiagnostics)
	mux.HandleFunc("POST /api/network/upnp/map", s.handleUPnPMap)
	mux.HandleFunc("POST /api/network/upnp/unmap", s.handleUPnPUnmap)
	mux.HandleFunc("GET /api/network/p2p/status", s.handleP2PStatus)
	mux.HandleFunc("POST /api/network/p2p/host", s.handleP2PHost)
	mux.HandleFunc("POST /api/network/p2p/join", s.handleP2PJoin)
	mux.HandleFunc("POST /api/network/p2p/stop", s.handleP2PStop)
	mux.HandleFunc("GET /api/network/playit/status", s.handlePlayitStatus)
	mux.HandleFunc("POST /api/network/playit/install", s.handlePlayitInstall)
	mux.HandleFunc("POST /api/network/playit/start", s.handlePlayitStart)
	mux.HandleFunc("POST /api/network/playit/stop", s.handlePlayitStop)
	mux.HandleFunc("GET /api/network/e4mc/status", s.handleE4mcStatus)
	mux.HandleFunc("POST /api/network/e4mc/start", s.handleE4mcStart)
	mux.HandleFunc("POST /api/network/e4mc/stop", s.handleE4mcStop)
	mux.HandleFunc("GET /api/network/pinggy/status", s.handlePinggyStatus)
	mux.HandleFunc("POST /api/network/pinggy/start", s.handlePinggyStart)
	mux.HandleFunc("POST /api/network/pinggy/stop", s.handlePinggyStop)
	mux.HandleFunc("GET /api/network/bore/status", s.handleBoreStatus)
	mux.HandleFunc("POST /api/network/bore/start", s.handleBoreStart)
	mux.HandleFunc("POST /api/network/bore/stop", s.handleBoreStop)
	mux.HandleFunc("POST /api/network/tunnel/auto-start", s.handleAutoStartTunnel)

	// WebSocket Console
	mux.HandleFunc("GET /api/ws/console/{id}", s.handleWSConsole)

	// SPA Static Files
	mux.Handle("/", s.spaHandler())
	return mux
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Sekai-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Expose-Headers", "X-Sekai-Live-Applied")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Protect API endpoints when accessed from non-loopback hosts
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/health" {
			if !isLoopbackRequest(r) {
				expected := s.store.GetConfig().PairingToken
				provided := extractToken(r)
				if expected == "" || provided != expected {
					writeError(w, http.StatusUnauthorized, "yetkisiz erisim: gecerli eslestirme tokeni gerekli")
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

func isLoopbackRequest(r *http.Request) bool {
	if r.Header.Get("X-Forwarded-For") != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

func extractToken(r *http.Request) string {
	if t := strings.TrimSpace(r.Header.Get("X-Sekai-Token")); t != "" {
		return t
	}
	if auth := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(auth), "bearer ") {
		return strings.TrimSpace(auth[7:])
	}
	return strings.TrimSpace(r.URL.Query().Get("token"))
}

func (s *Server) spaHandler() http.Handler {
	if s.staticFS == nil {
		return http.NotFoundHandler()
	}
	fileServer := http.FileServer(http.FS(s.staticFS))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/packs/") {
			http.NotFound(w, r)
			return
		}
		clean := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+r.URL.Path)), "/")
		if clean == "" {
			clean = "index.html"
		}
		if f, err := s.staticFS.Open(clean); err == nil {
			_ = f.Close()
			fileServer.ServeHTTP(w, r)
			return
		}
		// Client-side route fallback
		raw, err := fs.ReadFile(s.staticFS, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(raw)
	})
}

// --- Handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"app":     "MC SekaiLink",
		"version": "0.1.0",
		"time":    time.Now().UTC(),
	})
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.GetConfig()
	javaInfo := engine.DetectJava(cfg.JavaPath, s.store.DataDir())
	ov := engine.CollectSystemOverview(s.store.DataDir(), javaInfo)
	writeJSON(w, http.StatusOK, ov)
}

func (s *Server) handleInstallJava(w http.ResponseWriter, r *http.Request) {
	major := 25
	if m, err := strconv.Atoi(r.URL.Query().Get("major")); err == nil && m >= 8 {
		major = m
	}
	info, err := engine.InstallManagedJRE(r.Context(), s.store.DataDir(), major, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, _ = s.store.UpdateConfig(func(cfg *models.AppConfig) {
		cfg.JavaPath = info.Path
	})
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.GetConfig())
}

func (s *Server) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	var body struct {
		APIPort         *int    `json:"apiPort"`
		RemoteEnabled   *bool   `json:"remoteEnabled"`
		Language        *string `json:"language"`
		JavaPath        *string `json:"javaPath"`
		PlayitAutoStart *bool   `json:"playitAutoStart"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	prev := s.store.GetConfig()
	updated, err := s.store.UpdateConfig(func(cfg *models.AppConfig) {
		if body.APIPort != nil && *body.APIPort > 1024 && *body.APIPort <= 65535 {
			cfg.APIPort = *body.APIPort
		}
		if body.RemoteEnabled != nil {
			cfg.RemoteEnabled = *body.RemoteEnabled
		}
		if body.Language != nil && (*body.Language == "tr" || *body.Language == "en") {
			cfg.Language = *body.Language
		}
		if body.JavaPath != nil {
			cfg.JavaPath = strings.TrimSpace(*body.JavaPath)
		}
		if body.PlayitAutoStart != nil {
			cfg.PlayitAutoStart = *body.PlayitAutoStart
		}
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if prev.BindAddress != updated.BindAddress || prev.APIPort != updated.APIPort {
		go func() {
			time.Sleep(150 * time.Millisecond)
			_ = s.RestartListening()
		}()
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleRegenerateToken(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.store.RegenerateToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleCatalogEngines(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, engine.ListEngineDescriptors())
}

func (s *Server) handleCatalogVersions(w http.ResponseWriter, r *http.Request) {
	eng := models.EngineType(r.PathValue("engine"))
	versions, err := engine.FetchVersions(r.Context(), eng)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.store.ListInstances())
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var req models.ServerInstance
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	inst, err := s.manager.CreateServer(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, err := s.store.GetInstance(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var body struct {
		Name         *string                 `json:"name"`
		MemoryMB     *int                    `json:"memoryMB"`
		JVMPreset    *models.JVMPreset       `json:"jvmPreset"`
		JavaPort     *int                    `json:"javaPort"`
		BedrockPort  *int                    `json:"bedrockPort"`
		AutoRestart  *bool                   `json:"autoRestart"`
		OfflineMode  *bool                   `json:"offlineMode"`
		EULAAccepted *bool                   `json:"eulaAccepted"`
		JavaPath     *string                 `json:"javaPath"`
		CrossPlay    *models.CrossPlayConfig `json:"crossPlay"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
		inst.Name = strings.TrimSpace(*body.Name)
	}
	if body.MemoryMB != nil && *body.MemoryMB >= 512 {
		inst.MemoryMB = *body.MemoryMB
	}
	if body.JVMPreset != nil {
		inst.JVMPreset = *body.JVMPreset
	}
	if body.JavaPort != nil && *body.JavaPort > 0 {
		inst.JavaPort = *body.JavaPort
	}
	if body.BedrockPort != nil && *body.BedrockPort > 0 {
		inst.BedrockPort = *body.BedrockPort
	}
	if body.AutoRestart != nil {
		inst.AutoRestart = *body.AutoRestart
	}
	if body.OfflineMode != nil {
		inst.OfflineMode = *body.OfflineMode
	}
	if body.EULAAccepted != nil {
		inst.EULAAccepted = *body.EULAAccepted
	}
	if body.JavaPath != nil {
		inst.JavaPath = strings.TrimSpace(*body.JavaPath)
	}
	if body.CrossPlay != nil {
		inst.CrossPlay = *body.CrossPlay
	}
	if err := s.store.SaveInstance(inst); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if body.OfflineMode != nil {
		_ = engine.SyncOfflineMode(inst)
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, err := s.store.GetInstance(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if s.manager.IsRunning(id) {
		_ = s.manager.Stop(id)
	}
	if err := s.store.DeleteInstance(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if inst.Path != "" {
		_ = os.RemoveAll(inst.Path)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleInstallServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetInstance(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	syncMode := r.URL.Query().Get("sync") == "true"
	if syncMode {
		if err := s.manager.InstallServer(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		inst, _ := s.store.GetInstance(id)
		writeJSON(w, http.StatusOK, inst)
		return
	}
	go func() {
		_ = s.manager.InstallServer(context.Background(), id)
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "installing"})
}

func (s *Server) handleStartServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if inst, err := s.store.GetInstance(id); err == nil {
		s.warnAddonIssuesToConsole(inst)
	}
	if err := s.manager.Start(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	inst, _ := s.store.GetInstance(id)
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleStopServer(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.Stop(r.PathValue("id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	inst, _ := s.store.GetInstance(r.PathValue("id"))
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleRestartServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if inst, err := s.store.GetInstance(id); err == nil {
		s.warnAddonIssuesToConsole(inst)
	}
	if err := s.manager.Restart(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	inst, _ := s.store.GetInstance(id)
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) warnAddonIssuesToConsole(inst models.ServerInstance) {
	list, err := addons.ListInstalledAddons(inst)
	if err != nil {
		return
	}
	for _, item := range list {
		if !item.Enabled {
			continue
		}
		if len(item.MissingDeps) > 0 {
			s.manager.AppendConsoleWarning(inst.ID, fmt.Sprintf("[MC SekaiLink] UYARI: '%s' icin eksik bagimlilik(lar): %s", item.Name, strings.Join(item.MissingDeps, ", ")))
		}
		for _, wMsg := range item.Warnings {
			s.manager.AppendConsoleWarning(inst.ID, fmt.Sprintf("[MC SekaiLink] UYARI: '%s' -> %s", item.Name, wMsg))
		}
	}
}

func (s *Server) handleSendCommand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.manager.SendCommand(r.PathValue("id"), body.Command); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"sent": true})
}

func (s *Server) handleGetMetrics(w http.ResponseWriter, r *http.Request) {
	m, err := s.manager.GetMetrics(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleGetPlayers(w http.ResponseWriter, r *http.Request) {
	m, err := s.manager.GetMetrics(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, m.Players)
}

func (s *Server) handleGetProperties(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	props, err := engine.ReadProperties(inst.Path)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, props)
}

func (s *Server) handleUpdateProperties(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var props map[string]string
	if err := json.NewDecoder(r.Body).Decode(&props); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	prevProps, _ := engine.ReadProperties(inst.Path)
	if err := engine.WriteProperties(inst.Path, props); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	changed := false
	if lvl := strings.TrimSpace(props["level-name"]); lvl != "" && lvl != inst.ActiveWorld {
		inst.ActiveWorld = lvl
		changed = true
	}
	if inst.Engine == models.EnginePowerNukkitX {
		if xa, ok := props["xbox-auth"]; ok {
			inst.OfflineMode = strings.EqualFold(strings.TrimSpace(xa), "off") || strings.EqualFold(strings.TrimSpace(xa), "false")
			changed = true
		}
	} else if om, ok := props["online-mode"]; ok {
		inst.OfflineMode = strings.EqualFold(strings.TrimSpace(om), "false")
		changed = true
	}
	if changed {
		_ = s.store.SaveInstance(inst)
		_ = engine.SyncOfflineMode(inst)
	}
	liveApplied := s.manager.ApplyLiveProperties(inst.ID, prevProps, props)
	if len(liveApplied) > 0 {
		w.Header().Set("X-Sekai-Live-Applied", strings.Join(liveApplied, ","))
	}
	if r.URL.Query().Get("withMeta") == "true" {
		if liveApplied == nil {
			liveApplied = []string{}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"properties":  props,
			"liveApplied": liveApplied,
		})
		return
	}
	writeJSON(w, http.StatusOK, props)
}

func (s *Server) handleOptimizeServerLag(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	props, err := engine.OptimizeLowLagProperties(inst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, props)
}

// --- Modrinth & Addons ---

func (s *Server) handleModrinthSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	mcVer := q.Get("mcVersion")
	loader := q.Get("loader")
	if strings.EqualFold(q.Get("engine"), string(models.EngineNeoForge)) || strings.EqualFold(loader, "neoforge") {
		mcVer = addons.EffectiveMCVersion(models.EngineNeoForge, mcVer)
	}
	res, err := addons.SearchModrinth(r.Context(), q.Get("query"), q.Get("projectType"), loader, mcVer, limit)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleModrinthVersions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	loaders := addons.ExpandLoaderFilter(q.Get("loader"))
	mcVer := q.Get("mcVersion")
	if strings.EqualFold(q.Get("engine"), string(models.EngineNeoForge)) {
		mcVer = addons.EffectiveMCVersion(models.EngineNeoForge, mcVer)
	}
	versions, err := addons.ListProjectVersions(r.Context(), r.PathValue("projectId"), loaders, mcVer)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, versions)
}

func (s *Server) handleInstallModrinth(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var body struct {
		ProjectID string `json:"projectId"`
		VersionID string `json:"versionId"`
		Kind      string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := addons.InstallModrinthProject(r.Context(), inst, body.ProjectID, body.VersionID, body.Kind)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleListAddons(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	list, err := addons.ListInstalledAddons(inst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleUploadAddon(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "dosya secilmedi")
		return
	}
	defer file.Close()
	kind := r.FormValue("kind")
	name, destPaths, effectiveKind, err := addons.InstallUploadedAddonWithPath(inst, header.Filename, file, kind)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var agg models.AddonInstallResult
	agg.Installed = []string{name}
	for _, dp := range destPaths {
		depRes := addons.ResolveUploadedAddonDependencies(r.Context(), inst, dp, effectiveKind)
		for _, d := range depRes.Dependencies {
			agg.Dependencies = append(agg.Dependencies, d)
		}
		for _, md := range depRes.MissingDependencies {
			agg.MissingDependencies = append(agg.MissingDependencies, md)
		}
		for _, wMsg := range depRes.Warnings {
			agg.Warnings = append(agg.Warnings, wMsg)
		}
	}
	writeJSON(w, http.StatusOK, agg)
}

func (s *Server) handleToggleAddon(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	enabled, err := addons.ToggleAddon(inst, body.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": enabled})
}

func (s *Server) handleDeleteAddon(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	relPath := r.URL.Query().Get("path")
	if relPath == "" {
		writeError(w, http.StatusBadRequest, "path parametresi gerekli")
		return
	}
	if err := addons.DeleteAddon(inst, relPath); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// --- Packs & Shaders ---

func (s *Server) handleListPacks(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	packs, err := addons.ListPacks(inst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, packs)
}

func (s *Server) handleUploadPack(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "dosya secilmedi")
		return
	}
	defer file.Close()
	pack, err := addons.SavePackFile(inst, header.Filename, file)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pack)
}

func (s *Server) handleApplyPack(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var body struct {
		Filename string `json:"filename"`
		BaseURL  string `json:"baseUrl"`
		Required bool   `json:"required"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.TrimSpace(body.BaseURL) == "" {
		cfg := s.store.GetConfig()
		body.BaseURL = fmt.Sprintf("http://%s:%d", engine.DetectLocalLANIP(), cfg.APIPort)
	}
	props, err := addons.ApplyResourcePack(inst, body.Filename, body.BaseURL, body.Required)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, props)
}

func (s *Server) handleExportMRPack(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	tmp, err := os.CreateTemp("", "sekailink-mrpack-*.mrpack")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := addons.ExportClientMRPack(inst, tmpPath); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s-client.mrpack\"", inst.ID))
	http.ServeFile(w, r, tmpPath)
}

func (s *Server) handleServePackFile(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rawName, err := url.PathUnescape(r.PathValue("filename"))
	if err != nil {
		rawName = r.PathValue("filename")
	}
	safeName := filepath.Base(rawName)
	full := filepath.Join(inst.Path, "sekailink_packs", safeName)
	if _, err := os.Stat(full); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, full)
}

// --- Worlds & Backups ---

func (s *Server) handleListWorlds(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	list, err := worlds.ListWorlds(inst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleImportWorld(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := r.ParseMultipartForm(512 << 20); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "dunya arsivi secilmedi")
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("", "sekailink-wimport-*.zip")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, file); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmp.Close()
	defer os.Remove(tmpPath)

	worldName := r.FormValue("name")
	if strings.TrimSpace(worldName) == "" {
		worldName = header.Filename
	}
	imported, err := worlds.ImportWorld(inst, tmpPath, worldName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": imported})
}

func (s *Server) handleSwitchWorld(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := worlds.SwitchActiveWorld(inst, body.Name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	inst.ActiveWorld = body.Name
	_ = s.store.SaveInstance(inst)
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleExportWorld(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	worldName := r.PathValue("name")
	tmp, err := os.CreateTemp("", "sekailink-wexport-*.zip")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	if err := worlds.ExportWorldZip(inst, worldName, tmpPath); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.zip\"", filepath.Base(worldName)))
	http.ServeFile(w, r, tmpPath)
}

func (s *Server) handleDeleteWorld(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, err := s.store.GetInstance(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if s.manager.IsRunning(id) {
		writeError(w, http.StatusConflict, "calisan sunucudaki dunya silinemez, once sunucuyu durdurun")
		return
	}
	if err := worlds.DeleteWorld(inst, r.PathValue("name")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleListBackups(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	list, err := worlds.ListBackups(inst)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateBackup(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	var body struct {
		WorldName string `json:"worldName"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	info, err := worlds.CreateBackup(inst, body.WorldName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) handleRestoreBackup(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, err := s.store.GetInstance(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if s.manager.IsRunning(id) {
		writeError(w, http.StatusConflict, "yedekten geri donmeden once sunucuyu durdurun")
		return
	}
	if err := worlds.RestoreBackup(inst, r.PathValue("filename")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"restored": true})
}

func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	inst, err := s.store.GetInstance(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if err := worlds.DeleteBackup(inst, r.PathValue("filename")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

// --- Network & Tunnels ---

func (s *Server) handleNetworkDiagnostics(w http.ResponseWriter, r *http.Request) {
	diag := network.RunDiagnostics(r.Context())
	writeJSON(w, http.StatusOK, diag)
}

func (s *Server) handleUPnPMap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Port     int    `json:"port"`
		Protocol string `json:"protocol"`
		Desc     string `json:"desc"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Protocol == "" {
		body.Protocol = "TCP"
	}
	if body.Desc == "" {
		body.Desc = "MC SekaiLink"
	}
	if err := network.MapPortUPnP(r.Context(), body.Port, strings.ToUpper(body.Protocol), body.Desc); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"mapped": true})
}

func (s *Server) handleUPnPUnmap(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Port     int    `json:"port"`
		Protocol string `json:"protocol"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Protocol == "" {
		body.Protocol = "TCP"
	}
	if err := network.UnmapPortUPnP(r.Context(), body.Port, strings.ToUpper(body.Protocol)); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"unmapped": true})
}

func (s *Server) handlePlayitStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.playit.Status())
}

func (s *Server) handlePlayitInstall(w http.ResponseWriter, r *http.Request) {
	st, err := s.playit.Install(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handlePlayitStart(w http.ResponseWriter, r *http.Request) {
	if err := s.playit.Start(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.playit.Status())
}

func (s *Server) handlePlayitStop(w http.ResponseWriter, r *http.Request) {
	if err := s.playit.Stop(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.playit.Status())
}

func (s *Server) handleE4mcStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.e4mc.Status())
}

func (s *Server) handleE4mcStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		BrokerURL string `json:"brokerUrl"`
		LocalPort int    `json:"localPort"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.LocalPort <= 0 {
		body.LocalPort = 25565
	}
	st, err := s.e4mc.Start(body.BrokerURL, body.LocalPort)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleE4mcStop(w http.ResponseWriter, r *http.Request) {
	_ = s.e4mc.Stop()
	writeJSON(w, http.StatusOK, s.e4mc.Status())
}

func (s *Server) handleBoreStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.bore.Status())
}

func (s *Server) handleBoreStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ServerHost string `json:"serverHost"`
		LocalPort  int    `json:"localPort"`
		Secret     string `json:"secret"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.LocalPort <= 0 {
		body.LocalPort = 25565
	}
	st, err := s.bore.Start(body.ServerHost, body.LocalPort, body.Secret)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleBoreStop(w http.ResponseWriter, r *http.Request) {
	_ = s.bore.Stop()
	writeJSON(w, http.StatusOK, s.bore.Status())
}

func (s *Server) handleP2PStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.p2p.Status())
}

func (s *Server) handleP2PHost(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LocalJavaPort    int  `json:"localJavaPort"`
		LocalBedrockPort int  `json:"localBedrockPort"`
		WantJava         bool `json:"wantJava"`
		WantBedrock      bool `json:"wantBedrock"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if !body.WantJava && !body.WantBedrock {
		body.WantJava = true
	}
	if body.WantJava && body.LocalJavaPort <= 0 {
		body.LocalJavaPort = 25565
	}
	if body.WantBedrock && body.LocalBedrockPort <= 0 {
		body.LocalBedrockPort = 19132
	}
	st, err := s.p2p.StartHost(network.P2PHostOptions{
		LocalJavaPort:    body.LocalJavaPort,
		LocalBedrockPort: body.LocalBedrockPort,
		WantJava:         body.WantJava,
		WantBedrock:      body.WantBedrock,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleP2PJoin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RoomCode             string `json:"roomCode"`
		PeerName             string `json:"peerName"`
		PreferredJavaPort    int    `json:"preferredJavaPort"`
		PreferredBedrockPort int    `json:"preferredBedrockPort"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := network.NormalizeRoomCode(body.RoomCode); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := s.p2p.StartJoin(network.P2PJoinOptions{
		RoomCode:             body.RoomCode,
		PeerName:             body.PeerName,
		PreferredJavaPort:    body.PreferredJavaPort,
		PreferredBedrockPort: body.PreferredBedrockPort,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleP2PStop(w http.ResponseWriter, r *http.Request) {
	_ = s.p2p.Stop()
	writeJSON(w, http.StatusOK, s.p2p.Status())
}

func (s *Server) handlePinggyStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.pinggy.Status())
}

func (s *Server) handlePinggyStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Region    string `json:"region"`
		Token     string `json:"token"`
		LocalPort int    `json:"localPort"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.LocalPort <= 0 {
		body.LocalPort = 25565
	}
	st, err := s.pinggy.Start(body.Region, body.Token, body.LocalPort)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handlePinggyStop(w http.ResponseWriter, r *http.Request) {
	_ = s.pinggy.Stop()
	writeJSON(w, http.StatusOK, s.pinggy.Status())
}

func (s *Server) handleAutoStartTunnel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		LocalJavaPort    int  `json:"localJavaPort"`
		LocalBedrockPort int  `json:"localBedrockPort"`
		WantJava         bool `json:"wantJava"`
		WantBedrock      bool `json:"wantBedrock"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.LocalJavaPort <= 0 {
		body.LocalJavaPort = 25565
	}
	if body.LocalBedrockPort <= 0 {
		body.LocalBedrockPort = 19132
	}
	if !body.WantJava && !body.WantBedrock {
		body.WantJava = true
	}

	_ = s.e4mc.Stop()
	_ = s.pinggy.Stop()
	_ = s.bore.Stop()

	if body.WantJava {
		e4mcSt, eErr := s.e4mc.Start("", body.LocalJavaPort)
		if eErr == nil && e4mcSt.Running {
			writeJSON(w, http.StatusOK, models.AutoTunnelResult{
				Provider:    "e4mc",
				NodeID:      "e4mc (" + e4mcSt.Region + ")",
				LatencyMs:   e4mcSt.LatencyMs,
				JavaAddress: e4mcSt.Domain,
				E4mc:        &e4mcSt,
			})
			return
		}

		var reason string
		if eErr != nil {
			reason = eErr.Error()
		}

		pinggySt, pErr := s.pinggy.Start("eu", "", body.LocalJavaPort)
		if pErr == nil && pinggySt.Running {
			writeJSON(w, http.StatusOK, models.AutoTunnelResult{
				Provider:       "pinggy",
				NodeID:         "pinggy.io (" + pinggySt.Region + ")",
				LatencyMs:      pinggySt.LatencyMs,
				JavaAddress:    pinggySt.Address,
				FallbackReason: reason,
				Pinggy:         &pinggySt,
			})
			return
		}

		boreSt, bErr := s.bore.Start("bore.pub", body.LocalJavaPort, "")
		if bErr == nil && boreSt.Running {
			if pErr != nil {
				reason = fmt.Sprintf("%s; pinggy=%v", reason, pErr)
			}
			writeJSON(w, http.StatusOK, models.AutoTunnelResult{
				Provider:       "bore",
				NodeID:         "bore.pub",
				LatencyMs:      boreSt.LatencyMs,
				JavaAddress:    boreSt.Address,
				FallbackReason: reason,
				Bore:           &boreSt,
			})
			return
		}
		writeError(w, http.StatusBadGateway, fmt.Sprintf("tum tüneller basarisiz: e4mc=(%v) pinggy=(%v) bore=(%v)", eErr, pErr, bErr))
		return
	}

	writeError(w, http.StatusBadGateway, "saf Bedrock (UDP) icin Ag & Tunel sekmesinden Direkt P2P Odasi acin veya playit.gg baslatin")
}

// --- WebSocket ---

func (s *Server) handleWSConsole(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := s.store.GetInstance(id); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ch, history, unsub := s.manager.Subscribe(id)
	defer unsub()

	_ = conn.WriteJSON(models.WSMessage{Type: "history", Data: history})

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var incoming struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			}
			if err := conn.ReadJSON(&incoming); err != nil {
				return
			}
			if incoming.Type == "command" && strings.TrimSpace(incoming.Command) != "" {
				_ = s.manager.SendCommand(id, incoming.Command)
			}
		}
	}()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case msg := <-ch:
			if err := conn.WriteJSON(msg); err != nil {
				return
			}
		case <-ticker.C:
			if m, err := s.manager.GetMetrics(id); err == nil {
				if err := conn.WriteJSON(models.WSMessage{Type: "metrics", Data: m}); err != nil {
					return
				}
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
