package main

import (
	"context"
	"log"

	"github.com/MC_SekaiLink/mc-sekailink/internal/api"
	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/MC_SekaiLink/mc-sekailink/internal/network"
	"github.com/MC_SekaiLink/mc-sekailink/internal/storage"
	"github.com/MC_SekaiLink/mc-sekailink/web"
)

type App struct {
	ctx     context.Context
	store   *storage.Store
	manager *engine.Manager
	playit  *network.PlayitManager
	apiSrv  *api.Server
}

func NewApp(store *storage.Store, manager *engine.Manager, playit *network.PlayitManager, apiSrv *api.Server) *App {
	return &App{
		store:   store,
		manager: manager,
		playit:  playit,
		apiSrv:  apiSrv,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if err := a.apiSrv.StartListening(); err != nil {
		log.Printf("MC SekaiLink API listener error: %v", err)
	}
}

func (a *App) shutdown(_ context.Context) {
	a.manager.StopAll()
	_ = a.playit.Stop()
	_ = a.apiSrv.Shutdown(context.Background())
}

// GetAPIBaseURL returns the live localhost API base URL to the Wails frontend.
func (a *App) GetAPIBaseURL() string {
	if a.apiSrv != nil {
		return a.apiSrv.ActualURL()
	}
	return "http://127.0.0.1:25580"
}

func createRuntimeServices(dataDir string) (*storage.Store, *engine.Manager, *network.PlayitManager, *api.Server, error) {
	network.InitDNSResolver()
	store, err := storage.NewStore(dataDir)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	mgr := engine.NewManager(store)
	playit := network.NewPlayitManager(store.DataDir(), store.BinDir())
	srv := api.NewServer(store, mgr, playit, web.SubFS())
	return store, mgr, playit, srv, nil
}
