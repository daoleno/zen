package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/daoleno/zen/daemon/control"
	"golang.org/x/sys/unix"
)

// `zen boot` is the single optional boot-persistence entry for the SAME runtime
// used by `zen` and the DEV runner: it renders a standard systemd user unit for
// the reviewed binary and does not introduce a second identity, state
// directory or supervisor.
//
// Ownership: the state lifecycle lock and the unit's systemd cgroup identify
// the owner. `zen boot` requires a process inside zen.service's cgroup to hold
// the installed state's lifecycle lock and /health to serve that state's
// identity; a manual daemon using another state cannot satisfy that even when
// it runs the same binary. The boot CLI never kills a process it did not
// start.
//
// KillMode=process is required because the daemon reuses the user's ordinary
// tmux server, a shared per-user resource that must survive unit stop and
// restart. Known dependency: the DEV runner's daemon child has no
// parent-death binding yet, so a killed watcher can leave an own daemon
// holding the state; install/uninstall detect and refuse that leftover as an
// own process instead of deleting its configuration. The zen-dev child
// lifetime fix is owned by a separate worker.

const (
	bootManagedMarker  = "# Managed by zen boot install"
	bootServiceName    = "zen.service"
	bootDefaultAddr    = "127.0.0.1:9876"
	bootLANAddr        = "0.0.0.0:9876"
	bootMetadataSuffix = ".meta.json"
	bootMetadataSchema = 1
	// bootLifecycleLockName mirrors control.lifecycleLockName. The lock file is
	// persistent by design; this command only ever looks for an existing holder.
	bootLifecycleLockName = "daemon.lock"
)

var (
	bootEffectiveUID = os.Geteuid
	// The budget covers a first start (and a DEV `go build`) without waiting
	// forever on a unit that systemd already reports as inactive or failed.
	bootVerifyTimeout  = 60 * time.Second
	bootVerifyInterval = 500 * time.Millisecond
	bootHTTPClient     = &http.Client{Timeout: 3 * time.Second}
)

type bootConfig struct {
	Binary   string
	StateDir string
	Addr     string
	WorkDir  string
	PathEnv  string
	Home     string
	LAN      bool
	DryRun   bool
}

type bootRunner interface {
	run(name string, args ...string) ([]byte, error)
}

type execBootRunner struct{}

func (execBootRunner) run(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

func runBootCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printBootUsage(stderr)
		return flag.ErrHelp
	}
	switch args[0] {
	case "install":
		config, err := parseBootConfig("zen boot install", args[1:])
		if err != nil {
			return err
		}
		return bootInstall(config, execBootRunner{}, os.Stdout)
	case "status":
		if len(args[1:]) > 0 {
			if !isHelpArg(args[1]) {
				return fmt.Errorf("zen boot status takes no flags; it reports the installed unit")
			}
			printBootUsage(stderr)
			return flag.ErrHelp
		}
		return bootStatus(execBootRunner{}, os.Stdout)
	case "uninstall":
		if len(args[1:]) > 0 {
			if !isHelpArg(args[1]) {
				return fmt.Errorf("zen boot uninstall takes no flags")
			}
			printBootUsage(stderr)
			return flag.ErrHelp
		}
		return bootUninstall(execBootRunner{}, os.Stdout)
	default:
		return fmt.Errorf("unknown boot command: %s", args[0])
	}
}

func printBootUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: zen boot <install|status|uninstall> [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  install    Render and enable a systemd user unit for this same zen runtime")
	fmt.Fprintln(w, "  status     Report the installed unit, its configuration, owner and /health")
	fmt.Fprintln(w, "  uninstall  Stop, disable and remove only the unit this command installed")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Install flags:")
	fmt.Fprintln(w, "  -binary <path>      zen or zen-dev executable to run (default: this executable)")
	fmt.Fprintln(w, "  -state-dir <path>   daemon state directory (default: ~/.zen)")
	fmt.Fprintln(w, "  -addr <host:port>   daemon listen address (default: 127.0.0.1:9876)")
	fmt.Fprintln(w, "  -work-dir <path>    daemon working directory (default: current directory)")
	fmt.Fprintln(w, "  -path-env <dirs>    executable search PATH stored in the unit (default: current PATH)")
	fmt.Fprintln(w, "  -lan                bind 0.0.0.0:9876 like `zen -lan`")
	fmt.Fprintln(w, "  -dry-run            print the unit without writing or enabling")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "The unit starts the same unprivileged runtime before an interactive")
	fmt.Fprintln(w, "login only when the user manager lingers (loginctl enable-linger).")
	fmt.Fprintln(w, "Remote desktop lock/login still needs the administrator broker install.")
	fmt.Fprintln(w, "`zen boot status` always reads the installed unit configuration.")
}

func parseBootConfig(name string, args []string) (bootConfig, error) {
	var config bootConfig
	addrExplicit := false
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&config.Binary, "binary", "", "zen or zen-dev executable to run")
	flags.StringVar(&config.StateDir, "state-dir", "", "daemon state directory")
	flags.StringVar(&config.Addr, "addr", "", "daemon listen address")
	flags.StringVar(&config.WorkDir, "work-dir", "", "daemon working directory")
	flags.StringVar(&config.PathEnv, "path-env", "", "executable search PATH stored in the unit")
	flags.BoolVar(&config.LAN, "lan", false, "bind the LAN address")
	flags.BoolVar(&config.DryRun, "dry-run", false, "print the unit without writing or enabling")
	if err := flags.Parse(args); err != nil {
		return config, err
	}
	if flags.NArg() > 0 {
		return config, fmt.Errorf("unexpected boot arguments: %s", strings.Join(flags.Args(), " "))
	}
	flags.Visit(func(item *flag.Flag) {
		if item.Name == "addr" {
			addrExplicit = true
		}
	})
	if config.LAN && addrExplicit {
		return config, fmt.Errorf("--lan and -addr cannot be used together")
	}
	return resolveBootConfig(config)
}

