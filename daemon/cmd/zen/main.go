package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/doctor"
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/selfupdate"
	"github.com/daoleno/zen/daemon/server"
	"github.com/daoleno/zen/daemon/setup"
	"github.com/daoleno/zen/daemon/stats"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"golang.org/x/term"
)

type daemonConfig struct {
	addr     string
	stateDir string
	lan      bool
}

type pairConfig struct {
	endpoint string
	stateDir string
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
		if errors.Is(err, doctor.ErrNotReady) {
			os.Exit(1)
		}
		if isSetupUserError(err) {
			os.Exit(1)
		}
		log.Fatalf("%v", err)
	}
}

func run(args []string, stderr io.Writer) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "serve":
			return runDaemon(args[1:], stderr)
		case "pair":
			return runPairCommand(args[1:], stderr)
		case "doctor":
			return runDoctorCommand(args[1:], stderr)
		case "setup":
			return runSetupCommand(args[1:], stderr)
		case "update":
			return runUpdateCommand(args[1:], stderr)
		case "agent":
			return runAgentCommand(args[1:], stderr)
		case "brain":
			return runBrainCommand(args[1:], stderr)
		}
	}
	return runDaemon(args, stderr)
}

func isSetupUserError(err error) bool {
	return errors.Is(err, setup.ErrBlocked) ||
		errors.Is(err, setup.ErrNoExecutor) ||
		errors.Is(err, setup.ErrConsentRequired) ||
		errors.Is(err, setup.ErrInvalidArgs) ||
		errors.Is(err, setup.ErrIncomplete)
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
	startUpdateNotice(stderr, stateDir)
	for _, name := range []string{"tasks.json", "runs.json", "meta.json"} {
		_ = os.Remove(filepath.Join(stateDir, name))
	}

	w := watcher.New(500 * time.Millisecond)
	w.SetActivityProbe(classifier.DefaultActivityProbe())
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
		stateDir:   authManager.StorageDir(),
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
		serverErr <- srv.RunWithReady(ctx, cfg.addr, func() {
			printStartupInfo(stderr, cfg.addr, cfg.stateDir, detectPrivateNetworkAddresses())
		})
	}()

	select {
	case err := <-controlErr:
		return fmt.Errorf("control server error: %w", err)
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
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

func runUpdateCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	checkOnly := false
	fs.BoolVar(&checkOnly, "check", false, "check for an update without installing it")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen update [--check]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	client, err := selfupdate.NewClient()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	candidate, err := client.Latest(ctx, Version)
	if err != nil {
		return fmt.Errorf("check for update: %w", err)
	}
	if candidate == nil {
		fmt.Fprintf(os.Stdout, "Zen %s is up to date.\n", Version)
		return nil
	}
	if checkOnly {
		fmt.Fprintf(os.Stdout, "Zen %s is available; run: zen update\n", candidate.Version)
		return nil
	}
	binary, err := client.DownloadBinary(ctx, *candidate)
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate zen executable: %w", err)
	}
	if err := selfupdate.ReplaceExecutable(executable, binary); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Updated Zen %s → %s. Stop and start zen to use it.\n", Version, candidate.Version)
	return nil
}

func startUpdateNotice(stderr io.Writer, stateDir string) {
	file, ok := stderr.(*os.File)
	if !ok || !startupNoticeAllowed(term.IsTerminal(int(file.Fd())), os.Getenv("TERM"), os.Getenv("CI"), false) {
		return
	}
	cachePath := filepath.Join(stateDir, "update-check.json")
	go func() {
		now := time.Now()
		if cache, fresh := selfupdate.ReadCache(cachePath, now); fresh {
			if line := selfupdate.NoticeLine(Version, cache.LatestVersion); line != "" {
				fmt.Fprintln(stderr, line)
			}
			return
		}
		client, err := selfupdate.NewClient()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		candidate, err := client.Latest(ctx, Version)
		if err != nil {
			return
		}
		latest := ""
		if candidate != nil {
			latest = candidate.Version
		}
		if selfupdate.WriteCache(cachePath, selfupdate.Cache{CheckedAt: now, LatestVersion: latest}) != nil {
			return
		}
		if line := selfupdate.NoticeLine(Version, latest); line != "" {
			fmt.Fprintln(stderr, line)
		}
	}()
}

func startupNoticeAllowed(interactive bool, termValue, ciValue string, jsonContext bool) bool {
	return interactive && !jsonContext && !strings.EqualFold(strings.TrimSpace(termValue), "dumb") && strings.TrimSpace(ciValue) == ""
}

func runAgentCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printAgentUsage(stderr)
		return flag.ErrHelp
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
	case "status":
		return runAgentStatus(args[1:], stderr)
	case "progress":
		return runAgentProgress(args[1:], stderr)
	case "close", "kill":
		return runAgentClose(args[1:], stderr)
	default:
		return fmt.Errorf("unknown agent command: %s", args[0])
	}
}

func runBrainCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printBrainUsage(stderr)
		return flag.ErrHelp
	}
	switch args[0] {
	case "workspace":
		return runBrainWorkspace(args[1:], stderr)
	case "context":
		return runBrainContext(args[1:], stderr)
	case "playbooks":
		return runBrainPlaybooks(args[1:], stderr)
	case "gc":
		return runBrainGC(args[1:], stderr)
	case "executors":
		return runBrainExecutors(args[1:], stderr)
	case "use":
		return runBrainUse(args[1:], stderr)
	default:
		return fmt.Errorf("unknown brain command: %s", args[0])
	}
}

func isHelpArg(value string) bool {
	return value == "-h" || value == "--help" || value == "help"
}

func printAgentUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: zen agent <list|spawn|send|capture|status|progress|close|kill> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  list       List visible agent sessions")
	fmt.Fprintln(w, "  spawn      Create a visible delegated agent session")
	fmt.Fprintln(w, "  send       Send text to an agent session")
	fmt.Fprintln(w, "  capture    Capture an agent session transcript")
	fmt.Fprintln(w, "  status     Print compact status for one agent session")
	fmt.Fprintln(w, "  progress   Report lifecycle progress for the current or selected agent")
	fmt.Fprintln(w, "  close      Close an agent session")
	fmt.Fprintln(w, "  kill       Alias for close")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  zen agent list --json")
	fmt.Fprintln(w, "  zen agent spawn -name \"Review docs\" -executor codex -cwd /repo -prompt \"Inspect docs\"")
	fmt.Fprintln(w, "  zen agent capture -id brain-agent-review-docs:@1 --json")
	fmt.Fprintln(w, "  zen agent status -id brain-agent-review-docs:@1 --json")
	fmt.Fprintln(w, "  zen agent progress --status running --phase working --attention none --summary \"Reading files\" --task-class lasting_design --event-kind invariant --lease 300")
	fmt.Fprintln(w, "  zen agent send -id brain-agent-review-docs:@1 -text \"continue\" --submit=true")
	fmt.Fprintln(w, "  zen agent close -id brain-agent-review-docs:@1 --force")
}

func printBrainUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: zen brain <workspace|context|playbooks|gc|executors|use> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  workspace  Print the Brain workspace path")
	fmt.Fprintln(w, "  context    Print structured Brain context")
	fmt.Fprintln(w, "  playbooks  Print the Brain playbook catalog")
	fmt.Fprintln(w, "  gc         Backfill Brain workspace files and print housekeeping status")
	fmt.Fprintln(w, "  executors  List configured Brain host executors")
	fmt.Fprintln(w, "  use        Switch the Brain host executor")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  zen brain workspace --json")
	fmt.Fprintln(w, "  zen brain context --json")
	fmt.Fprintln(w, "  zen brain playbooks --json")
	fmt.Fprintln(w, "  zen brain gc --json")
	fmt.Fprintln(w, "  zen brain executors --json")
	fmt.Fprintln(w, "  zen brain use codex")
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
	fs.StringVar(&req.Profile, "profile", "implementation", "agent lifecycle profile: quick, research, implementation, or long_running")
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
	req.AgentID = currentAgentID()
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func currentAgentID() string {
	if agentID := strings.TrimSpace(os.Getenv("ZEN_AGENT_ID")); agentID != "" {
		return agentID
	}
	pane := strings.TrimSpace(os.Getenv("TMUX_PANE"))
	if pane == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", pane, "#{session_name}:#{window_id}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
	fs.BoolVar(&req.Force, "force", false, "force send to a non-delegated external session")
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

func runAgentStatus(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen agent status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "agent_status"}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&req.AgentID, "id", "", "agent session id")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen agent status -id main:@42 [flags]")
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

