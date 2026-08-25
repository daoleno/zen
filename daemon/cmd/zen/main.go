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
	"sync"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/agentproc"
	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/doctor"
	"github.com/daoleno/zen/daemon/link"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/push"
	"github.com/daoleno/zen/daemon/selfupdate"
	"github.com/daoleno/zen/daemon/server"
	"github.com/daoleno/zen/daemon/setup"
	"github.com/daoleno/zen/daemon/stats"
	telegramchannel "github.com/daoleno/zen/daemon/telegram"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
	"golang.org/x/term"
)

type daemonConfig struct {
	addr           string
	stateDir       string
	linkConfigPath string
	lan            bool
}

type pairConfig struct {
	endpoint       string
	stateDir       string
	linkConfigPath string
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
		case "calendar":
			return runCalendarCommand(args[1:], stderr)
		case "telegram":
			return runTelegramCommand(args[1:], stderr)
		case "codex-gateway":
			return runCodexGatewayCommand(args[1:], stderr)
		case "devices":
			return runDevicesCommand(args[1:], stderr)
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

// daemonTmuxRuntime returns the user-visible tmux server selected at daemon
// startup and the daemon-namespaced scratch directory used to contain plain
// tmux commands launched inside provider panes. An inherited TMUX value binds
// Zen to that exact caller server; an empty value selects the Unix user's
// ordinary default server. The durable daemon identity namespaces only the
// provider scratch, never a private host server.
func daemonTmuxRuntime(home, daemonID, tmuxEnv string) (socketPath, scratchDir string, err error) {
	daemonID = strings.TrimSpace(daemonID)
	if daemonID == "" {
		return "", "", fmt.Errorf("empty daemon identity")
	}
	if len(daemonID) > 24 {
		daemonID = daemonID[:24]
	}
	return tmuxSocketFromEnvironment(tmuxEnv),
		filepath.Join(home, ".zen", "run", "tmux-scratch", daemonID), nil
}

func runDaemon(args []string, stderr io.Writer) error {
	cfg, err := parseDaemonConfig(args, stderr)
	if err != nil {
		return err
	}
	lifecycleLock, authManager, err := acquireDaemonAuthOwner(cfg.stateDir)
	if err != nil {
		return err
	}
	defer lifecycleLock.Close()

	ctx, stopSignals := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer stopSignals()

	stateDir := authManager.StorageDir()
	startUpdateNotice(stderr, stateDir)
	for _, name := range []string{"tasks.json", "runs.json", "meta.json"} {
		_ = os.Remove(filepath.Join(stateDir, name))
	}

	w := watcher.New(500 * time.Millisecond)
	w.ConfigureDelegatedResources(authManager.DaemonID())
	// Bind every Zen-owned Brain and delegated Session to the server visible to
	// the daemon's caller. When launched inside tmux this is the exact inherited
	// server socket; otherwise empty socket semantics select the user's ordinary
	// default server. Provider-internal plain tmux remains contained by the
	// daemon-namespaced scratch directory and the launch-shell TMUX scrub.
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home for daemon tmux runtime: %w", err)
	}
	tmuxSocketPath, tmuxScratchDir, err := daemonTmuxRuntime(home, authManager.DaemonID(), os.Getenv("TMUX"))
	if err != nil {
		return fmt.Errorf("daemon tmux runtime: %w", err)
	}
	if err := os.MkdirAll(tmuxScratchDir, 0o700); err != nil {
		return fmt.Errorf("create daemon tmux scratch directory: %w", err)
	}
	w.SetTmuxServer(tmuxSocketPath, tmuxScratchDir)
	w.SetActivityProbe(classifier.DefaultActivityProbe())
	w.SetProviderActivityProbe(newWorkProviderActivityProbe())
	sc := stats.NewCollector()

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
	worktreeRoot, err := work.DefaultWorktreeRoot()
	if err != nil {
		return fmt.Errorf("resolve worktree root: %w", err)
	}
	if err := work.EnsureDir(worktreeRoot); err != nil {
		return fmt.Errorf("initialize worktree root: %w", err)
	}

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
	w.SetTurnLedger(brainService)
	go brainService.RunLifecycleScheduler(ctx)
	calendarRoot, err := calendar.DefaultRoot()
	if err != nil {
		return fmt.Errorf("resolve calendar root: %w", err)
	}
	calendarStore, err := calendar.NewStore(calendarRoot)
	if err != nil {
		return fmt.Errorf("initialize calendar store: %w", err)
	}
	controlPath, err := control.DefaultSocketPath(authManager.StorageDir())
	if err != nil {
		return fmt.Errorf("resolve control socket path: %w", err)
	}
	controlHandler := &controlApp{
		auth:          authManager,
		watcher:       w,
		execs:         execs,
		brainStore:    brainStore,
		brainService:  brainService,
		calendarStore: calendarStore,
		stateDir:      authManager.StorageDir(),
	}

	profilesPath, err := work.DefaultModelProfilesPath()
	if err != nil {
		return fmt.Errorf("resolve model profiles path: %w", err)
	}
	routesPath, err := work.DefaultRouteBindingsPath()
	if err != nil {
		return fmt.Errorf("resolve route bindings path: %w", err)
	}
	listenerPath, err := work.DefaultRouteListenerPath()
	if err != nil {
		return fmt.Errorf("resolve route listener path: %w", err)
	}
	discoveryPath, err := work.DefaultProviderDiscoveryPath()
	if err != nil {
		return fmt.Errorf("resolve provider discovery path: %w", err)
	}
	credentialsPath, err := work.DefaultProviderCredentialsPath()
	if err != nil {
		return fmt.Errorf("resolve provider credentials path: %w", err)
	}
	credentialStore, err := modelprofiles.NewFileCredentialStore(credentialsPath)
	if err != nil {
		return fmt.Errorf("initialize provider credentials: %w", err)
	}
	profileOwner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath:  profilesPath,
		RoutesPath:    routesPath,
		ListenerPath:  listenerPath,
		DiscoveryPath: discoveryPath,
		Credentials:   credentialStore,
		// Per-session Codex app-server control sockets (live native
		// thread/settings/update for managed Codex sessions).
		CodexControlDir: filepath.Join(authManager.StorageDir(), "codex-ctl"),
		// Machine-level Codex gateway + config takeover: a stable loopback
		// endpoint baked into the CLI's native config; Provider switching
		// retargets the gateway without touching running Codex processes.
		GatewayAddr:     modelprofiles.DefaultGatewayListenAddr,
		GatewayStateDir: filepath.Join(authManager.StorageDir(), "codex-gateway"),
		CodexConfigPath: modelprofiles.DefaultCodexConfigPath(),
	})
	if err != nil {
		return fmt.Errorf("start model profiles owner: %w", err)
	}
	for _, n := range profileOwner.RestoreContractNotices() {
		log.Printf("route %s (session %s) restored with stale contract: %s; the restored binding remains authoritative until an explicit model switch", n.RouteID, n.SessionID, n.Reason)
	}
	defer func() { _ = profileOwner.Close() }()
	if takeover := profileOwner.Takeover(); takeover != nil {
		// New managed Codex launches use the canonical machine-level gateway
		// route while takeover is enabled (the native config projection is the
		// routing truth; Session ownership stays a terminal-control concern).
		profileOwner.SetGatewayBypass(func() bool {
			state, stateErr := takeover.LoadState()
			return stateErr == nil && state.Enabled
		})
	}
	controlHandler.profiles = profileOwner
	brainService.SetSessionRouteLifecycle(profileOwner)

	pusher := push.New()
	launcher := work.NewLauncher(work.TmuxRunner{
		Watcher:          w,
		Env:              progressEnvForStateDir(authManager.StorageDir()),
		Profiles:         profileOwner,
		InputReadyBudget: work.DefaultScheduledInputReadyBudget,
	}, execs)
	srv := server.New(authManager, w, pusher, sc, workStore, execs, brainService)
	telegramManager, err := telegramchannel.NewManager(authManager.StorageDir(), brainService)
	if err != nil {
		return fmt.Errorf("initialize Telegram connection: %w", err)
	}
	srv.SetTelegram(telegramManager)
	controlHandler.telegram = telegramManager
	srv.SetModelProfiles(profileOwner)
	controlHandler.threadRuntimeSet = func(sessionID string, choice modelprofiles.ThreadRuntimeChoice) (modelprofiles.WireSessionSnapshot, modelprofiles.PersistResult, error) {
		snapshot, persist, err := srv.SetThreadRuntime(sessionID, choice)
		return snapshot, persist, err
	}
	calendarScheduler := calendar.NewScheduler(calendarStore, &calendar.WorkRunner{Store: workStore, Launcher: launcher, Watcher: w, Brain: brainService})
	controlHandler.calendarScheduler = calendarScheduler
	srv.SetCalendar(calendarStore, calendarScheduler)
	controlServer := &control.Server{
		Path:    controlPath,
		Handler: controlHandler,
	}
	runtimeOwners := []runtimeOwner{
		{
			name: "Telegram connection",
			run:  telegramManager.Run,
		},
		{
			name: "watcher",
			run:  w.Run,
		},
		{
			name: "stats collector",
			run: func(ctx context.Context) error {
				sc.Start(ctx)
				return nil
			},
		},
		{
			name: "calendar scheduler",
			run: func(ctx context.Context) error {
				calendarScheduler.Run(ctx)
				return nil
			},
		},
		{
			name: "control server",
			run:  controlServer.Run,
		},
	}

	linkConfig, linkConfigPath, linkEnabled, err := loadOptionalLinkConfig(
		authManager.StorageDir(),
		cfg.linkConfigPath,
	)
	if err != nil {
		return err
	}
	if linkEnabled {
		linkConfig.StateObserver = func(state link.ConnectorState) {
			switch {
			case state.Phase == "connected":
				log.Printf(
					"Zen Link connected via %s (registration RTT %s)",
					state.Relay,
					state.MeasuredRTT.Round(time.Millisecond),
				)
			case state.Phase == "offline" && state.LastError != "":
				log.Printf(
					"Zen Link offline: %s; check relay reachability, connector credentials, and route ownership",
					state.LastError,
				)
			}
		}
		transportIdentity, identityErr := link.LoadOrCreateTransportIdentity(
			authManager.StorageDir(),
			link.RelayDomains(linkConfig),
		)
		if identityErr != nil {
			return fmt.Errorf(
				"initialize Zen Link transport identity: %w",
				identityErr,
			)
		}
		connector, connectorErr := link.NewConnector(
			linkConfig,
			authManager,
			transportIdentity,
			srv.Handler(),
		)
		if connectorErr != nil {
			return fmt.Errorf("initialize Zen Link connector: %w", connectorErr)
		}
		runtimeOwners = append(runtimeOwners, runtimeOwner{
			name: "Zen Link connector",
			run:  connector.Run,
		})
		fmt.Fprintf(
			stderr,
			"Zen Link configured from %s; connecting outbound.\n",
			linkConfigPath,
		)
	}

	runtimeOwners = append(runtimeOwners,
		runtimeOwner{
			name: "HTTP server",
			run: func(ctx context.Context) error {
				return srv.RunWithReady(ctx, cfg.addr, func() {
					if linkEnabled {
						printLinkStartupInfo(stderr, cfg.addr, cfg.stateDir)
						return
					}
					printStartupInfo(
						stderr,
						cfg.addr,
						stateDir,
						detectPrivateNetworkAddresses(),
					)
				})
			},
		},
	)
	return runJoinedRuntime(ctx, runtimeOwners)
}