// resolveBootConfig turns user input into the explicit contract stored in the
// unit and metadata: absolute binary, state directory and working directory,
// an explicit listen address and the non-secret executable PATH.
func resolveBootConfig(config bootConfig) (bootConfig, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return config, fmt.Errorf("resolve current directory: %w", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return config, fmt.Errorf("resolve home directory: %w", err)
	}
	config.Home = home

	if strings.TrimSpace(config.Binary) == "" {
		executable, err := os.Executable()
		if err != nil {
			return config, fmt.Errorf("resolve executable: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(executable); err == nil {
			executable = resolved
		}
		config.Binary = executable
	}
	if config.Binary, err = bootAbsolutePath(config.Binary, cwd, "boot binary"); err != nil {
		return config, err
	}

	if strings.TrimSpace(config.StateDir) == "" {
		config.StateDir = filepath.Join(home, ".zen")
	}
	if config.StateDir, err = bootAbsolutePath(config.StateDir, cwd, "state directory"); err != nil {
		return config, err
	}

	if strings.TrimSpace(config.WorkDir) == "" {
		config.WorkDir = cwd
	}
	if config.WorkDir, err = bootAbsolutePath(config.WorkDir, cwd, "working directory"); err != nil {
		return config, err
	}
	if info, err := os.Stat(config.WorkDir); err != nil {
		return config, fmt.Errorf("boot working directory: %w", err)
	} else if !info.IsDir() {
		return config, fmt.Errorf("boot working directory is not a directory: %s", config.WorkDir)
	}

	if config.LAN {
		config.Addr = bootLANAddr
	} else if strings.TrimSpace(config.Addr) == "" {
		config.Addr = bootDefaultAddr
	}

	if strings.TrimSpace(config.PathEnv) == "" {
		config.PathEnv = os.Getenv("PATH")
	}
	if config.PathEnv, err = bootResolvePathEnv(config.PathEnv, cwd); err != nil {
		return config, err
	}
	return config, validateBootConfig(config)
}

// bootAbsolutePath expands a leading ~ and resolves value against base so the
// unit never depends on the user manager's working directory.
func bootAbsolutePath(value, base, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "~" || strings.HasPrefix(value, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home for %s: %w", label, err)
		}
		if value == "~" {
			value = home
		} else {
			value = filepath.Join(home, value[2:])
		}
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	if err := bootRejectUnitCharacters(label, value); err != nil {
		return "", err
	}
	return filepath.Clean(value), nil
}

// bootResolvePathEnv keeps the invoking PATH contract while making every entry
// absolute, so `zen boot install` from a relative directory cannot leak a
// relative executable path into the unit.
func bootResolvePathEnv(value, base string) (string, error) {
	entries := make([]string, 0, 8)
	for _, entry := range strings.Split(value, string(os.PathListSeparator)) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		resolved, err := bootAbsolutePath(entry, base, "PATH entry")
		if err != nil {
			return "", err
		}
		entries = append(entries, resolved)
	}
	if len(entries) == 0 {
		return "", errors.New("boot PATH has no usable entries")
	}
	return strings.Join(entries, string(os.PathListSeparator)), nil
}

// bootRejectUnitCharacters refuses characters that systemd unit values cannot
// carry safely (newline terminates or corrupts the directive; control
// characters have no legitimate place in these paths, addresses or PATH).
func bootRejectUnitCharacters(label, value string) error {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("boot %s contains an invalid control character", label)
		}
	}
	return nil
}

func validateBootConfig(config bootConfig) error {
	for _, item := range []struct {
		label string
		value string
	}{
		{"binary", config.Binary},
		{"state directory", config.StateDir},
		{"working directory", config.WorkDir},
		{"home directory", config.Home},
	} {
		if !filepath.IsAbs(item.value) {
			return fmt.Errorf("boot %s must be absolute: %s", item.label, item.value)
		}
		if err := bootRejectUnitCharacters(item.label, item.value); err != nil {
			return err
		}
	}
	if err := bootRejectUnitCharacters("address", config.Addr); err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(config.Addr)
	if err != nil || strings.TrimSpace(port) == "" {
		return fmt.Errorf("invalid boot address %q", config.Addr)
	}
	if strings.TrimSpace(config.PathEnv) == "" {
		return errors.New("boot PATH is required")
	}
	return bootRejectUnitCharacters("PATH", config.PathEnv)
}

func bootUnitPath() (string, error) {
	configHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home: %w", err)
		}
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "systemd", "user", bootServiceName), nil
}

func bootMetadataPath(unitPath string) string {
	return unitPath + bootMetadataSuffix
}

// systemdExecArgument quotes one ExecStart word. systemd doubles `%` specifiers
// and expands `$` variables inside command lines, so both are escaped; double
// quotes keep whitespace and apostrophes literal and are the only quoting
// systemd needs. Control characters are rejected before rendering.
func systemdExecArgument(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, `$`, `$$`)
	escaped = strings.ReplaceAll(escaped, `%`, `%%`)
	return `"` + escaped + `"`
}

