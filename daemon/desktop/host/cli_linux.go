package host

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/daoleno/zen/daemon/auth"
	"github.com/daoleno/zen/daemon/desktop"
	"github.com/daoleno/zen/daemon/desktop/nativebind"
)

// RunLinuxCLI implements `zen desktop-host` and the historical zen-desktop-host entry.
func RunLinuxCLI(args []string, stderr io.Writer) error {
	if len(args) > 0 {
		switch args[0] {
		case "authorize":
			return runAuthorizeCommand(args[1:], stderr)
		case "revoke":
			return runRevokeCommand(args[1:], stderr)
		}
	}
	fs := flag.NewFlagSet("zen desktop-host", flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "/etc/zen/desktop-host.json", "Root-owned host configuration")
	initConfig := fs.Bool("init-config", false, "Generate an idempotent user-owned host configuration")
	stateDir := fs.String("state-dir", "", "Zen state directory used to read the canonical daemon identity")
	ownerUnit := fs.String("owner-unit", "", "Optional root-enrolled systemd unit that owns the canonical daemon")
	register := fs.String("register", "", "SDDM registration action: start or stop")
	registerCurrent := fs.Bool("register-current", false, "With --install --activate: register and verify the current SDDM X11 display without restart")
	install := fs.Bool("install", false, "Install reviewed same-binary zen as broker/agent")
	rollback := fs.Bool("rollback", false, "Roll back unchanged installed files; broker must be stopped")
	activate := fs.Bool("activate", false, "Enable/start the installed broker; do not restart SDDM or the owner")
	verbose := fs.Bool("verbose", false, "Print verified probe details and rollback instructions; the default success stays brief")
	planOnly := fs.Bool("plan", false, "Print the reviewed install manifest without changing the host")
	status := fs.Bool("status", false, "Print the read-only desktop authorization/inhibitor status")
	statusJSON := fs.Bool("json", false, "With --status: print machine-readable JSON")
	binarySource := fs.String("binary-source", "", "Reviewed desktop-capable zen ELF used for every role")
	brokerSource := fs.String("broker-source", "", "Legacy alias; must match --binary-source / --agent-source")
	agentSource := fs.String("agent-source", "", "Legacy alias; must match --binary-source / --broker-source")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected desktop-host arguments")
	}
	if *registerCurrent && (!*install || !*activate || *rollback || *planOnly || *initConfig || *register != "") {
		return errors.New("--register-current requires --install --activate and cannot be combined with init-config, plan, rollback or register")
	}
	if *statusJSON && !*status {
		return errors.New("--json requires --status")
	}
	if *status {
		if *install || *rollback || *activate || *planOnly || *initConfig || *register != "" || *registerCurrent {
			return errors.New("--status is read-only and cannot be combined with init-config, plan, install, rollback, activate or register")
		}
		return printAuthorizationStatus(stderr, *statusJSON, *stateDir)
	}
	if *initConfig {
		return initLinuxConfig(fs, *configPath, *stateDir, *ownerUnit, stderr)
	}
	if *planOnly {
		if *install || *rollback || *activate || *register != "" {
			fmt.Fprintln(stderr, "zen desktop-host --plan cannot be combined with install, rollback, activate, or register.")
			return errors.New("invalid_desktop_host_plan")
		}
		file, err := os.Open(*configPath)
		if err != nil {
			fmt.Fprintln(stderr, "Zen desktop host operation failed.")
			return err
		}
		config, err := ReadHostConfig(file)
		file.Close()
		if err != nil {
			fmt.Fprintln(stderr, "Zen desktop host operation failed.")
			return err
		}
		plan, err := PrepareLinuxInstall(config)
		if err != nil {
			fmt.Fprintln(stderr, "Zen desktop host operation failed.")
			return err
		}
		PrintInstallPlan(stderr, plan)
		return nil
	}
	var err error
	if *rollback {
		if serviceCommand("is-active", "--quiet", "zen-desktop-host.service") == nil {
			fmt.Fprintln(stderr, "Stop the installed broker before rollback.")
			return errBrokerActive
		}
		err = serviceCommand("disable", "--no-reload", "zen-desktop-host.service")
		if err == nil {
			err = RollbackLinuxInstall()
		}
		if err == nil {
			err = serviceCommand("daemon-reload")
		}
	} else if *register != "" {
		err = RegisterDisplay(*register, os.Getenv("DISPLAY"), os.Getenv("XAUTHORITY"))
	} else {
		var config HostConfig
		if *install {
			var file *os.File
			file, err = os.Open(*configPath)
			if err == nil {
				config, err = ReadHostConfig(file)
				file.Close()
			}
		} else {
			config, err = LoadRootConfig(*configPath)
		}
		if err == nil {
			if *install {
				source, sourceErr := ResolveInstallSource(*binarySource, *brokerSource, *agentSource)
				if sourceErr != nil {
					err = sourceErr
				} else if *registerCurrent {
					return installAndRegisterCurrent(config, source, stderr, *verbose)
				} else {
					err = InstallLinux(config, source)
				}
			}
			if err == nil && *activate {
				// An ownerUnit is optional: with account scope there is no unit to
				// preflight, and the broker admits the owner UID directly.
				if config.OwnerUnit != "" {
					err = serviceCommand("is-active", "--quiet", config.OwnerUnit)
				}
				if err == nil {
					err = serviceCommand("daemon-reload")
				}
				if err == nil {
					err = serviceCommand("enable", "--now", "zen-desktop-host.service")
				}
			} else if !*install && !*activate {
				ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
				defer cancel()
				err = ServeBroker(ctx, config)
			}
		}
	}
	if err != nil {
		if !errors.Is(err, errBrokerActive) {
			fmt.Fprintln(stderr, "Zen desktop host operation failed.")
		}
		return err
	}
	return nil
}

