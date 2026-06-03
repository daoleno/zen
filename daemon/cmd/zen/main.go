package main

import (
	"context"
	"encoding/json"
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
	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/server"
	"github.com/daoleno/zen/daemon/stats"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type daemonConfig struct {
	addr         string
	advertiseURL string
	stateDir     string
	pairingTTL   time.Duration
}

type cliConfig struct {
	stateDir string
	json     bool
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
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "serve":
			return runDaemon(args[1:], stderr)
		case "pair", "print-link":
			return runPairCommand(args[1:], stderr)
		case "agent":
			return runAgentCommand(args[1:], stderr)
		case "brain":
			return runBrainCommand(args[1:], stderr)
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
	go func() {
		if err := w.Run(ctx); err != nil && ctx.Err() == nil {
			log.Printf("watcher error: %v", err)
			cancel()
		}
	}()

	sc := stats.NewCollector()
	go sc.Start(ctx)

	workRoot, err := work.DefaultRoot()
	if err != nil {
		return fmt.Errorf("resolve work root: %w", err)
	}
	workStore, err := work.NewStore(workRoot)
	if err != nil {
		return fmt.Errorf("initialize work store: %w", err)
	}
	if err := workStore.StartWatcher(); err != nil {
		return fmt.Errorf("start work watcher: %w", err)
	}
	defer workStore.Close()

	executorsPath, err := work.DefaultExecutorsPath()
	if err != nil {
		return fmt.Errorf("resolve executors path: %w", err)
	}
	execs, err := work.LoadExecutors(executorsPath)
	if err != nil {
		return fmt.Errorf("load executors: %w", err)
	}

	brainRoot, err := brain.DefaultRoot()
	if err != nil {
		return fmt.Errorf("resolve brain root: %w", err)
	}
	brainStore, err := brain.NewStore(brainRoot)
	if err != nil {
		return fmt.Errorf("initialize brain store: %w", err)
	}
	brainService := brain.NewService(brainStore, w, execs)
	controlPath, err := control.DefaultSocketPath(authManager.StorageDir())
	if err != nil {
		return fmt.Errorf("resolve control socket path: %w", err)
	}
	controlHandler := &controlApp{
		watcher:    w,
		execs:      execs,
		brainStore: brainStore,
	}

	pusher := push.New()
	launcher := work.NewLauncher(&work.WatcherRegistry{W: w}, work.TmuxRunner{}, execs)
	srv := server.New(authManager, w, pusher, sc, workStore, launcher, execs, brainService)
	controlErr := make(chan error, 1)
	go func() {
		controlServer := &control.Server{
			Path:    controlPath,
			Handler: controlHandler,
		}
		if err := controlServer.Run(ctx); err != nil && ctx.Err() == nil {
			controlErr <- err
			cancel()
		}
	}()
	serverErr := make(chan error, 1)
	go func() {
		serverErr <- srv.Run(ctx, cfg.addr)
	}()

	select {
	case err := <-controlErr:
		return fmt.Errorf("control server error: %w", err)
	case err := <-serverErr:
		if err != nil && err.Error() != "http: Server closed" {
			return fmt.Errorf("server error: %w", err)
		}
	}

	select {
	case err := <-controlErr:
		return fmt.Errorf("control server error: %w", err)
	default:
	}
	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func runAgentCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: zen agent <list|spawn|send|capture> [flags]")
	}
	switch args[0] {
	case "list":
		return runAgentList(args[1:], stderr)
	case "spawn":
		return runAgentSpawn(args[1:], stderr)
	case "send":
		return runAgentSend(args[1:], stderr)
	case "capture":
		return runAgentCapture(args[1:], stderr)
	default:
		return fmt.Errorf("unknown agent command: %s", args[0])
	}
}

func runBrainCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: zen brain <workspace|adapters|use> [flags]")
	}
	switch args[0] {
	case "workspace":
		return runBrainWorkspace(args[1:], stderr)
	case "adapters":
		return runBrainAdapters(args[1:], stderr)
	case "use":
		return runBrainUse(args[1:], stderr)
	default:
		return fmt.Errorf("unknown brain command: %s", args[0])
	}
}