func runAgentProgress(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen agent progress", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{
		stateDir: strings.TrimSpace(os.Getenv("ZEN_STATE_DIR")),
		json:     true,
	}
	req := control.Request{
		Type:    "agent_progress",
		AgentID: strings.TrimSpace(os.Getenv("ZEN_AGENT_ID")),
	}
	fs.StringVar(&cfg.stateDir, "state-dir", cfg.stateDir, "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&req.AgentID, "id", req.AgentID, "agent session id; defaults to ZEN_AGENT_ID")
	fs.StringVar(&req.Status, "status", "", "progress status: running, done, failed, or blocked")
	fs.StringVar(&req.Phase, "phase", "", "progress phase: starting, reading, planning, working, verifying, or reporting")
	fs.StringVar(&req.Attention, "attention", "", "attention state: none, done, blocked, failed, user_input, or stale")
	fs.StringVar(&req.Summary, "summary", "", "short progress summary")
	fs.StringVar(&req.TaskClass, "task-class", "", "semantic task class: exploration, mechanical_change, or lasting_design")
	fs.StringVar(&req.EventKind, "event-kind", "", "semantic progress event: progress, invariant, artifact, risk, needs_judgment, verification, or done")
	fs.StringVar(&req.DetailsJSON, "details-json", "", "optional JSON details for the semantic event")
	fs.IntVar(&req.LeaseSeconds, "lease", 0, "seconds until the next expected progress update")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen agent progress --status running --phase working --attention none --summary 'Reading files' --lease 300 [flags]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(req.AgentID) == "" {
		return fmt.Errorf("agent id is required; pass -id or set ZEN_AGENT_ID")
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runAgentClose(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen agent close", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "agent_close"}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&req.AgentID, "id", "", "agent session id")
	fs.BoolVar(&req.Force, "force", false, "force close even if a delegated agent is still running")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen agent close -id main:@42 [flags]")
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

func runBrainContext(args []string, stderr io.Writer) error {
	cfg, err := parseCLIConfig("zen brain context", args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: "brain_context"})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainPlaybooks(args []string, stderr io.Writer) error {
	cfg, err := parseCLIConfig("zen brain playbooks", args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: "brain_playbooks"})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainGC(args []string, stderr io.Writer) error {
	cfg, err := parseCLIConfig("zen brain gc", args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: "brain_gc"})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainExecutors(args []string, stderr io.Writer) error {
	cfg, err := parseCLIConfig("zen brain executors", args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: "brain_executors"})
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
		fmt.Fprintln(stderr, "Usage: zen brain use <executor> [flags]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: zen brain use <executor> [flags]")
	}
	resp, err := callControl(cfg, control.Request{
		Type:       "brain_set_executor",
		ExecutorID: fs.Arg(0),
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
	if len(resp.Executors) > 0 {
		for _, executor := range resp.Executors {
			marker := "  "
			if executor.Host || (resp.Executor != nil && resp.Executor.ID == executor.ID) {
				marker = "H "
			}
			if executor.Delegated || (resp.DelegatedExecutor != nil && resp.DelegatedExecutor.ID == executor.ID) {
				if strings.TrimSpace(marker) == "H" {
					marker = "HD"
				} else {
					marker = "D "
				}
			}
			fmt.Fprintf(w, "%s%s\t%s\t%s\t%s\n", marker, executor.ID, executor.Provider, executor.Runtime, executor.Command)
		}
		return nil
	}
	if resp.Executor != nil {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", resp.Executor.ID, resp.Executor.Provider, resp.Executor.Runtime, resp.Executor.Command)
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
	authManager, err := auth.NewManager(cfg.stateDir)
	if err != nil {
		return fmt.Errorf("initialize auth manager: %w", err)
	}
	pairing, err := authManager.IssuePairingToken(auth.DefaultPairingTTL)
	if err != nil {
		return fmt.Errorf("issue pairing token: %w", err)
	}
	offers, err := buildConnectionOffers(cfg.endpoint, authManager, pairing)
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
	fs.BoolVar(&cfg.lan, "lan", false, "listen on all IPv4 interfaces for trusted private-network access")
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen [flags]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Subcommands:")
		fmt.Fprintln(stderr, "  serve      Start the daemon")
		fmt.Fprintln(stderr, "  pair       Generate a fresh pairing link")
		fmt.Fprintln(stderr, "  doctor     Diagnose machine readiness for Zen")
		fmt.Fprintln(stderr, "  setup      Guided first-run setup (uses doctor)")
		fmt.Fprintln(stderr, "  update     Verify and install the latest Zen release")
		fmt.Fprintln(stderr, "  agent      List, spawn, inspect, message, progress, and close agent sessions")
		fmt.Fprintln(stderr, "  brain      Inspect Brain workspace and host executor configuration")
	}

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	addrExplicit := false
	fs.Visit(func(value *flag.Flag) {
		if value.Name == "addr" {
			addrExplicit = true
		}
	})
	if cfg.lan && addrExplicit {
		return cfg, fmt.Errorf("--lan and -addr cannot be used together")
	}
	if cfg.lan {
		cfg.addr = "0.0.0.0:9876"
	}
	if fs.NArg() > 0 {
		return cfg, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return cfg, nil
}

func parsePairConfig(args []string, stderr io.Writer) (pairConfig, error) {
	fs := flag.NewFlagSet("zen pair", flag.ContinueOnError)
	fs.SetOutput(stderr)

	cfg := pairConfig{}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen pair [flags] <endpoint>")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() != 1 {
		return cfg, fmt.Errorf("pair requires exactly one endpoint")
	}
	cfg.endpoint = fs.Arg(0)
	return cfg, nil
}
