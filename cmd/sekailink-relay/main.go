package main

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/MC_SekaiLink/mc-sekailink/internal/network"
)

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func main() {
	nodeID := flag.String("node-id", envOrDefault("SEKAILINK_NODE_ID", "SV-Relay"), "Bridge node identifier shown in MC SekaiLink (e.g. SV-Oracle)")
	listen := flag.String("listen", envOrDefault("SEKAILINK_LISTEN", ":25590"), "TCP control/relay listen address")
	publicHost := flag.String("public-host", envOrDefault("SEKAILINK_PUBLIC_HOST", ""), "Public IP or domain players connect to")
	secret := flag.String("secret", envOrDefault("SEKAILINK_SECRET", ""), "Optional shared secret key for tunnel hosts")
	portMin := flag.Int("port-min", 25600, "Minimum public TCP/UDP port for game traffic (0 = OS ephemeral)")
	portMax := flag.Int("port-max", 25700, "Maximum public TCP/UDP port for game traffic")
	certDir := flag.String("cert-dir", envOrDefault("SEKAILINK_CERT_DIR", ""), "Optional directory to persist auto-generated TLS 1.3 cert & CertPin")
	tlsCert := flag.String("tls-cert", "", "Optional path to custom TLS certificate PEM")
	tlsKey := flag.String("tls-key", "", "Optional path to custom TLS private key PEM")
	noTLS := flag.Bool("no-tls", false, "Disable TLS 1.3 encryption on Host <-> VDS tunnel")
	allowPlain := flag.Bool("allow-plain", false, "Allow unencrypted legacy clients alongside TLS 1.3")
	flag.Parse()

	var tlsCfg *tls.Config
	var certPin string
	if !*noTLS {
		if *tlsCert != "" && *tlsKey != "" {
			cert, err := tls.LoadX509KeyPair(*tlsCert, *tlsKey)
			if err != nil {
				log.Fatalf("failed to load TLS cert/key: %v", err)
			}
			tlsCfg = &tls.Config{
				Certificates: []tls.Certificate{cert},
				MinVersion:   tls.VersionTLS13,
			}
			if len(cert.Certificate) > 0 {
				sum := sha256.Sum256(cert.Certificate[0])
				certPin = strings.ToUpper(hex.EncodeToString(sum[:]))
			}
		} else {
			var err error
			tlsCfg, certPin, err = network.LoadOrCreateRelayTLS(*certDir)
			if err != nil {
				log.Fatalf("failed to initialize TLS 1.3 certificate: %v", err)
			}
		}
	}

	srv := network.NewRelayServerWithOptions(network.RelayServerOptions{
		NodeID:     *nodeID,
		PublicHost: *publicHost,
		Secret:     *secret,
		PortMin:    *portMin,
		PortMax:    *portMax,
		TLSConfig:  tlsCfg,
		AllowPlain: *allowPlain || *noTLS,
	})

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		fmt.Println("\nShutting down MC SekaiLink Relay...")
		_ = srv.Close()
		os.Exit(0)
	}()

	fmt.Printf("===================================================\n")
	fmt.Printf("  MC SekaiLink VDS Bridge (%s)\n", *nodeID)
	fmt.Printf("  Listen      : %s\n", *listen)
	fmt.Printf("  Public Host : %s\n", *publicHost)
	fmt.Printf("  Port Range  : %d-%d (TCP + UDP)\n", *portMin, *portMax)
	fmt.Printf("  TLS 1.3     : %v (allow-plain=%v)\n", tlsCfg != nil, *allowPlain || *noTLS)
	if certPin != "" {
		fmt.Printf("  CertPin     : %s\n", certPin)
	}
	fmt.Printf("===================================================\n")

	if err := srv.ListenAndServe(*listen); err != nil {
		log.Fatalf("relay exited: %v", err)
	}
}