// systemdPathValue formats a path setting such as WorkingDirectory=. systemd
// expands `%` specifiers there but treats `$`, quotes and backslashes
// literally, so only `%` is doubled and the value is never quoted.
func systemdPathValue(value string) string {
	return strings.ReplaceAll(value, "%", "%%")
}

// systemdEnvironmentValue formats one `NAME=value` assignment inside a quoted
// Environment= item. `%` specifiers expand (doubled here); `$` is literal; a
// backslash or double quote needs a C escape.
func systemdEnvironmentValue(value string) string {
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return strings.ReplaceAll(escaped, "%", "%%")
}

func renderBootUnit(config bootConfig) (string, error) {
	if err := validateBootConfig(config); err != nil {
		return "", err
	}
	arguments := []string{
		systemdExecArgument(config.Binary),
		"-state-dir",
		systemdExecArgument(config.StateDir),
	}
	if config.LAN {
		arguments = append(arguments, "-lan")
	} else {
		arguments = append(arguments, "-addr", systemdExecArgument(config.Addr))
	}
	var builder strings.Builder
	builder.WriteString(bootManagedMarker + "\n")
	builder.WriteString("[Unit]\n")
	builder.WriteString("Description=Zen daemon (same runtime as `zen` and `zen-dev`)\n")
	// default.target automatically orders itself after the units it wants, so
	// an explicit After=default.target would form an ordering cycle at boot.
	builder.WriteString("\n[Service]\n")
	builder.WriteString("Type=simple\n")
	builder.WriteString("WorkingDirectory=" + systemdPathValue(config.WorkDir) + "\n")
	builder.WriteString("ExecStart=" + strings.Join(arguments, " ") + "\n")
	builder.WriteString("Environment=\"HOME=" + systemdEnvironmentValue(config.Home) + "\"\n")
	builder.WriteString("Environment=\"PATH=" + systemdEnvironmentValue(config.PathEnv) + "\"\n")
	builder.WriteString("Restart=on-failure\n")
	builder.WriteString("RestartSec=5\n")
	// The daemon reuses the user's ordinary tmux server, a shared per-user
	// resource that can predate this unit. Only the daemon process belongs to
	// this unit, so a stop or restart must not kill the whole cgroup (tmux and
	// Worker sessions survive; systemd still terminates and restarts the
	// daemon itself).
	builder.WriteString("KillMode=process\n")
	builder.WriteString("\n[Install]\n")
	builder.WriteString("WantedBy=default.target\n")
	return builder.String(), nil
}

type bootMetadata struct {
	Schema   int    `json:"schema"`
	Binary   string `json:"binary"`
	StateDir string `json:"state_dir"`
	Addr     string `json:"addr"`
	LAN      bool   `json:"lan,omitempty"`
	WorkDir  string `json:"working_dir"`
	PathEnv  string `json:"path"`
	Home     string `json:"home"`
}

func bootMetadataFor(config bootConfig) bootMetadata {
	return bootMetadata{
		Schema:   bootMetadataSchema,
		Binary:   config.Binary,
		StateDir: config.StateDir,
		Addr:     config.Addr,
		LAN:      config.LAN,
		WorkDir:  config.WorkDir,
		PathEnv:  config.PathEnv,
		Home:     config.Home,
	}
}

// sameCommand compares the runtime-context fields. Any difference is a
// contract change the installer applies with an explicit managed restart.
func (metadata bootMetadata) sameCommand(config bootConfig) bool {
	return metadata.Binary == config.Binary &&
		metadata.StateDir == config.StateDir &&
		metadata.Addr == config.Addr &&
		metadata.LAN == config.LAN &&
		metadata.WorkDir == config.WorkDir
}

func (metadata bootMetadata) same(config bootConfig) bool {
	return metadata.Schema == bootMetadataSchema &&
		metadata.sameCommand(config) &&
		metadata.PathEnv == config.PathEnv &&
		metadata.Home == config.Home
}

func readBootMetadata(path string) (bootMetadata, error) {
	var metadata bootMetadata
	data, err := os.ReadFile(path)
	if err != nil {
		return metadata, err
	}
	if err := json.Unmarshal(data, &metadata); err != nil {
		return metadata, fmt.Errorf("decode boot metadata: %w", err)
	}
	if metadata.Schema != bootMetadataSchema {
		return metadata, fmt.Errorf("unsupported boot metadata schema %d", metadata.Schema)
	}
	return metadata, nil
}

func writeBootMetadata(path string, metadata bootMetadata) error {
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode boot metadata: %w", err)
	}
	data = append(data, '\n')
	return bootWriteFileAtomic(path, data, 0o644)
}

func bootWriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, perm); err != nil {
		return err
	}
	if err := os.Chmod(temporary, perm); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