func acquireDaemonAuthOwner(
	stateDir string,
) (*control.LifecycleLock, *auth.Manager, error) {
	resolvedStateDir, err := auth.ResolveStorageDir(stateDir)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve auth storage directory: %w", err)
	}
	lifecycleLock, acquired, err := control.TryAcquireLifecycleLock(
		resolvedStateDir,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("acquire daemon lifecycle owner: %w", err)
	}
	if !acquired {
		return nil, nil, errors.New(
			"another Zen daemon owns this state directory",
		)
	}
	authManager, err := auth.NewManager(resolvedStateDir)
	if err != nil {
		_ = lifecycleLock.Close()
		return nil, nil, fmt.Errorf("initialize auth manager: %w", err)
	}
	return lifecycleLock, authManager, nil
}

type runtimeOwner struct {
	name string
	run  func(context.Context) error
}

type runtimeResult struct {
	name string
	err  error
}

func runJoinedRuntime(parent context.Context, owners []runtimeOwner) error {
	if len(owners) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	results := make(chan runtimeResult, len(owners))
	var running sync.WaitGroup
	for _, owner := range owners {
		owner := owner
		running.Add(1)
		go func() {
			defer running.Done()
			results <- runtimeResult{name: owner.name, err: owner.run(ctx)}
		}()
	}

	first := <-results
	cancel()
	running.Wait()
	close(results)

	all := make([]runtimeResult, 0, len(owners))
	all = append(all, first)
	for result := range results {
		all = append(all, result)
	}
	for _, result := range all {
		if result.err == nil ||
			errors.Is(result.err, context.Canceled) ||
			errors.Is(result.err, http.ErrServerClosed) {
			continue
		}
		return fmt.Errorf("%s error: %w", result.name, result.err)
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
	case "__supervise":
		return runAgentSupervisor(args[1:], stderr)
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

func runAgentSupervisor(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen agent __supervise", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var resourceID string
	var leaseDir string
	var memoryHigh string
	var memoryMax string
	var tasksMax int
	var poolGuard bool
	fs.StringVar(&resourceID, "resource-id", "", "owned delegated resource id")
	fs.StringVar(&leaseDir, "lease-dir", "", "durable resource lease directory")
	fs.StringVar(&memoryHigh, "memory-high", "", "shared soft memory threshold")
	fs.StringVar(&memoryMax, "memory-max", "", "shared hard memory threshold")
	fs.IntVar(&tasksMax, "tasks-max", 0, "maximum owned process count")
	fs.BoolVar(&poolGuard, "pool-guard", false, "elect portable shared-pool memory guard")
	if err := fs.Parse(args); err != nil {
		return err
	}
	command := fs.Args()
	if strings.TrimSpace(resourceID) == "" {
		return fmt.Errorf("resource id is required")
	}
	if strings.TrimSpace(leaseDir) == "" {
		return fmt.Errorf("lease directory is required")
	}
	if len(command) == 0 {
		return fmt.Errorf("supervised command is required after --")
	}
	if os.Getenv("ZEN_AGENT_DELEGATED") != "1" || strings.TrimSpace(os.Getenv("ZEN_AGENT_RESOURCE_UNIT")) != strings.TrimSpace(resourceID) {
		return fmt.Errorf("delegated resource environment does not match resource id")
	}
	return agentproc.RunSupervisor(agentproc.SupervisorConfig{
		ResourceID: resourceID,
		LeaseDir:   leaseDir,
		MemoryHigh: memoryHigh,
		MemoryMax:  memoryMax,
		TasksMax:   tasksMax,
		PoolGuard:  poolGuard,
		Command:    command,
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	})
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
	case "work":
		return runBrainWork(args[1:], stderr)
	case "executors":
		return runBrainExecutors(args[1:], stderr)
	case "use":
		return runBrainUse(args[1:], stderr)
	case "set-delegated":
		return runBrainSetDelegated(args[1:], stderr)
	default:
		return fmt.Errorf("unknown brain command: %s", args[0])
	}
}

func runCalendarCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprintln(stderr, "Usage: zen calendar <list|get|create|update|cancel|run> [flags]")
		return flag.ErrHelp
	}
	switch args[0] {
	case "list":
		return runCalendarSimple("calendar_list", args[1:], stderr)
	case "get":
		return runCalendarID("calendar_get", args[1:], stderr, false)
	case "cancel":
		return runCalendarID("calendar_cancel", args[1:], stderr, true)
	case "run":
		return runCalendarID("calendar_run", args[1:], stderr, false)
	case "create":
		return runCalendarWrite(false, args[1:], stderr)
	case "update":
		return runCalendarWrite(true, args[1:], stderr)
	default:
		return fmt.Errorf("unknown calendar command: %s", args[0])
	}
}

