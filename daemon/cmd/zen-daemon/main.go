package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/issue"
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/server"
	"github.com/daoleno/zen/daemon/stats"
	"github.com/daoleno/zen/daemon/watcher"
)

type daemonConfig struct {
	addr         string
	advertiseURL string
	stateDir     string
	pairingTTL   time.Duration
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Fatalf("%v", err)
	}
}

func run(args []string, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "pair", "print-link":
			return runPairCommand(args[1:], stderr)
		}
	}
	return runDaemon(args, stderr)
}

func runDaemon(args []string, stderr io.Writer) error {
	cfg, err := parseDaemonConfig(args, stderr)
	if err != nil {
		return err
	}

	authManager, err := auth.NewManager(cfg.stateDir)
	if err != nil {
		return fmt.Errorf("initialize auth manager: %w", err)
	}

	mode := startupModeLocalOnly
	if strings.TrimSpace(cfg.advertiseURL) != "" {
		mode = startupModePairable
	}
	printStartupBanner(stderr, cfg.addr, authManager.DaemonID(), mode)

	if mode == startupModePairable {
		pairing, err := authManager.IssuePairingToken(cfg.pairingTTL)
		if err != nil {
			return fmt.Errorf("issue pairing token: %w", err)
		}
		offers, err := buildConnectionOffers(cfg.advertiseURL, authManager, pairing)
		if err != nil {
			return fmt.Errorf("build connection info: %w", err)
		}
		printPairingInfo(stderr, offers)
	} else {
		printLocalOnlyInfo(stderr, cfg.stateDir)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		fmt.Fprintln(stderr, "\nShutting down...")
		cancel()
	}()

	stateDir := authManager.StorageDir()
	for _, name := range []string{"tasks.json", "runs.json", "meta.json"} {
		_ = os.Remove(filepath.Join(stateDir, name))
	}

	w := watcher.New(500 * time.Millisecond)
	go w.Run(ctx)

	sc := stats.NewCollector()
	go sc.Start(ctx)

	issuesRoot, err := issue.DefaultRoot()
	if err != nil {
		return fmt.Errorf("resolve issues root: %w", err)
	}
	issueStore, err := issue.NewStore(issuesRoot)
	if err != nil {
		return fmt.Errorf("initialize issue store: %w", err)
	}
	if err := issueStore.StartWatcher(); err != nil {
		return fmt.Errorf("start issue watcher: %w", err)
	}
	defer issueStore.Close()

	executorsPath, err := issue.DefaultExecutorsPath()
	if err != nil {
		return fmt.Errorf("resolve executors path: %w", err)
	}
	execs, err := issue.LoadExecutors(executorsPath)
	if err != nil {
		return fmt.Errorf("load executors: %w", err)
	}

	pusher := push.New()
	dispatcher := issue.NewDispatcher(&issue.WatcherRegistry{W: w}, issue.TmuxRunner{}, execs)
	srv := server.New(authManager, w, pusher, sc, issueStore, dispatcher, execs)
	if err := srv.Run(ctx, cfg.addr); err != nil && err.Error() != "http: Server closed" {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func runPairCommand(args []string, stderr io.Writer) error {
	cfg, err := parsePairConfig(args, stderr)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.advertiseURL) == "" {
		return fmt.Errorf("pair requires -advertise-url or -url")
	}

	authManager, err := auth.NewManager(cfg.stateDir)
	if err != nil {
		return fmt.Errorf("initialize auth manager: %w", err)
	}
	pairing, err := authManager.IssuePairingToken(cfg.pairingTTL)
	if err != nil {
		return fmt.Errorf("issue pairing token: %w", err)
	}
	offers, err := buildConnectionOffers(cfg.advertiseURL, authManager, pairing)
	if err != nil {
		return fmt.Errorf("build connection info: %w", err)
	}
	printPairCommandInfo(stderr, authManager.DaemonID(), offers)
	return nil
}

func parseDaemonConfig(args []string, stderr io.Writer) (daemonConfig, error) {
	fs := flag.NewFlagSet("zen-daemon", flag.ContinueOnError)
	fs.SetOutput(stderr)

	cfg := daemonConfig{}
	fs.StringVar(&cfg.addr, "addr", "127.0.0.1:9876", "listen address")
	fs.StringVar(&cfg.advertiseURL, "advertise-url", "", "public https/wss URL exposed by your tunnel or reverse proxy")
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
	fs.DurationVar(&cfg.pairingTTL, "pairing-ttl", auth.DefaultPairingTTL, "lifetime for the printed one-time pairing token")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen-daemon [flags]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Subcommands:")
		fmt.Fprintln(stderr, "  pair       Generate a fresh pairing link without restarting the daemon")
		fmt.Fprintln(stderr, "  print-link Alias for pair")
	}

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return cfg, nil
}

func parsePairConfig(args []string, stderr io.Writer) (daemonConfig, error) {
	fs := flag.NewFlagSet("zen-daemon pair", flag.ContinueOnError)
	fs.SetOutput(stderr)

	cfg := daemonConfig{}
	fs.StringVar(&cfg.advertiseURL, "advertise-url", "", "public https/wss URL exposed by your tunnel or reverse proxy")
	fs.StringVar(&cfg.advertiseURL, "url", "", "alias for -advertise-url")
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
	fs.DurationVar(&cfg.pairingTTL, "pairing-ttl", auth.DefaultPairingTTL, "lifetime for the printed one-time pairing token")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen-daemon pair -advertise-url https://your-host/ws [flags]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return cfg, nil
}