func runAgentList(args []string, stderr io.Writer) error {
	cfg, err := parseCLIConfig("zen agent list", args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: "agent_list"})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runAgentSpawn(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen agent spawn", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "agent_spawn"}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&req.Name, "name", "", "visible agent name")
	fs.StringVar(&req.Executor, "executor", "", "configured executor name")
	fs.StringVar(&req.Command, "command", "", "explicit command override")
	fs.StringVar(&req.Cwd, "cwd", "", "agent working directory")
	fs.StringVar(&req.Prompt, "prompt", "", "initial prompt text")
	fs.StringVar(&req.PromptFile, "prompt-file", "", "file containing the initial prompt")
	fs.BoolVar(&req.Hidden, "hidden", false, "create a hidden session")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen agent spawn -name Franklin -executor codex -cwd /repo -prompt-file task.md [flags]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runAgentSend(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen agent send", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "agent_send", Submit: true}
	stdin := false
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&req.AgentID, "id", "", "agent session id")
	fs.StringVar(&req.Text, "text", "", "text to send")
	fs.BoolVar(&stdin, "stdin", false, "read text from stdin")
	fs.BoolVar(&req.Submit, "submit", true, "submit after sending text")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen agent send -id main:@42 -text 'continue' [flags]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if stdin {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		req.Text = string(raw)
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runAgentCapture(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen agent capture", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "agent_capture"}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&req.AgentID, "id", "", "agent session id")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen agent capture -id main:@42 [flags]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainWorkspace(args []string, stderr io.Writer) error {
	cfg, err := parseCLIConfig("zen brain workspace", args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: "brain_workspace"})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainAdapters(args []string, stderr io.Writer) error {
	cfg, err := parseCLIConfig("zen brain adapters", args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: "brain_adapters"})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainUse(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen brain use", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen brain use <adapter> [flags]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: zen brain use <adapter> [flags]")
	}
	resp, err := callControl(cfg, control.Request{
		Type:      "brain_set_adapter",
		AdapterID: fs.Arg(0),
	})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func parseCLIConfig(name string, args []string, stderr io.Writer) (cliConfig, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: %s [flags]\n", name)
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

func callControl(cfg cliConfig, req control.Request) (control.Response, error) {
	socketPath, err := control.DefaultSocketPath(cfg.stateDir)
	if err != nil {
		return control.Response{}, err
	}
	resp, err := control.Call(socketPath, req)
	if err != nil {
		return control.Response{}, err
	}
	if !resp.OK {
		return resp, nil
	}
	return resp, nil
}

func writeControlResponse(w io.Writer, resp control.Response, asJSON bool) error {
	if asJSON {
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(resp); err != nil {
			return err
		}
		return controlResponseError(resp)
	}
	if !resp.OK {
		if resp.Error != nil {
			fmt.Fprintf(w, "%s: %s\n", resp.Error.Code, resp.Error.Message)
		}
		return controlResponseError(resp)
	}
	if resp.Workspace != "" {
		fmt.Fprintln(w, resp.Workspace)
		return nil
	}
	if resp.Text != "" {
		fmt.Fprint(w, resp.Text)
		return nil
	}
	if resp.Agent != nil {
		fmt.Fprintf(w, "%s\t%s\t%s\n", resp.Agent.ID, resp.Agent.Status, resp.Agent.Name)
		return nil
	}
	if len(resp.Adapters) > 0 {
		for _, adapter := range resp.Adapters {
			marker := " "
			if adapter.Preferred || (resp.Adapter != nil && resp.Adapter.ID == adapter.ID) {
				marker = "*"
			}
			fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\n", marker, adapter.ID, adapter.Provider, adapter.Runtime, adapter.Command)
		}
		return nil
	}
	if resp.Adapter != nil {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", resp.Adapter.ID, resp.Adapter.Provider, resp.Adapter.Runtime, resp.Adapter.Command)
		return nil
	}
	for _, agent := range resp.Agents {
		fmt.Fprintf(w, "%s\t%s\t%s\n", agent.ID, agent.Status, agent.Name)
	}
	return nil
}

func controlResponseError(resp control.Response) error {
	if resp.OK {
		return nil
	}
	if resp.Error != nil {
		return fmt.Errorf("%s: %s", resp.Error.Code, resp.Error.Message)
	}
	return fmt.Errorf("control request failed")
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
	fs := flag.NewFlagSet("zen", flag.ContinueOnError)
	fs.SetOutput(stderr)

	cfg := daemonConfig{}
	fs.StringVar(&cfg.addr, "addr", "127.0.0.1:9876", "listen address")
	fs.StringVar(&cfg.advertiseURL, "advertise-url", "", "public https/wss URL exposed by your tunnel or reverse proxy")
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
	fs.DurationVar(&cfg.pairingTTL, "pairing-ttl", auth.DefaultPairingTTL, "lifetime for the printed one-time pairing token")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen [flags]")
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
	fs := flag.NewFlagSet("zen pair", flag.ContinueOnError)
	fs.SetOutput(stderr)

	cfg := daemonConfig{}
	fs.StringVar(&cfg.advertiseURL, "advertise-url", "", "public https/wss URL exposed by your tunnel or reverse proxy")
	fs.StringVar(&cfg.advertiseURL, "url", "", "alias for -advertise-url")
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
	fs.DurationVar(&cfg.pairingTTL, "pairing-ttl", auth.DefaultPairingTTL, "lifetime for the printed one-time pairing token")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen pair -advertise-url https://your-host/ws [flags]")
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
