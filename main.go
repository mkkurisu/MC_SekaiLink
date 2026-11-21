package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/MC_SekaiLink/mc-sekailink/internal/models"
	"github.com/MC_SekaiLink/mc-sekailink/web"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

func main() {
	for _, arg := range os.Args[1:] {
		if arg == "--headless" {
			runHeadless()
			return
		}
	}

	_ = os.Setenv("SEKAILINK_MODE", "desktop")
	store, mgr, playit, apiSrv, err := createRuntimeServices("")
	if err != nil {
		log.Fatalf("MC SekaiLink init failed: %v", err)
	}

	app := NewApp(store, mgr, playit, apiSrv)

	err = wails.Run(&options.App{
		Title:     "MC SekaiLink",
		Width:     1320,
		Height:    840,
		MinWidth:  960,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Assets: web.SubFS(),
		},
		BackgroundColour: &options.RGBA{R: 7, G: 11, B: 20, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			Theme:                windows.Dark,
		},
		Linux: &linux.Options{
			WindowIsTranslucent: false,
			ProgramName:         "MC SekaiLink",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

func runHeadless() {
	fs := flag.NewFlagSet("headless", flag.ExitOnError)
	_ = fs.Bool("headless", true, "Run without desktop UI")
	portFlag := fs.Int("port", 0, "HTTP/WebSocket API port")
	hostFlag := fs.String("host", "", "Bind address")
	dataDirFlag := fs.String("data-dir", "", "Writable data directory")

	filtered := make([]string, 0, len(os.Args)-1)
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "-psn_") {
			filtered = append(filtered, a)
		}
	}
	_ = fs.Parse(filtered)

	store, mgr, playit, apiSrv, err := createRuntimeServices(*dataDirFlag)
	if err != nil {
		log.Fatalf("init failed: %v", err)
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
	if err := apiSrv.StartListening(); err != nil {
		log.Fatalf("listen failed: %v", err)
	}
	cfg := store.GetConfig()
	fmt.Printf("MC SekaiLink Headless running on http://%s:%d\n", cfg.BindAddress, cfg.APIPort)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh

	mgr.StopAll()
	_ = playit.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = apiSrv.Shutdown(ctx)
}
