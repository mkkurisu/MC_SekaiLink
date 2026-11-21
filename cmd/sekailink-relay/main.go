package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/MC_SekaiLink/mc-sekailink/internal/network"
)

func main() {
	listen := flag.String("listen", ":25590", "TCP control/relay listen address")
	publicHost := flag.String("public-host", "", "Public IP or domain players connect to")
	secret := flag.String("secret", "", "Optional shared secret key for tunnel hosts")
	flag.Parse()

	srv := network.NewRelayServer(*publicHost, *secret)
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		fmt.Println("\nShutting down MC SekaiLink Relay...")
		_ = srv.Close()
		os.Exit(0)
	}()

	fmt.Printf("MC SekaiLink TCP+UDP Relay listening on %s (public-host=%q)\n", *listen, *publicHost)
	if err := srv.ListenAndServe(*listen); err != nil {
		log.Fatalf("relay exited: %v", err)
	}
}