// bootValidateLocalContext proves the installing user can actually run the
// binary and reach the state and working directories at user-manager runtime.
// It intentionally checks access as this unprivileged user: a root-only 0700
// directory is rejected here instead of producing a failing unit.
func bootValidateLocalContext(config bootConfig) error {
	info, err := os.Stat(config.Binary)
	if err != nil {
		return fmt.Errorf("boot binary: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("boot binary is not a regular file: %s", config.Binary)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("boot binary is not executable: %s", config.Binary)
	}
	if err := unix.Access(config.Binary, unix.X_OK); err != nil {
		return fmt.Errorf("boot binary is not executable by this user: %s: %w", config.Binary, err)
	}
	if err := unix.Access(config.WorkDir, unix.R_OK|unix.X_OK); err != nil {
		return fmt.Errorf("boot working directory is not accessible to this user: %s: %w", config.WorkDir, err)
	}
	stateDir := config.StateDir
	for {
		info, err := os.Stat(stateDir)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("boot state directory is not a directory: %s", stateDir)
			}
			if err := unix.Access(stateDir, unix.W_OK|unix.X_OK); err != nil {
				return fmt.Errorf("boot state directory is not writable by this user: %s: %w", stateDir, err)
			}
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("boot state directory: %w", err)
		}
		parent := filepath.Dir(stateDir)
		if parent == stateDir || !strings.HasPrefix(parent, string(os.PathSeparator)) {
			return fmt.Errorf("boot state directory has no writable parent: %s", config.StateDir)
		}
		stateDir = parent
	}
}

func bootStateLockHeld(stateDir string) (bool, error) {
	// A missing state directory cannot have an owner: the daemon creates the
	// directory before acquiring the lock, so probing must not create it.
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return false, nil
	}
	lock, acquired, err := control.TryAcquireLifecycleLock(stateDir)
	if err != nil {
		return false, err
	}
	if acquired {
		if err := lock.Close(); err != nil {
			return false, err
		}
		return false, nil
	}
	return true, nil
}

func bootAddrFree(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return listener.Close()
}

func bootUnitActive(runner bootRunner) (string, bool, error) {
	output, err := runner.run("systemctl", "--user", "is-active", bootServiceName)
	state := strings.TrimSpace(string(output))
	if err != nil && state == "" {
		return "", false, err
	}
	switch state {
	case "active", "activating", "reloading":
		return state, true, nil
	case "inactive", "deactivating", "failed", "unknown", "maintenance":
		return state, false, nil
	default:
		return state, false, fmt.Errorf("unexpected zen.service state %q: %s", state, strings.TrimSpace(string(output)))
	}
}

func bootUnitMainPID(runner bootRunner) (int, error) {
	output, err := runner.run("systemctl", "--user", "show", bootServiceName, "-p", "MainPID", "--value")
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return 0, fmt.Errorf("parse MainPID %q: %w", strings.TrimSpace(string(output)), err)
	}
	return pid, nil
}

func bootUnitEnabled(runner bootRunner) string {
	output, err := runner.run("systemctl", "--user", "is-enabled", bootServiceName)
	state := strings.TrimSpace(string(output))
	if state == "" && err != nil {
		return "unknown"
	}
	return state
}

// bootCheckFragmentPath confirms the user manager loaded the unit file this
// command wrote (HOME/XDG_CONFIG_HOME can differ from the login session). An
// unknown fragment path is a failed installation step, never a success
// default.
func bootCheckFragmentPath(runner bootRunner, path string) error {
	output, err := runner.run("systemctl", "--user", "show", bootServiceName, "-p", "FragmentPath", "--value")
	if err != nil {
		return fmt.Errorf("systemctl --user show %s -p FragmentPath: %v: %s", bootServiceName, err, strings.TrimSpace(string(output)))
	}
	actual := strings.TrimSpace(string(output))
	if actual == "" {
		return fmt.Errorf("systemd reports no FragmentPath for %s; refusing to claim it loaded the unit written to %s", bootServiceName, path)
	}
	if !bootSameFilePath(actual, path) {
		return fmt.Errorf("systemd loaded %s from %s, not %s; check HOME/XDG_CONFIG_HOME for the user manager", bootServiceName, actual, path)
	}
	return nil
}

func bootSameFilePath(left, right string) bool {
	resolvedLeft, err := filepath.EvalSymlinks(left)
	if err == nil {
		left = resolvedLeft
	}
	resolvedRight, err := filepath.EvalSymlinks(right)
	if err == nil {
		right = resolvedRight
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func bootProcessMatchesBinary(pid int, binary string) error {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return fmt.Errorf("read zen.service main process %d: %w", pid, err)
	}
	parts := strings.Split(string(data), "\x00")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		return fmt.Errorf("zen.service main process %d has no command line", pid)
	}
	if !bootSameFilePath(parts[0], binary) {
		return fmt.Errorf("zen.service main process %d runs %s, not the installed binary %s", pid, parts[0], binary)
	}
	return nil
}

func bootUnitControlGroup(runner bootRunner) (string, error) {
	output, err := runner.run("systemctl", "--user", "show", bootServiceName, "-p", "ControlGroup", "--value")
	if err != nil {
		return "", fmt.Errorf("systemctl --user show %s -p ControlGroup: %v: %s", bootServiceName, err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func bootLifecycleLockPath(stateDir string) (string, error) {
	socketPath, err := control.DefaultSocketPath(stateDir)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(socketPath), bootLifecycleLockName), nil
}

// bootStateLockPID returns the PID holding the state's lifecycle lock, or 0
// when no live process holds it. A non-empty controlGroup restricts the search
// to that systemd cgroup, which is how the installed unit is bound to the
// state it actually owns.
func bootStateLockPID(stateDir, controlGroup string) int {
	lockPath, err := bootLifecycleLockPath(stateDir)
	if err != nil {
		return 0
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		if controlGroup != "" && !bootPIDInCgroup(pid, controlGroup) {
			continue
		}
		if bootPIDHasOpenFile(pid, lockPath) {
			return pid
		}
	}
	return 0
}

func bootPIDInCgroup(pid int, controlGroup string) bool {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cgroup")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 3 {
			continue
		}
		path := strings.TrimSpace(fields[2])
		if path == controlGroup || strings.HasPrefix(path, controlGroup+"/") {
			return true
		}
	}
	return false
}