var runDesktopUserSystemctl = func(args ...string) ([]byte, error) {
	return exec.Command("systemctl", append([]string{"--user"}, args...)...).CombinedOutput()
}

// probeCanonicalDaemon detects the already-running daemon that owns this
// canonical state directory. This lets authorize reuse an interactive `zen`
// or `zen-dev` process instead of trying to start a second zen.service against
// the same lock, identity and control socket.
var probeCanonicalDaemon = func(stateDir string) bool {
	socket := filepath.Join(stateDir, "run", "zen.sock")
	conn, err := net.DialTimeout("unix", socket, 500*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func runAuthorizeCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen desktop-host authorize", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "canonical Zen state directory (default: ~/.zen)")
	unit := fs.String("unit", "zen.service", "existing Zen user unit to ensure")
	jsonOut := fs.Bool("json", false, "print machine-readable output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected desktop-host authorize arguments")
	}
	resolvedState, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return fmt.Errorf("locate canonical Zen state: %w", err)
	}
	if _, err := os.Stat(filepath.Join(resolvedState, "identity.json")); err != nil {
		return errors.New("canonical Zen identity is unavailable; start Zen once before authorizing remote desktop")
	}
	manager, err := auth.NewManager(resolvedState)
	if err != nil {
		return fmt.Errorf("read canonical Zen state: %w", err)
	}
	active := false
	canonicalRunning := false
	if _, probeErr := runDesktopUserSystemctl("is-active", "--quiet", *unit); probeErr == nil {
		active = true
	} else {
		canonicalRunning = probeCanonicalDaemon(resolvedState)
		if canonicalRunning {
			// The caller already owns this state directory. Do not install or
			// launch another user unit; authorization will be observed by this
			// daemon through its live control/desktop lifecycle.
		} else if output, startErr := runDesktopUserSystemctl("enable", "--now", *unit); startErr != nil {
			return fmt.Errorf("ensure %s in the user manager: %v: %s; run `zen boot install` once if the unit is not installed", *unit, startErr, strings.TrimSpace(string(output)))
		}
	}
	if !active && !canonicalRunning {
		if _, probeErr := runDesktopUserSystemctl("is-active", "--quiet", *unit); probeErr != nil {
			return fmt.Errorf("%s did not become active; inspect `systemctl --user status %s`", *unit, *unit)
		}
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"ok": true, "unit": *unit, "daemon": map[string]any{"systemd_user_unit": active, "canonical_running": canonicalRunning}, "state_dir": manager.StorageDir(),
			"source": "kde-wayland-portal", "phone_action": "Open Zen on the paired phone, choose Remote Desktop, then Enable remote desktop and confirm.",
		})
	}
	if canonicalRunning {
		fmt.Fprintf(os.Stdout, "Remote desktop host ready via the running canonical Zen daemon (state %s).\n", manager.StorageDir())
	} else {
		fmt.Fprintf(os.Stdout, "Remote desktop host ready via user unit %s (canonical state %s).\n", *unit, manager.StorageDir())
	}
	fmt.Fprintln(os.Stdout, "On the paired phone: open Remote Desktop, choose Enable remote desktop, and confirm the desktop scope.")
	fmt.Fprintln(os.Stdout, "Repeat this command safely; use `zen desktop-host revoke` to clear every desktop scope.")
	return nil
}

