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

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/server"
	"github.com/daoleno/zen/daemon/watcher"
)

func main() {
	addr := flag.String("addr", ":9876", "listen address")
	secretHex := flag.String("secret", "", "hex-encoded pairing secret (generated if empty)")
	flag.Parse()

	// Load or generate secret.
	var secret *auth.Secret
	var err error
	if *secretHex != "" {
		secret, err = auth.LoadSecret(*secretHex)
		if err != nil {
			log.Fatalf("invalid secret: %v", err)
		}
	} else {
		secret, err = auth.GenerateSecret()
		if err != nil {
			log.Fatalf("generating secret: %v", err)
		}
	}

	fmt.Fprintln(os.Stderr, "╔══════════════════════════════════════╗")
	fmt.Fprintln(os.Stderr, "║         zen-daemon v0.1.0            ║")
	fmt.Fprintln(os.Stderr, "╠══════════════════════════════════════╣")
	fmt.Fprintf(os.Stderr,  "║  Listening on %-22s ║\n", *addr)
	fmt.Fprintf(os.Stderr,  "║  Pairing code: %-21s ║\n", secret.PairingCode())
	fmt.Fprintln(os.Stderr, "╠══════════════════════════════════════╣")
	fmt.Fprintf(os.Stderr,  "║  Secret: %-27s ║\n", secret.Hex()[:16]+"...")
	fmt.Fprintln(os.Stderr, "╚══════════════════════════════════════╝")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintln(os.Stderr, "\nShutting down...")
		cancel()
	}()

	w := watcher.New(500 * time.Millisecond)
	go w.Run(ctx)

	pusher := push.New()
	srv := server.New(secret, w, pusher)
	if err := srv.Run(ctx, *addr); err != nil && err.Error() != "http: Server closed" {
		log.Fatalf("server error: %v", err)
	}
}