func runCalendarSimple(requestType string, args []string, stderr io.Writer) error {
	cfg, err := parseCLIConfig("zen calendar list", args, stderr)
	if err != nil {
		return err
	}
	resp, err := callControl(cfg, control.Request{Type: requestType})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runCalendarID(requestType string, args []string, stderr io.Writer, withRevision bool) error {
	fs := flag.NewFlagSet("zen calendar", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: requestType}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&req.ID, "id", "", "calendar item id")
	if withRevision {
		fs.Int64Var(&req.Revision, "revision", 0, "expected revision (0 disables conflict check)")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(req.ID) == "" {
		return fmt.Errorf("calendar item -id is required")
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runCalendarWrite(update bool, args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen calendar create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	var itemJSON, title, kind, date, clock, endDate, endClock, timezone, occurrence, endOccurrence, recurrence, notes, instruction, cwd, sourceThread string
	var revision int64
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&itemJSON, "item-json", "", "complete calendar item JSON (required for update)")
	fs.StringVar(&title, "title", "", "title")
	fs.StringVar(&kind, "kind", "", "event, reminder, deadline, or scheduled_action (requires -source-thread)")
	fs.StringVar(&date, "date", "", "local date in YYYY-MM-DD")
	fs.StringVar(&clock, "time", "", "local wall time in HH:MM")
	fs.StringVar(&endDate, "end-date", "", "event end local date in YYYY-MM-DD")
	fs.StringVar(&endClock, "end-time", "", "event end local wall time in HH:MM")
	fs.StringVar(&timezone, "timezone", "", "IANA timezone, for example Asia/Shanghai")
	fs.StringVar(&occurrence, "occurrence", "", "first or second when the local time occurs twice at DST fall-back")
	fs.StringVar(&endOccurrence, "end-occurrence", "", "first or second when the event end occurs twice at DST fall-back")
	fs.StringVar(&recurrence, "recurrence", "none", "none, daily, weekly, or weekdays")
	fs.StringVar(&notes, "notes", "", "notes")
	fs.StringVar(&instruction, "instruction", "", "scheduled action instruction")
	fs.StringVar(&cwd, "cwd", "", "scheduled action working directory")
	fs.StringVar(&sourceThread, "source-thread", "", "required for scheduled_action: source_thread_id for canonical Calendar result projection")
	fs.Int64Var(&revision, "revision", 0, "expected revision")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	var item calendar.Item
	if strings.TrimSpace(itemJSON) != "" {
		if err := json.Unmarshal([]byte(itemJSON), &item); err != nil {
			return fmt.Errorf("decode item-json: %w", err)
		}
	} else {
		if update {
			return fmt.Errorf("calendar update requires -item-json from calendar get and -revision")
		}
		at, err := calendar.ResolveLocalDateTime(date, clock, timezone, occurrence)
		if err != nil {
			return fmt.Errorf("resolve calendar time: %w", err)
		}
		item = calendar.Item{Title: title, Kind: calendar.Kind(kind), Timezone: timezone, Recurrence: calendar.Recurrence(recurrence), Notes: notes, ActionInstruction: instruction, ActionCwd: cwd, SourceThreadID: sourceThread}
		switch item.Kind {
		case calendar.KindEvent:
			item.StartAt = &at
			end, err := calendar.ResolveLocalDateTime(endDate, endClock, timezone, endOccurrence)
			if err != nil {
				return fmt.Errorf("resolve event end: %w", err)
			}
			item.EndAt = &end
		case calendar.KindReminder:
			item.NotifyAt = &at
		default:
			item.DueAt = &at
		}
	}
	if item.Kind == calendar.KindScheduledAction && strings.TrimSpace(item.SourceThreadID) == "" {
		return fmt.Errorf("scheduled_action requires -source-thread (source_thread_id)")
	}
	typeName := "calendar_create"
	if update {
		typeName = "calendar_update"
	}
	resp, err := callControl(cfg, control.Request{Type: typeName, CalendarItem: &item, Revision: revision})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
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
	fmt.Fprintln(w, "Usage: zen brain <workspace|context|playbooks|gc|work|executors|use|set-delegated> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  workspace      Print the Brain workspace path")
	fmt.Fprintln(w, "  context        Print structured Brain context")
	fmt.Fprintln(w, "  playbooks      Print the Brain playbook catalog")
	fmt.Fprintln(w, "  gc             Reconcile product-owned Brain workspace blocks while preserving user content")
	fmt.Fprintln(w, "  work           List, create, update, or append an event to durable Active work")
	fmt.Fprintln(w, "  executors      List configured Brain host and delegated executors")
	fmt.Fprintln(w, "  use            Switch the Brain host executor")
	fmt.Fprintln(w, "  set-delegated  Switch the live Delegated Executor (no restart)")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  zen brain workspace --json")
	fmt.Fprintln(w, "  zen brain context --json")
	fmt.Fprintln(w, "  zen brain playbooks --json")
	fmt.Fprintln(w, "  zen brain gc --json")
	fmt.Fprintln(w, "  zen brain work list --json")
	fmt.Fprintln(w, "  zen brain executors --json")
	fmt.Fprintln(w, "  zen brain use codex")
	fmt.Fprintln(w, "  zen brain set-delegated grok")
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

// runCodexGatewayCommand manages the machine-level Codex gateway takeover:
// status | enable | disable | restore-backup.
func runCodexGatewayCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: zen codex-gateway <status|enable|disable|restore-backup> [flags]")
		return errors.New("codex-gateway subcommand is required")
	}
	cfg, err := parseCLIConfig("zen codex-gateway "+args[0], args[1:], stderr)
	if err != nil {
		return err
	}
	reqType := ""
	switch args[0] {
	case "status":
		reqType = "codex_gateway_status"
	case "enable":
		reqType = "codex_gateway_enable"
	case "disable":
		reqType = "codex_gateway_disable"
	case "restore-backup", "restore":
		reqType = "codex_gateway_restore_backup"
	default:
		fmt.Fprintln(stderr, "Usage: zen codex-gateway <status|enable|disable|restore-backup> [flags]")
		return fmt.Errorf("unknown codex-gateway subcommand %q", args[0])
	}
	resp, err := callControl(cfg, control.Request{Type: reqType})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runAgentSpawn(args []string, stderr io.Writer) error {
	cfg, req, err := parseAgentSpawnArgs(args, stderr)
	if err != nil {
		return err
	}
	req.AgentID = currentAgentID()
	socketPath, err := control.DefaultSocketPath(cfg.stateDir)
	if err != nil {
		return err
	}
	resp, err := control.CallWithTimeout(socketPath, req, agentSpawnControlTimeout)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

// agentSpawnControlTimeout only contains the server's existing bounded
// startup-readiness and provider-admission work. It is a transport deadline,
// not another lifecycle timer or a license to retry non-replayable input.
const agentSpawnControlTimeout = 2 * time.Minute

// parseAgentSpawnArgs binds zen agent spawn flags. -profile is the Work
// lifecycle profile; -model-profile is the optional Model Profile override
// (omit to resolve the selected executor default server-side).
func parseAgentSpawnArgs(args []string, stderr io.Writer) (cliConfig, control.Request, error) {
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
	fs.StringVar(&req.ProfileID, "model-profile", "", "optional Model Profile id override; omit to use the selected executor default")
	fs.StringVar(&req.WorkID, "work", "", "existing Brain Work id to own this delegated Session")
	var completionPolicy string
	var doneCriteriaRef string
	var contextRef string
	fs.StringVar(&completionPolicy, "completion", string(brain.CompletionBounded), "Work completion policy: bounded or until_done")
	fs.StringVar(&doneCriteriaRef, "done-criteria", "", "required done-criteria reference for until_done Work")
	fs.StringVar(&contextRef, "context", "", "optional worklog/context reference")
	fs.BoolVar(&req.Hidden, "hidden", false, "create a hidden session")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen agent spawn -name Franklin -executor codex -cwd /repo -prompt-file task.md [flags]")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return cliConfig{}, control.Request{}, err
	}
	if fs.NArg() > 0 {
		return cliConfig{}, control.Request{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	req.BrainWork = &brain.Work{
		CompletionPolicy: brain.CompletionPolicy(strings.TrimSpace(completionPolicy)),
		DoneCriteriaRef:  strings.TrimSpace(doneCriteriaRef),
		ContextRef:       strings.TrimSpace(contextRef),
	}
	return cfg, req, nil
}

// tmuxSocketFromEnvironment returns the server socket encoded in a TMUX
// environment value. Empty means ordinary default-server semantics.
func tmuxSocketFromEnvironment(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	// TMUX is socket,pid,index. Parse from the right because an explicit -S
	// socket path may itself contain commas; the inherited server binding must
	// remain byte-for-byte exact.
	lastComma := strings.LastIndexByte(value, ',')
	if lastComma < 0 {
		return value
	}
	previousComma := strings.LastIndexByte(value[:lastComma], ',')
	if previousComma < 0 {
		return value
	}
	return value[:previousComma]
}

// tmuxClientSocket returns the exact tmux server of the caller's own pane, or
// "" when the caller is outside tmux and ordinary default-server semantics
// apply. Pane identity hints and daemon startup share the same parser.
func tmuxClientSocket() string {
	return tmuxSocketFromEnvironment(os.Getenv("TMUX"))
}

func currentAgentID() string {
	if agentID := strings.TrimSpace(os.Getenv("ZEN_AGENT_ID")); agentID != "" {
		return agentID
	}
	pane := strings.TrimSpace(os.Getenv("TMUX_PANE"))
	if pane == "" {
		return ""
	}
	args := []string{"display-message", "-p", "-t", pane, "#{session_name}:#{window_id}"}
	if socket := tmuxClientSocket(); socket != "" {
		args = append([]string{"-S", socket}, args...)
	}
	out, err := exec.Command("tmux", args...).Output()
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
	fs.StringVar(&req.WorkID, "work-id", "", "delivered Work id authorizing a review follow-up")
	fs.StringVar(&req.EventID, "event-id", "", "canonical delivered Event identity")
	fs.StringVar(&req.HandlingID, "handling-id", "", "exact Host review handling identity")
	fs.StringVar(&req.ProviderTurnID, "provider-turn-id", "", "exact Host provider Turn identity")
	fs.Int64Var(&req.Revision, "revision", 0, "frozen Work revision from the delivered review")
	fs.StringVar(&req.TurnID, "turn-id", "", "caller-supplied random delegated turn identity for review-authorized reuse")
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
	fs.StringVar(&req.TurnID, "turn-id", "", "exact delegated prompt turn identity")
	fs.StringVar(&req.Status, "status", "", "progress status: running, done, failed, or blocked")
	fs.StringVar(&req.Phase, "phase", "", "progress phase: starting, reading, planning, working, verifying, or reporting")
	fs.StringVar(&req.Attention, "attention", "", "attention state: none, done, blocked, failed, user_input, or stale")
	fs.StringVar(&req.Summary, "summary", "", "short progress summary")
	fs.StringVar(&req.TaskClass, "task-class", "", "semantic task class: exploration, mechanical_change, or lasting_design")
	fs.StringVar(&req.EventKind, "event-kind", "", "semantic progress event: progress, invariant, artifact, risk, needs_judgment, verification, or done")
	fs.StringVar(&req.DetailsJSON, "details-json", "", "optional JSON details for the semantic event")
	fs.IntVar(&req.LeaseSeconds, "lease", 0, "seconds until the next expected progress update")
	fs.StringVar(&req.ProgressEventID, "progress-event-id", "", "logical progress submission id, minted once per submission and reused on retry; generated per call when empty")
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

func runBrainWork(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprintln(stderr, "Usage: zen brain work <list|get|create|update|close|event|resolve> [flags]")
		return flag.ErrHelp
	}
	switch args[0] {
	case "list", "get":
		return runBrainWorkList(args[1:], stderr)
	case "create":
		return runBrainWorkCreate(args[1:], stderr)
	case "update":
		return runBrainWorkUpdate(args[1:], stderr)
	case "close":
		return runBrainWorkClose(args[1:], stderr)
	case "event":
		return runBrainWorkEvent(args[1:], stderr)
	case "event-resolve":
		return runBrainWorkEventResolve(args[1:], stderr)
	case "resolve":
		return runBrainWorkResolve(args[1:], stderr)
	default:
		return fmt.Errorf("unknown brain work command: %s", args[0])
	}
}

func runBrainWorkClose(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen brain work close", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	var workID string
	var revision int64
	var status string
	var actor string
	var reason string
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&workID, "id", "", "Work id")
	fs.Int64Var(&revision, "revision", 0, "exact current Work revision")
	fs.StringVar(&status, "status", "", "terminal status: done or cancelled")
	fs.StringVar(&actor, "actor", "", "explicit actor recording the terminal decision")
	fs.StringVar(&reason, "reason", "", "audited reason for terminalizing the Work")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(workID) == "" || revision <= 0 || strings.TrimSpace(actor) == "" || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("Work id, positive revision, actor, and reason are required")
	}
	status = strings.TrimSpace(status)
	if status != string(brain.WorkDone) && status != string(brain.WorkCancelled) {
		return fmt.Errorf("status must be done or cancelled")
	}
	resp, err := callControl(cfg, control.Request{
		Type: "brain_work_close", WorkID: workID, Revision: revision,
		Status: status, Actor: actor, Reason: reason,
	})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainWorkResolve(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen brain work resolve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	request := brain.WorkReviewDispositionRequest{}
	var disposition string
	var wakeKind string
	var wakeRef string
	var nextAttemptAt string
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&request.WorkID, "work-id", "", "delivered Work id")
	fs.StringVar(&request.HandlingID, "handling-id", "", "exact random Host handling identity")
	fs.StringVar(&request.ProviderTurnID, "provider-turn-id", "", "exact admitted provider Turn identity")
	fs.Uint64Var(&request.ExpectedWorkRevision, "revision", 0, "expected Work revision from the delivered input")
	fs.StringVar(&disposition, "disposition", "", "continue, wait, complete, or cancel")
	fs.StringVar(&request.NextSessionID, "next-attempt-session-id", "", "prepared next Attempt Session id for continue")
	fs.StringVar(&request.NextTurnToken, "next-attempt-turn-token", "", "exact next Attempt turn token")
	fs.StringVar(&wakeKind, "wake-kind", "", "session_terminal, calendar_result, user_input, or due_retry for wait")
	fs.StringVar(&wakeRef, "wake-ref", "", "exact producer reference for wait")
	fs.StringVar(&nextAttemptAt, "next-attempt-at", "", "RFC3339 due time for a due_retry wait")
	fs.StringVar(&request.NextAction, "next-action", "", "descriptive next action")
	fs.StringVar(&request.Summary, "summary", "", "audited disposition summary")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	request.Disposition = brain.WorkDisposition(strings.TrimSpace(disposition))
	if strings.TrimSpace(wakeKind) != "" || strings.TrimSpace(wakeRef) != "" || strings.TrimSpace(nextAttemptAt) != "" {
		request.Wake = &brain.WorkWake{Kind: brain.WorkWakeKind(strings.TrimSpace(wakeKind)), Ref: strings.TrimSpace(wakeRef)}
		if strings.TrimSpace(nextAttemptAt) != "" {
			parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(nextAttemptAt))
			if err != nil {
				return fmt.Errorf("invalid next-attempt-at: %w", err)
			}
			request.Wake.NextAttemptAt = &parsed
		}
	}
	resp, err := callControl(cfg, control.Request{Type: "brain_work_resolve", BrainWorkDisposition: &request})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainWorkList(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen brain work list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	var workID string
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&workID, "id", "", "optional Work id (includes its event history)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	resp, err := callControl(cfg, control.Request{Type: "brain_work_list", WorkID: workID})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainWorkCreate(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen brain work create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	item := brain.Work{Status: brain.WorkOpen, CompletionPolicy: brain.CompletionBounded}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&item.ID, "id", "", "optional stable Work id")
	fs.StringVar(&item.Title, "title", "", "short Active work title")
	fs.StringVar(&item.Objective, "objective", "", "durable requested outcome")
	fs.Var(workStatusFlag{value: &item.Status}, "status", "Work status")
	fs.Var(completionPolicyFlag{value: &item.CompletionPolicy}, "completion", "bounded or until_done")
	fs.StringVar(&item.DoneCriteriaRef, "done-criteria", "", "done-criteria reference required for until_done")
	fs.StringVar(&item.NextAction, "next-action", "", "next useful action")
	fs.StringVar(&item.WaitFor, "wait-for", "", "current wait condition")
	fs.StringVar(&item.ContextRef, "context", "", "worklog/context reference")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	resp, err := callControl(cfg, control.Request{Type: "brain_work_create", BrainWork: &item})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainWorkUpdate(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen brain work update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	item := brain.Work{}
	var workID string
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&workID, "id", "", "Work id")
	fs.StringVar(&item.Title, "title", "", "short Active work title")
	fs.StringVar(&item.Objective, "objective", "", "durable requested outcome")
	fs.Var(workStatusFlag{value: &item.Status}, "status", "Work status")
	fs.StringVar(&item.AttemptSessionID, "attempt-session-id", "", "active Attempt Session id")
	fs.Var(completionPolicyFlag{value: &item.CompletionPolicy}, "completion", "bounded or until_done")
	fs.StringVar(&item.DoneCriteriaRef, "done-criteria", "", "done-criteria reference")
	fs.StringVar(&item.NextAction, "next-action", "", "next useful action")
	fs.StringVar(&item.WaitFor, "wait-for", "", "current wait condition")
	fs.StringVar(&item.ContextRef, "context", "", "worklog/context reference")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if strings.TrimSpace(workID) == "" {
		return fmt.Errorf("Work id is required")
	}
	fields := []string{}
	fieldNames := map[string]string{
		"title": "title", "objective": "objective", "status": "status", "attempt-session-id": "attempt_session_id",
		"completion": "completion_policy", "done-criteria": "done_criteria_ref",
		"next-action": "next_action", "wait-for": "wait_for", "context": "context_ref",
	}
	fs.Visit(func(value *flag.Flag) {
		if field := fieldNames[value.Name]; field != "" {
			fields = append(fields, field)
		}
	})
	resp, err := callControl(cfg, control.Request{
		Type: "brain_work_update", WorkID: workID, BrainWork: &item, WorkFields: fields,
	})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func runBrainWorkEvent(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen brain work event", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	event := brain.WorkEvent{}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&event.ID, "event-id", "", "optional event id")
	fs.StringVar(&event.WorkID, "id", "", "Work id")
	fs.StringVar(&event.Kind, "kind", "", "event kind")
	fs.StringVar(&event.DedupeKey, "dedupe", "", "stable dedupe key")
	fs.StringVar(&event.PayloadRef, "payload", "", "optional payload/evidence reference")
	fs.BoolVar(&event.Actionable, "actionable", false, "allow this event to wake Brain once")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	resp, err := callControl(cfg, control.Request{Type: "brain_work_event", BrainWorkEvent: &event})
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

type workStatusFlag struct {
	value *brain.WorkStatus
}

func (f workStatusFlag) String() string {
	if f.value == nil {
		return ""
	}
	return string(*f.value)
}

func (f workStatusFlag) Set(value string) error {
	*f.value = brain.WorkStatus(strings.TrimSpace(value))
	return nil
}

type completionPolicyFlag struct {
	value *brain.CompletionPolicy
}

func (f completionPolicyFlag) String() string {
	if f.value == nil {
		return ""
	}
	return string(*f.value)
}

func (f completionPolicyFlag) Set(value string) error {
	*f.value = brain.CompletionPolicy(strings.TrimSpace(value))
	return nil
}

// runBrainWorkEventResolve closes held delivery claims explicitly and
// actor-recorded (C.2.6): mark_delivered, discard, or user-authorized replay.
func runBrainWorkEventResolve(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen brain work event-resolve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	var workID, action, actor, reason string
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&workID, "id", "", "held review Work id")
	fs.StringVar(&action, "action", "", "resolution: mark_delivered, discard, or replay")
	fs.StringVar(&actor, "actor", "", "resolving actor (user or Brain on explicit user approval)")
	fs.StringVar(&reason, "reason", "", "audited resolution reason")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen brain work event-resolve -id <work_id> -action <mark_delivered|discard|replay> -actor <actor> -reason <reason>")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	resp, err := callControl(cfg, control.Request{
		Type:      "brain_work_event_resolve",
		WorkID:    workID,
		Operation: action,
		Actor:     actor,
		Reason:    reason,
	})
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

func runBrainSetDelegated(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen brain set-delegated", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen brain set-delegated <executor> [flags]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "Switches the live Delegated Executor in the running daemon without restart.")
		fmt.Fprintln(stderr, "Existing agent sessions keep their original executor.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: zen brain set-delegated <executor> [flags]")
	}
	resp, err := callControl(cfg, control.Request{
		Type:       "set_delegated_executor",
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
	if resp.BrainWork != nil {
		fmt.Fprintf(w, "%s\t%s\t%s\n", resp.BrainWork.ID, resp.BrainWork.Status, resp.BrainWork.Title)
		return nil
	}
	if len(resp.BrainWorks) > 0 {
		for _, item := range resp.BrainWorks {
			fmt.Fprintf(w, "%s\t%s\t%s\n", item.ID, item.Status, item.Title)
		}
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
	retry := time.NewTicker(25 * time.Millisecond)
	defer retry.Stop()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ownerResult, err := withAuthRuntimeOwnerWait(
		cfg.stateDir,
		control.Request{Type: "pair"},
		retry.C,
		deadline.C,
		"pairing token outcome is unknown",
		func(manager *auth.Manager) (control.Response, error) {
			return issuePairingControlResponse(manager), nil
		},
	)
	if err != nil {
		return err
	}
	response := ownerResult.Response
	if err := controlResponseError(response); err != nil {
		return err
	}
	if response.Pairing == nil ||
		strings.TrimSpace(response.Pairing.Token) == "" ||
		strings.TrimSpace(response.Pairing.DaemonID) == "" ||
		strings.TrimSpace(response.Pairing.DaemonPublicKey) == "" {
		return errors.New("runtime owner returned incomplete pairing information")
	}
	pairing := auth.PairingToken{
		Value:     response.Pairing.Token,
		ExpiresAt: response.Pairing.ExpiresAt,
	}
	if strings.TrimSpace(cfg.endpoint) != "" {
		offers, offerErr := buildConnectionOffersWithPublicKey(
			cfg.endpoint,
			response.Pairing.DaemonPublicKey,
			pairing,
		)
		if offerErr != nil {
			return fmt.Errorf("build connection info: %w", offerErr)
		}
		printPairCommandInfo(stderr, response.Pairing.DaemonID, offers)
		return nil
	}

	authManager, err := auth.NewManager(cfg.stateDir)
	if err != nil {
		return fmt.Errorf("initialize auth manager: %w", err)
	}
	if authManager.DaemonID() != response.Pairing.DaemonID ||
		authManager.PublicKeyHex() != response.Pairing.DaemonPublicKey {
		return errors.New("runtime owner pairing identity changed")
	}
	linkConfig, linkConfigPath, enabled, err := loadOptionalLinkConfig(
		authManager.StorageDir(),
		cfg.linkConfigPath,
	)
	if err != nil {
		return err
	}
	if !enabled {
		return fmt.Errorf(
			"Zen Link is not configured; create %s or run zen pair <endpoint> for an Advanced/Self-managed connection",
			link.DefaultConfigPath(authManager.StorageDir()),
		)
	}
	identity, err := link.LoadOrCreateTransportIdentity(
		authManager.StorageDir(),
		link.RelayDomains(linkConfig),
	)
	if err != nil {
		return fmt.Errorf("initialize Zen Link transport identity: %w", err)
	}
	pairContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	admissions, err := link.IssueAdmissions(
		pairContext,
		linkConfig,
		authManager,
		identity,
		auth.DefaultPairingTTL,
	)
	if err != nil {
		return fmt.Errorf(
			"request Zen Link pairing admission from %s: %w (make sure zen is running and Link is connected)",
			linkConfigPath,
			err,
		)
	}
	connectLink, payload, err := link.BuildPairingLink(
		authManager,
		identity,
		linkConfig,
		pairing,
		admissions,
	)
	if err != nil {
		return fmt.Errorf("build Zen Link pairing payload: %w", err)
	}
	primaryURL := ""
	for _, candidate := range payload.Candidates {
		if candidate.AdmissionURL != "" {
			primaryURL = candidate.StableURL
			break
		}
	}
	printPairCommandInfo(stderr, response.Pairing.DaemonID, []connectionOffer{{
		Label:       "Zen Link",
		URL:         primaryURL,
		ConnectLink: connectLink,
	}})
	return nil
}

func parseDaemonConfig(args []string, stderr io.Writer) (daemonConfig, error) {
	fs := flag.NewFlagSet("zen", flag.ContinueOnError)
	fs.SetOutput(stderr)

	cfg := daemonConfig{}
	fs.StringVar(&cfg.addr, "addr", "127.0.0.1:9876", "listen address")
	fs.BoolVar(&cfg.lan, "lan", false, "listen on all IPv4 interfaces for trusted private-network access")
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
	fs.StringVar(&cfg.linkConfigPath, "link-config", "", "Zen Link config (default: <state-dir>/link.json when present)")
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
		fmt.Fprintln(stderr, "  devices    List or revoke paired mobile devices")
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
	fs.StringVar(&cfg.linkConfigPath, "link-config", "", "Zen Link config (default: <state-dir>/link.json)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: zen pair [flags] [endpoint]")
		fmt.Fprintln(stderr, "Without endpoint, use configured Zen Link. Explicit endpoint keeps Pairing V1.")
		fmt.Fprintln(stderr, "")
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	if fs.NArg() > 1 {
		return cfg, fmt.Errorf("pair accepts at most one endpoint")
	}
	if fs.NArg() == 1 {
		cfg.endpoint = fs.Arg(0)
	}
	return cfg, nil
}

func loadOptionalLinkConfig(
	stateDir string,
	explicitPath string,
) (link.ConnectorConfig, string, bool, error) {
	path := strings.TrimSpace(explicitPath)
	explicit := path != ""
	if path == "" {
		path = link.DefaultConfigPath(stateDir)
	}
	config, err := link.LoadConnectorConfig(path)
	if err == nil {
		return config, path, true, nil
	}
	if !explicit && errors.Is(err, os.ErrNotExist) {
		return link.ConnectorConfig{}, path, false, nil
	}
	return link.ConnectorConfig{}, path, false, fmt.Errorf(
		"load Zen Link config %s: %w",
		path,
		err,
	)
}

func runDevicesCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprintln(stderr, "Usage: zen devices <list|revoke> [-state-dir DIR] [flags]")
		return flag.ErrHelp
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("zen devices list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		var stateDir string
		var outputJSON bool
		fs.StringVar(&stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
		fs.BoolVar(&outputJSON, "json", false, "print JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
		}
		devices, err := listDevices(stateDir)
		if err != nil {
			return err
		}
		if outputJSON {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"devices": devices})
		}
		for _, device := range devices {
			fmt.Fprintf(
				os.Stdout,
				"%s\t%s\t%s\n",
				device.ID,
				device.Name,
				device.LastSeenAt.Format(time.RFC3339),
			)
		}
		return nil
	case "revoke":
		fs := flag.NewFlagSet("zen devices revoke", flag.ContinueOnError)
		fs.SetOutput(stderr)
		var stateDir string
		var deviceID string
		fs.StringVar(&stateDir, "state-dir", "", "state directory for daemon identity and trusted devices")
		fs.StringVar(&deviceID, "id", "", "paired device id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 || strings.TrimSpace(deviceID) == "" {
			return errors.New("device -id is required")
		}
		result, err := revokeDevice(stateDir, deviceID)
		if err != nil {
			return err
		}
		return writeDeviceRevokeResult(
			os.Stdout,
			strings.TrimSpace(deviceID),
			result,
		)
	default:
		return fmt.Errorf("unknown devices command: %s", args[0])
	}
}

func writeDeviceRevokeResult(
	output io.Writer,
	deviceID string,
	result deviceRevokeResult,
) error {
	switch result.Outcome {
	case control.PersistenceApplied:
		if result.Durable {
			fmt.Fprintf(
				output,
				"Revoked device %s.\n",
				deviceID,
			)
		} else {
			fmt.Fprintf(
				output,
				"Revoked device %s; persistence was applied but directory durability is uncertain.\n",
				deviceID,
			)
		}
	case control.PersistenceVerifiedAbsent:
		if result.EarlierRequestMayHaveArrived {
			if result.Durable {
				fmt.Fprintf(
					output,
					"Device %s is not trusted; an earlier request may have applied the revocation and durable persisted absence was verified.\n",
					deviceID,
				)
			} else {
				fmt.Fprintf(
					output,
					"Device %s is not trusted; an earlier request may have applied the revocation, current absence was verified, and crash durability is uncertain.\n",
					deviceID,
				)
			}
		} else {
			if result.Durable {
				fmt.Fprintf(
					output,
					"Device %s is not trusted; durable persisted absence was verified.\n",
					deviceID,
				)
			} else {
				fmt.Fprintf(
					output,
					"Device %s is not trusted; current absence was verified and crash durability is uncertain.\n",
					deviceID,
				)
			}
		}
	default:
		return errors.New("revocation outcome is unknown")
	}
	return nil
}

const deviceControlRequestTimeout = time.Second

func listDevices(stateDir string) ([]auth.DeviceInfo, error) {
	retry := time.NewTicker(25 * time.Millisecond)
	defer retry.Stop()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	ownerResult, err := withAuthRuntimeOwnerWait(
		stateDir,
		control.Request{Type: "device_list"},
		retry.C,
		deadline.C,
		"device list was not read",
		func(manager *auth.Manager) (control.Response, error) {
			return control.Response{
				OK:      true,
				Devices: manager.ListDevices(),
			}, nil
		},
	)
	if err != nil {
		return nil, err
	}
	response := ownerResult.Response
	if err := controlResponseError(response); err != nil {
		return nil, err
	}
	return response.Devices, nil
}

type deviceRevokeResult struct {
	Outcome                      control.PersistenceOutcome
	Durable                      bool
	EarlierRequestMayHaveArrived bool
}

func revokeDevice(
	stateDir string,
	deviceID string,
) (deviceRevokeResult, error) {
	retry := time.NewTicker(25 * time.Millisecond)
	defer retry.Stop()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	return revokeDeviceWithRuntimeOwnerWait(
		stateDir,
		deviceID,
		retry.C,
		deadline.C,
	)
}

func revokeDeviceWithRuntimeOwnerWait(
	stateDir string,
	deviceID string,
	retry <-chan time.Time,
	deadline <-chan time.Time,
) (deviceRevokeResult, error) {
	normalizedID := strings.TrimSpace(deviceID)
	ownerResult, err := withAuthRuntimeOwnerWait(
		stateDir,
		control.Request{
			Type: "device_revoke",
			ID:   normalizedID,
		},
		retry,
		deadline,
		"revocation outcome is unknown",
		func(manager *auth.Manager) (control.Response, error) {
			return revokeDeviceControlResponse(manager, normalizedID), nil
		},
	)
	if err != nil {
		return deviceRevokeResult{Outcome: control.PersistenceUnknown}, err
	}
	response := ownerResult.Response
	if response.OK {
		if response.PersistenceOutcome != control.PersistenceApplied ||
			response.PersistenceDurable == nil {
			return deviceRevokeResult{
					Outcome: control.PersistenceUnknown,
				}, errors.New(
					"runtime owner returned success without a complete persistence result",
				)
		}
		return deviceRevokeResult{
			Outcome:                      response.PersistenceOutcome,
			Durable:                      *response.PersistenceDurable,
			EarlierRequestMayHaveArrived: ownerResult.EarlierRequestMayHaveArrived,
		}, nil
	}
	if response.Error != nil &&
		response.Error.Code == "device_not_found" {
		if response.PersistenceOutcome !=
			control.PersistenceVerifiedAbsent ||
			response.PersistenceDurable == nil {
			return deviceRevokeResult{
					Outcome: control.PersistenceUnknown,
				}, errors.New(
					"runtime owner returned device_not_found without a complete durability result",
				)
		}
		return deviceRevokeResult{
			Outcome:                      control.PersistenceVerifiedAbsent,
			Durable:                      *response.PersistenceDurable,
			EarlierRequestMayHaveArrived: ownerResult.EarlierRequestMayHaveArrived,
		}, nil
	}
	return deviceRevokeResult{
		Outcome: control.PersistenceUnknown,
	}, controlResponseError(response)
}

type authRuntimeOwnerResult struct {
	Response                     control.Response
	EarlierRequestMayHaveArrived bool
}

func withAuthRuntimeOwnerWait(
	stateDir string,
	request control.Request,
	retry <-chan time.Time,
	deadline <-chan time.Time,
	unknownOutcome string,
	offline func(manager *auth.Manager) (control.Response, error),
) (authRuntimeOwnerResult, error) {
	resolvedStateDir, err := auth.ResolveStorageDir(stateDir)
	if err != nil {
		return authRuntimeOwnerResult{}, err
	}
	socketPath, err := control.DefaultSocketPath(resolvedStateDir)
	if err != nil {
		return authRuntimeOwnerResult{}, err
	}
	var lastCallErr error
	var earlierRequestMayHaveArrived bool
	for {
		callResult, callErr := control.CallWithTimeoutResult(
			socketPath,
			request,
			deviceControlRequestTimeout,
		)
		if callErr == nil {
			return authRuntimeOwnerResult{
				Response:                     callResult.Response,
				EarlierRequestMayHaveArrived: earlierRequestMayHaveArrived,
			}, nil
		}
		earlierRequestMayHaveArrived =
			earlierRequestMayHaveArrived ||
				callResult.RequestMayHaveArrived
		lastCallErr = callErr

		lifecycleLock, acquired, lockErr :=
			control.TryAcquireLifecycleLock(resolvedStateDir)
		if lockErr != nil {
			return authRuntimeOwnerResult{
				EarlierRequestMayHaveArrived: earlierRequestMayHaveArrived,
			}, lockErr
		}
		if acquired {
			defer lifecycleLock.Close()
			socketInfo, statErr := os.Lstat(socketPath)
			if statErr == nil &&
				socketInfo.Mode()&os.ModeSocket == 0 {
				return authRuntimeOwnerResult{
						EarlierRequestMayHaveArrived: earlierRequestMayHaveArrived,
					}, fmt.Errorf(
						"daemon control path is not a socket: %s",
						socketPath,
					)
			}
			if statErr != nil &&
				!errors.Is(statErr, os.ErrNotExist) {
				return authRuntimeOwnerResult{
						EarlierRequestMayHaveArrived: earlierRequestMayHaveArrived,
					}, fmt.Errorf(
						"inspect daemon control socket: %w",
						statErr,
					)
			}
			manager, managerErr := auth.NewManager(resolvedStateDir)
			if managerErr != nil {
				return authRuntimeOwnerResult{
					EarlierRequestMayHaveArrived: earlierRequestMayHaveArrived,
				}, managerErr
			}
			response, offlineErr := offline(manager)
			return authRuntimeOwnerResult{
				Response:                     response,
				EarlierRequestMayHaveArrived: earlierRequestMayHaveArrived,
			}, offlineErr
		}

		select {
		case <-retry:
			continue
		case <-deadline:
			return authRuntimeOwnerResult{
					EarlierRequestMayHaveArrived: earlierRequestMayHaveArrived,
				}, fmt.Errorf(
					"daemon runtime owner is active but its control socket is unavailable; %s: %w",
					unknownOutcome,
					lastCallErr,
				)
		}
	}
}