func runRevokeCommand(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen desktop-host revoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	stateDir := fs.String("state-dir", "", "canonical Zen state directory (default: ~/.zen)")
	jsonOut := fs.Bool("json", false, "print machine-readable output")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected desktop-host revoke arguments")
	}
	resolvedState, err := auth.ResolveStorageDir(*stateDir)
	if err != nil {
		return fmt.Errorf("locate canonical Zen state: %w", err)
	}
	if _, err := os.Stat(filepath.Join(resolvedState, "identity.json")); err != nil {
		return errors.New("canonical Zen identity is unavailable; start Zen once before revoking remote desktop")
	}
	manager, err := auth.NewManager(resolvedState)
	if err != nil {
		return fmt.Errorf("read canonical Zen state: %w", err)
	}
	count, err := manager.RevokeDesktopScopes()
	if err != nil {
		return fmt.Errorf("revoke desktop scopes: %w", err)
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "revoked": count})
	}
	fmt.Fprintf(os.Stdout, "Remote desktop disabled; cleared desktop scope from %d paired device(s).\n", count)
	fmt.Fprintln(os.Stdout, "The running daemon will release desktop inhibitors on its next authorization reconcile.")
	return nil
}

// printAuthorizationStatus is the read-only operator surface for the scoped
// authorization lifecycle. It acquires nothing and changes no state.
func printAuthorizationStatus(out io.Writer, asJSON bool, stateDir string) error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	report := InspectAuthorizationReport(ctx, stateDir)
	if asJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}
	freshness := "stale"
	if report.StatusFresh {
		freshness = "fresh"
	}
	status := report.Status
	fmt.Fprintln(out, "Zen desktop authorization status (read-only; no inhibitor acquired)")
	fmt.Fprintf(out, "  persisted authorization: %d device(s) with desktop scope\n", report.PersistedDevices)
	if report.PersistedError != "" {
		fmt.Fprintf(out, "  persisted authorization error: %s\n", report.PersistedError)
	}
	fmt.Fprintf(out, "  daemon status record: %s pid=%d reason=%s active=%t\n", freshness, status.PID, status.Reason, status.Active)
	if report.StatusError != "" {
		fmt.Fprintf(out, "  status record error: %s\n", report.StatusError)
	}
	if report.Status.Recovery != "" {
		fmt.Fprintf(out, "  recovery (request only; verify after it returns): %s\n", report.Status.Recovery)
	}
	fmt.Fprintf(out, "  inhibitor: idle=%s sleep=%s lock=%s\n", status.Inhibitors.LogindIdle, status.Inhibitors.LogindSleep, status.Inhibitors.ScreenSaver)
	if status.Session != nil {
		fmt.Fprintf(out, "  session: %s %s uid=%d %s\n", status.Session.Backend, status.Session.Display, status.Session.UID, status.Session.Seat)
	}
	fmt.Fprintf(out, "  live logind inhibitor: idle=%t sleep=%t pid=%d\n", report.LiveLogind.Idle, report.LiveLogind.Sleep, report.LiveLogind.PID)
	if report.LiveError != "" {
		fmt.Fprintf(out, "  live logind error: %s\n", report.LiveError)
	}
	fmt.Fprintf(out, "  status record path: %s\n", report.StatusPath)
	return nil
}

