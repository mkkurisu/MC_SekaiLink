package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/api"
	"github.com/MC_SekaiLink/mc-sekailink/internal/engine"
	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"github.com/MC_SekaiLink/mc-sekailink/internal/network"
	"github.com/MC_SekaiLink/mc-sekailink/internal/storage"
	"github.com/MC_SekaiLink/mc-sekailink/web"
)

func main() {
	portFlag := flag.Int("port", 0, "HTTP/WebSocket API port (default 25580)")
	hostFlag := flag.String("host", "", "Bind host (e.g. 127.0.0.1 or 0.0.0.0)")
	dataDirFlag := flag.String("data-dir", "", "Writable data directory path")
	flag.Parse()

	network.InitTLSRootCAs()
	network.InitDNSResolver()

	store, err := storage.NewStore(*dataDirFlag)
	if err != nil {
		log.Fatalf("store init failed: %v", err)
	}
	if *portFlag > 0 || *hostFlag != "" {
		_, _ = store.UpdateConfig(func(cfg *models.AppConfig) {
			if *portFlag > 0 {
				cfg.APIPort = *portFlag
			}
			if *hostFlag != "" {
				cfg.BindAddress = *hostFlag
				cfg.RemoteEnabled = (*hostFlag == "0.0.0.0")
			}
		})
	}

	mgr := engine.NewManager(store)
	playit := network.NewPlayitManager(store.DataDir(), store.BinDir())
	srv := api.NewServer(store, mgr, playit, web.SubFS())

	if err := srv.StartListening(); err != nil {
		log.Fatalf("listen failed: %v", err)
	}

	cfg := store.GetConfig()
	fmt.Printf("MC SekaiLink Daemon running at http://%s:%d (data: %s)\n", cfg.BindAddress, cfg.APIPort, store.DataDir())

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	fmt.Println("Shutting down MC SekaiLink...")
	mgr.StopAll()
	_ = playit.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}
