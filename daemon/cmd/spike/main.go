package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

func main() {
	fmt.Fprintln(os.Stderr, "zen-spike: tmux session watcher (validation spike)")
	fmt.Fprintln(os.Stderr, "Polling tmux sessions every 500ms. Press Ctrl+C to stop.")
	fmt.Fprintln(os.Stderr, "Events are printed as JSON to stdout.")
	fmt.Fprintln(os.Stderr, "---")

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

	// Print events as JSON.
	go func() {
		enc := json.NewEncoder(os.Stdout)
		for ev := range w.Events() {
			enc.Encode(ev)
		}
	}()

	// Periodically print agent list summary to stderr.
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				agents := w.Agents()
				if len(agents) == 0 {
					fmt.Fprintln(os.Stderr, "[no tmux sessions found]")
					continue
				}
				fmt.Fprintf(os.Stderr, "\n=== %d agents ===\n", len(agents))
				for _, a := range agents {
					fmt.Fprintf(os.Stderr, "  %-20s [%-8s] %s\n", a.ID, a.State, a.Summary)
				}
			}
		}
	}()

	if err := w.Run(ctx); err != nil && err != context.Canceled {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