func initLinuxConfig(fs *flag.FlagSet, configuredPath, stateDir, ownerUnit string, stderr io.Writer) error {
	for _, name := range []string{"register", "register-current", "install", "rollback", "activate", "plan", "binary-source", "broker-source", "agent-source"} {
		if flagWasSet(fs, name) {
			return errors.New("--init-config cannot be combined with install, rollback, activate, plan, register, or binary source flags")
		}
	}
	if !nativebind.NativeLinked {
		return errors.New("desktop host setup requires a Linux desktop-capable zen ELF (CGO and -tags zen_desktop)")
	}
	if os.Getuid() == 0 {
		return errors.New("run --init-config as the unprivileged Zen owner, not sudo")
	}
	if strings.TrimSpace(stateDir) == "" {
		var err error
		stateDir, err = auth.DefaultStorageDir()
		if err != nil {
			return fmt.Errorf("locate Zen state directory: %w", err)
		}
	}
	stateDir, err := filepath.Abs(stateDir)
	if err != nil {
		return err
	}
	if info, err := os.Lstat(filepath.Join(stateDir, "identity.json")); err != nil || !info.Mode().IsRegular() {
		return errors.New("existing daemon identity required; use the same --state-dir as the running Zen")
	}
	executable, err := desktop.CurrentExecutable()
	if err != nil {
		return fmt.Errorf("locate current zen executable: %w", err)
	}
	if err := inspectInitHost(); err != nil {
		return err
	}
	manager, err := auth.NewManager(stateDir)
	if err != nil {
		return fmt.Errorf("read canonical daemon identity: %w", err)
	}
	path := configuredPath
	if !flagWasSet(fs, "config") {
		path = filepath.Join(stateDir, "desktop-host.json")
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	config := HostConfig{Version: 1, HostID: manager.DaemonID(), OwnerUID: uint32(os.Getuid()), Seat: "seat0", OwnerUnit: strings.TrimSpace(ownerUnit)}
	created, err := InitializeConfig(path, config)
	if err != nil {
		return err
	}
	if created {
		fmt.Fprintf(stderr, "Zen desktop host config created: %s\n", path)
	} else {
		fmt.Fprintf(stderr, "Zen desktop host config already matches this daemon: %s\n", path)
	}
	fmt.Fprintf(stderr, "  owner account: uid %d, seat0\n", config.OwnerUID)
	fmt.Fprintf(stderr, "  review: %s desktop-host --plan --config %s\n", shellQuote(executable), shellQuote(path))
	fmt.Fprintf(stderr, "  install: sudo %s desktop-host --install --config %s --binary-source %s --activate --register-current\n", shellQuote(executable), shellQuote(path), shellQuote(executable))
	fmt.Fprintln(stderr, "  current-session access does not need this administrator step; boot/greeter access does.")
	fmt.Fprintln(stderr, "  --register-current validates the current desktop session without restarting it or sending input.")
	return nil
}

var inspectInitHost = func() error {
	observation, err := InspectLinux(context.Background())
	if err != nil {
		return fmt.Errorf("desktop setup: active seat0 session unavailable: %w", err)
	}
	switch observation.Session.Backend {
	case "x11":
		if _, _, _, err := sddmConfiguration(); err != nil {
			return fmt.Errorf("desktop setup: SDDM X11 hooks unavailable or unsafe: %w", err)
		}
		return nil
	case "wayland":
		if observation.Class != "user" || observation.Session.UID != uint32(os.Getuid()) {
			return errors.New("desktop setup: run inside the enrolled owner's unlocked Wayland desktop")
		}
		if observation.Session.Surface != Desktop {
			return errors.New("desktop setup: unlock the Wayland desktop before setup")
		}
		if err := waylandHostQualification(observation.Session.UID); err != nil {
			return fmt.Errorf("desktop setup: Wayland desktop qualification failed: %w", err)
		}
		return nil
	default:
		return errors.New("desktop setup: active seat0 session type is unsupported")
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func flagWasSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

func serviceCommand(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/systemctl", args...).Run()
}

// serviceOutput reads a single systemctl property value for verification.
func serviceOutput(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/systemctl", args...).Output()
	return strings.TrimSpace(string(out)), err
}