func bootPIDHasOpenFile(pid int, path string) bool {
	directory := "/proc/" + strconv.Itoa(pid) + "/fd"
	entries, err := os.ReadDir(directory)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(directory, entry.Name()))
		if err != nil {
			continue
		}
		if bootSameFilePath(target, path) {
			return true
		}
	}
	return false
}

func bootProbeAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "::1"
	}
	return net.JoinHostPort(host, port)
}

func bootProbeHealth(addr string) (string, error) {
	response, err := bootHTTPClient.Get("http://" + bootProbeAddr(addr) + "/health")
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("health returned http %d", response.StatusCode)
	}
	var payload struct {
		DaemonID string `json:"daemon_id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode health: %w", err)
	}
	if strings.TrimSpace(payload.DaemonID) == "" {
		return "", errors.New("health response has no daemon_id")
	}
	return payload.DaemonID, nil
}

// bootStateDaemonID derives the daemon identity from the same state directory
// the unit uses, read-only, so health can be attributed to the real owner.
func bootStateDaemonID(stateDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(stateDir, "identity.json"))
	if err != nil {
		return "", err
	}
	var identity struct {
		PrivateKeyHex string `json:"private_key_hex"`
	}
	if err := json.Unmarshal(data, &identity); err != nil {
		return "", fmt.Errorf("decode daemon identity: %w", err)
	}
	privateKey, err := hex.DecodeString(strings.TrimSpace(identity.PrivateKeyHex))
	if err != nil {
		return "", fmt.Errorf("parse daemon identity: %w", err)
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("daemon identity has %d bytes, want %d", len(privateKey), ed25519.PrivateKeySize)
	}
	publicKey, ok := ed25519.PrivateKey(privateKey).Public().(ed25519.PublicKey)
	if !ok {
		return "", errors.New("derive daemon public key")
	}
	sum := sha256.Sum256(publicKey)
	return hex.EncodeToString(sum[:]), nil
}

func bootFileHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

// bootOwnerSnapshot is the single ownership observation used by install
// verification and status. Unknown values stay zero/unset; success is only
// claimed by verified().
type bootOwnerSnapshot struct {
	InspectErr   error
	UnitState    string
	Active       bool
	MainPID      int
	MainMatches  bool
	ControlGroup string
	LockHeld     bool
	LockPID      int
	LockErr      error
	ServedID     string
	ExpectedID   string
	HealthErr    error
	StateIDErr   error
}

// bootInspectOwnership binds the installed unit to the state it actually
// owns without probing the daemon endpoint. Preflight uses it before any
// systemd change.
func bootInspectOwnership(config bootConfig, runner bootRunner) bootOwnerSnapshot {
	var snapshot bootOwnerSnapshot
	state, active, err := bootUnitActive(runner)
	snapshot.UnitState, snapshot.Active = state, active
	if err != nil {
		snapshot.InspectErr = fmt.Errorf("inspect %s: %w", bootServiceName, err)
	}
	if active {
		if pid, err := bootUnitMainPID(runner); err == nil {
			snapshot.MainPID = pid
		}
		if snapshot.MainPID > 0 {
			snapshot.MainMatches = bootProcessMatchesBinary(snapshot.MainPID, config.Binary) == nil
		}
		if group, err := bootUnitControlGroup(runner); err == nil {
			snapshot.ControlGroup = group
		}
	}
	if held, err := bootStateLockHeld(config.StateDir); err != nil {
		snapshot.LockErr = err
	} else {
		snapshot.LockHeld = held
	}
	if snapshot.ControlGroup != "" {
		snapshot.LockPID = bootStateLockPID(config.StateDir, snapshot.ControlGroup)
	}
	return snapshot
}

// bootInspectOwner adds health identity to the ownership observation used by
// install verification and status.
func bootInspectOwner(config bootConfig, runner bootRunner) bootOwnerSnapshot {
	snapshot := bootInspectOwnership(config, runner)
	if served, err := bootProbeHealth(config.Addr); err == nil {
		snapshot.ServedID = served
	} else {
		snapshot.HealthErr = err
	}
	if expected, err := bootStateDaemonID(config.StateDir); err == nil {
		snapshot.ExpectedID = expected
	} else {
		snapshot.StateIDErr = err
	}
	return snapshot
}

func (snapshot bootOwnerSnapshot) verified(config bootConfig) error {
	if snapshot.InspectErr != nil {
		return snapshot.InspectErr
	}
	if !snapshot.Active {
		return fmt.Errorf("%s is %s, not active", bootServiceName, snapshot.UnitState)
	}
	if snapshot.MainPID <= 0 {
		return fmt.Errorf("%s has no readable main process", bootServiceName)
	}
	if !snapshot.MainMatches {
		return fmt.Errorf("%s main process %d does not match the installed binary %s", bootServiceName, snapshot.MainPID, config.Binary)
	}
	if snapshot.ControlGroup == "" {
		return fmt.Errorf("cannot read %s ControlGroup; refusing to claim state ownership", bootServiceName)
	}
	if snapshot.LockErr != nil {
		return fmt.Errorf("inspect state ownership: %w", snapshot.LockErr)
	}
	if !snapshot.LockHeld {
		return fmt.Errorf("no daemon owns state directory %s", config.StateDir)
	}
	if snapshot.LockPID <= 0 {
		return fmt.Errorf("state directory %s is owned by a process outside %s", config.StateDir, bootServiceName)
	}
	if snapshot.HealthErr != nil {
		return fmt.Errorf("health %s: %w", bootProbeAddr(config.Addr), snapshot.HealthErr)
	}
	if snapshot.StateIDErr != nil {
		return fmt.Errorf("read state identity: %w", snapshot.StateIDErr)
	}
	if snapshot.ServedID != snapshot.ExpectedID {
		return fmt.Errorf("health endpoint %s serves daemon %s, installed state is %s", bootProbeAddr(config.Addr), snapshot.ServedID, snapshot.ExpectedID)
	}
	return nil
}

// describe renders the ownership state for `zen boot status` without
// claiming ownership when any fact is unknown.
func (snapshot bootOwnerSnapshot) describe() string {
	switch {
	case snapshot.InspectErr != nil:
		return "unknown (" + snapshot.InspectErr.Error() + ")"
	case snapshot.LockErr != nil:
		return "unknown (" + snapshot.LockErr.Error() + ")"
	case snapshot.LockPID > 0 && snapshot.Active:
		if snapshot.MainPID <= 0 || !snapshot.MainMatches {
			return fmt.Sprintf("state directory is owned by zen.service (PID %d); unit main process does not match the installed binary", snapshot.LockPID)
		}
		return fmt.Sprintf("state directory is owned by zen.service (PID %d)", snapshot.LockPID)
	case snapshot.LockPID > 0:
		return fmt.Sprintf("state directory is owned by zen.service cgroup process PID %d while the unit is %s", snapshot.LockPID, snapshot.UnitState)
	case snapshot.LockHeld:
		return "state lifecycle lock is held by a process outside zen.service; zen boot never kills it"
	case snapshot.Active && (snapshot.MainPID <= 0 || !snapshot.MainMatches):
		return "unit is active but its main process is not the installed binary"
	default:
		return "no running daemon owns the state directory"
	}
}

func (snapshot bootOwnerSnapshot) healthSummary(config bootConfig) string {
	if snapshot.HealthErr != nil {
		return fmt.Sprintf("unreachable (%s: %v)", bootProbeAddr(config.Addr), snapshot.HealthErr)
	}
	if snapshot.StateIDErr != nil {
		return fmt.Sprintf("endpoint responded daemon_id=%s; state identity unavailable (%v)", snapshot.ServedID, snapshot.StateIDErr)
	}
	if snapshot.ServedID != snapshot.ExpectedID {
		return fmt.Sprintf("mismatch: endpoint serves daemon_id=%s, installed state identity=%s", snapshot.ServedID, snapshot.ExpectedID)
	}
	return "ok daemon_id=" + snapshot.ServedID
}

func bootVerifyOwner(config bootConfig, runner bootRunner) error {
	deadline := time.Now().Add(bootVerifyTimeout)
	var last error
	for {
		snapshot := bootInspectOwner(config, runner)
		last = snapshot.verified(config)
		if last == nil {
			return nil
		}
		// A unit systemd reports as inactive or failed cannot become the owner
		// without external action; report the precise cause instead of waiting.
		if snapshot.InspectErr == nil && !snapshot.Active {
			return last
		}
		if time.Now().After(deadline) {
			return last
		}
		time.Sleep(bootVerifyInterval)
	}
}

func bootInstall(config bootConfig, runner bootRunner, out io.Writer) error {
	if runtime.GOOS != "linux" {
		return errors.New("zen boot install requires Linux user systemd")
	}
	if bootEffectiveUID() == 0 {
		return errors.New("zen boot install must run as the unprivileged user that owns the daemon state; refusing root")
	}
	unit, err := renderBootUnit(config)
	if err != nil {
		return err
	}
	if err := bootValidateLocalContext(config); err != nil {
		return err
	}
	path, err := bootUnitPath()
	if err != nil {
		return err
	}
	metadataPath := bootMetadataPath(path)
	existingUnit, readErr := os.ReadFile(path)
	if readErr == nil && !strings.Contains(string(existingUnit), bootManagedMarker) {
		return fmt.Errorf("refusing to replace a unit not managed by zen boot: %s", path)
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("read existing boot unit: %w", readErr)
	}
	if config.DryRun {
		fmt.Fprint(out, unit)
		return nil
	}

	// Preflight ownership before writing anything: the state must either be
	// free or owned by a process inside the active unit's own cgroup. A manual
	// daemon using another state (or the same state outside the unit) is
	// refused, never killed.
	preflight := bootInspectOwnership(config, runner)
	if preflight.InspectErr != nil {
		return preflight.InspectErr
	}
	switch {
	case preflight.LockErr != nil:
		return fmt.Errorf("inspect state ownership: %w", preflight.LockErr)
	case preflight.LockPID > 0:
		// The active unit's own cgroup holds the state lock: update in place.
	case preflight.LockHeld && preflight.Active && preflight.ControlGroup == "":
		return fmt.Errorf("state directory %s is locked, but %s ControlGroup is unreadable; refusing to claim ownership", config.StateDir, bootServiceName)
	case preflight.LockHeld:
		return fmt.Errorf("state directory %s is owned by a running process outside %s; stop that process first (zen boot never kills it)", config.StateDir, bootServiceName)
	}
	active, activeState := preflight.Active, preflight.UnitState

	previous, previousErr := readBootMetadata(metadataPath)
	writeUnit := readErr != nil || string(existingUnit) != unit
	writeMetadata := writeUnit || previousErr != nil || !previous.same(config)
	// Any runtime-context change restarts explicitly; a first install or a unit
	// whose previous contract cannot be attributed also restarts. Only an
	// unchanged, verified contract is left running.
	commandChanged := previousErr != nil || !previous.same(config)
	// Inactive units must find the address free. An active unit already holds
	// its address, so only a changed address needs a new availability check.
	if !active || (previousErr == nil && previous.Addr != config.Addr) {
		if err := bootAddrFree(config.Addr); err != nil {
			return fmt.Errorf("listen address %s is not available: %v; stop the process using it first", config.Addr, err)
		}
	}

	if writeUnit {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("create user unit directory: %w", err)
		}
		if err := bootWriteFileAtomic(path, []byte(unit), 0o644); err != nil {
			return fmt.Errorf("write boot unit: %w", err)
		}
	}
	if writeMetadata {
		if err := writeBootMetadata(metadataPath, bootMetadataFor(config)); err != nil {
			return fmt.Errorf("write boot metadata: %w", err)
		}
	}
	if writeUnit {
		if output, err := runner.run("systemctl", "--user", "daemon-reload"); err != nil {
			return fmt.Errorf("systemctl --user daemon-reload: %v: %s", err, strings.TrimSpace(string(output)))
		}
		if err := bootCheckFragmentPath(runner, path); err != nil {
			return err
		}
	}

	action := "Installed and started"
	restarted := false
	switch {
	case active && !commandChanged:
		action = "Already installed and running"
		if _, err := runner.run("systemctl", "--user", "enable", bootServiceName); err != nil {
			return fmt.Errorf("systemctl --user enable %s: %v", bootServiceName, err)
		}
	case active:
		action = "Updated and restarted"
		if output, err := runner.run("systemctl", "--user", "enable", bootServiceName); err != nil {
			return fmt.Errorf("systemctl --user enable %s: %v: %s", bootServiceName, err, strings.TrimSpace(string(output)))
		}
		if output, err := runner.run("systemctl", "--user", "restart", bootServiceName); err != nil {
			return fmt.Errorf("systemctl --user restart %s: %v: %s", bootServiceName, err, strings.TrimSpace(string(output)))
		}
		restarted = true
	default:
		if output, err := runner.run("systemctl", "--user", "enable", "--now", bootServiceName); err != nil {
			return fmt.Errorf("systemctl --user enable --now %s: %v: %s", bootServiceName, err, strings.TrimSpace(string(output)))
		}
		restarted = true
	}

	if err := bootVerifyOwner(config, runner); err != nil {
		if restarted {
			_, _ = runner.run("systemctl", "--user", "stop", bootServiceName)
		}
		return fmt.Errorf("zen.service did not become the verified owner (%s, previous state %s): %w; fix the reported cause and run systemctl --user start %s", config.StateDir, activeState, err, bootServiceName)
	}

	fmt.Fprintf(out, "%s %s\n", action, path)
	fmt.Fprintf(out, "Binary: %s\n", config.Binary)
	fmt.Fprintf(out, "State directory: %s\n", config.StateDir)
	fmt.Fprintf(out, "Address: %s\n", config.Addr)
	fmt.Fprintf(out, "Working directory: %s\n", config.WorkDir)
	if active && !commandChanged && !writeUnit {
		fmt.Fprintln(out, "Unit unchanged; the live daemon was not restarted.")
	} else if active && !commandChanged {
		fmt.Fprintln(out, "Unit file updated; the live daemon was not restarted because the runtime contract is unchanged.")
	}
	fmt.Fprintln(out, "tmux and Worker sessions are not owned by this unit and are never stopped here.")

	linger, err := bootLingerState(runner)
	if err != nil {
		return err
	}
	if !linger {
		operator := "sudo loginctl enable-linger " + currentUserName()
		if output, err := runner.run("loginctl", "enable-linger", currentUserName()); err != nil {
			fmt.Fprintf(out, "Boot before login still requires lingering; run exactly:\n  %s\n", operator)
			return fmt.Errorf("boot linger required: %s (%s)", operator, strings.TrimSpace(string(output)))
		}
	}
	fmt.Fprintln(out, "Linger: enabled (starts before an interactive login)")
	fmt.Fprintln(out, "Stop/rollback: zen boot uninstall")
	return nil
}

func bootLingerState(runner bootRunner) (bool, error) {
	output, err := runner.run("loginctl", "show-user", currentUserName(), "-p", "Linger", "--value")
	if err != nil {
		return false, fmt.Errorf("loginctl show-user %s: %v: %s", currentUserName(), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)) == "yes", nil
}

func currentUserName() string {
	if current, err := user.Current(); err == nil && current.Username != "" {
		return current.Username
	}
	return os.Getenv("USER")
}

func bootStatus(runner bootRunner, out io.Writer) error {
	path, err := bootUnitPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintf(out, "%s: not installed (%s)\n", bootServiceName, path)
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), bootManagedMarker) {
		fmt.Fprintf(out, "%s: present but not managed by zen boot (%s)\n", bootServiceName, path)
		return nil
	}
	fmt.Fprintf(out, "unit: %s\n", path)
	fmt.Fprintf(out, "enabled: %s\n", bootUnitEnabled(runner))
	state, _, activeErr := bootUnitActive(runner)
	if activeErr != nil {
		fmt.Fprintf(out, "service: unknown (%v)\n", activeErr)
	} else {
		fmt.Fprintf(out, "service: %s\n", state)
	}

	metadata, metadataErr := readBootMetadata(bootMetadataPath(path))
	if metadataErr != nil {
		fmt.Fprintf(out, "configuration: unavailable (%v); reinstall with `zen boot install`\n", metadataErr)
	} else {
		fmt.Fprintf(out, "binary: %s\n", metadata.Binary)
		if hash, err := bootFileHash(metadata.Binary); err == nil {
			fmt.Fprintf(out, "binary sha256: %s\n", hash)
		} else {
			fmt.Fprintf(out, "binary sha256: unavailable (%v)\n", err)
		}
		fmt.Fprintf(out, "state directory: %s\n", metadata.StateDir)
		fmt.Fprintf(out, "address: %s\n", metadata.Addr)
		fmt.Fprintf(out, "working directory: %s\n", metadata.WorkDir)
		fmt.Fprintf(out, "PATH: %s\n", metadata.PathEnv)
		probeConfig := bootConfig{Binary: metadata.Binary, StateDir: metadata.StateDir, Addr: metadata.Addr}
		snapshot := bootInspectOwner(probeConfig, runner)
		if snapshot.MainPID > 0 {
			fmt.Fprintf(out, "main pid: %d\n", snapshot.MainPID)
		}
		fmt.Fprintf(out, "ownership: %s\n", snapshot.describe())
		fmt.Fprintf(out, "health: %s\n", snapshot.healthSummary(probeConfig))
	}

	if linger, err := bootLingerState(runner); err == nil {
		fmt.Fprintf(out, "linger: %t\n", linger)
	} else {
		fmt.Fprintf(out, "linger: unknown (%v)\n", err)
	}
	return nil
}

func bootUninstall(runner bootRunner, out io.Writer) error {
	path, err := bootUnitPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintf(out, "%s not installed (%s)\n", bootServiceName, path)
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), bootManagedMarker) {
		return fmt.Errorf("refusing to remove a unit not managed by zen boot: %s", path)
	}
	metadataPath := bootMetadataPath(path)
	metadata, metadataErr := readBootMetadata(bootMetadataPath(path))

	// Capture the unit's cgroup before stopping so a DEV leftover daemon can
	// still be attributed to this unit after systemd reports it inactive.
	controlGroup := ""
	if group, err := bootUnitControlGroup(runner); err == nil {
		controlGroup = group
	}

	// Stop first and keep the unit and metadata on any failure so the
	// configuration stays recoverable; never remove a still-running owner.
	if output, err := runner.run("systemctl", "--user", "stop", bootServiceName); err != nil {
		return fmt.Errorf("systemctl --user stop %s: %v: %s (unit and configuration retained; retry `zen boot uninstall`)", bootServiceName, err, strings.TrimSpace(string(output)))
	}
	if state, active, err := bootUnitActive(runner); err != nil {
		return fmt.Errorf("confirm %s stopped: %w (unit and configuration retained)", bootServiceName, err)
	} else if active {
		return fmt.Errorf("%s is still %s after stop (unit and configuration retained)", bootServiceName, state)
	}

	// An own daemon can outlive the unit when KillMode=process leaves a child
	// behind (the DEV watcher/daemon pair). Detect it by lock holder inside the
	// captured unit cgroup and retain everything instead of deleting metadata.
	outsideOwner := false
	if metadataErr == nil {
		ownerPID := 0
		if controlGroup != "" {
			ownerPID = bootStateLockPID(metadata.StateDir, controlGroup)
		}
		switch {
		case ownerPID > 0:
			return fmt.Errorf("zen.service cgroup process %d still owns state directory %s after stop; configuration retained; stop that process explicitly before removing the unit", ownerPID, metadata.StateDir)
		case controlGroup == "":
			if bootStateLockPID(metadata.StateDir, "") > 0 {
				return fmt.Errorf("state directory %s is still owned after stop, but zen.service ControlGroup is unknown; configuration retained; stop the owning process explicitly", metadata.StateDir)
			}
		default:
			outsideOwner = bootStateLockPID(metadata.StateDir, "") > 0
		}
	}

	if output, err := runner.run("systemctl", "--user", "disable", bootServiceName); err != nil {
		return fmt.Errorf("systemctl --user disable %s: %v: %s (unit and configuration retained)", bootServiceName, err, strings.TrimSpace(string(output)))
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove boot unit: %w", err)
	}
	if err := os.Remove(metadataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove boot metadata: %w", err)
	}
	if output, err := runner.run("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %v: %s (unit removed but systemd cache not reloaded)", err, strings.TrimSpace(string(output)))
	}
	fmt.Fprintf(out, "Removed %s; daemon state and pairing are untouched\n", path)
	if outsideOwner {
		fmt.Fprintf(out, "Note: state directory %s is owned by a process outside zen.service; it was not stopped or modified\n", metadata.StateDir)
	}
	return nil
}
