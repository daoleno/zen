package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// `zen boot` is the single optional boot-persistence entry for the SAME runtime
// used by `zen` and the DEV runner: it renders a standard systemd user unit for
// the reviewed binary and does not introduce a second identity, state
// directory or supervisor. Process restart never touches the external tmux
// server or Worker sessions.

const bootManagedMarker = "# Managed by zen boot install"

type bootConfig struct {
	Binary   string
	StateDir string
	Addr     string
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
		config, err := parseBootConfig("zen boot status", args[1:])
		if err != nil {
			return err
		}
		return bootStatus(config, execBootRunner{}, os.Stdout)
	case "uninstall":
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
	fmt.Fprintln(w, "  status     Show the unit, binary hash, service state, linger and /health")
	fmt.Fprintln(w, "  uninstall  Disable and remove only the unit this command installed")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Install flags:")
	fmt.Fprintln(w, "  -binary <path>    zen ELF to run (default: this executable)")
	fmt.Fprintln(w, "  -state-dir <path> daemon state directory (default: ~/.zen)")
	fmt.Fprintln(w, "  -addr <host:port> daemon listen address (default: 127.0.0.1:9876)")
	fmt.Fprintln(w, "  -lan              bind the LAN address like `zen -lan`")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "The unit starts the same unprivileged runtime before an interactive")
	fmt.Fprintln(w, "login only when the user manager lingers (loginctl enable-linger).")
	fmt.Fprintln(w, "Remote desktop lock/login still needs the administrator broker install.")
}

func parseBootConfig(name string, args []string) (bootConfig, error) {
	var config bootConfig
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&config.Binary, "binary", "", "zen binary to run")
	flags.StringVar(&config.StateDir, "state-dir", "", "daemon state directory")
	flags.StringVar(&config.Addr, "addr", "", "daemon listen address")
	flags.BoolVar(&config.LAN, "lan", false, "bind LAN address")
	flags.BoolVar(&config.DryRun, "dry-run", false, "print the unit without writing or enabling")
	if err := flags.Parse(args); err != nil {
		return config, err
	}
	if flags.NArg() > 0 {
		return config, fmt.Errorf("unexpected boot arguments: %s", strings.Join(flags.Args(), " "))
	}
	if strings.TrimSpace(config.Binary) == "" {
		executable, err := os.Executable()
		if err != nil {
			return config, fmt.Errorf("resolve executable: %w", err)
		}
		resolved, err := filepath.EvalSymlinks(executable)
		if err == nil {
			executable = resolved
		}
		config.Binary = executable
	}
	absolute, err := filepath.Abs(config.Binary)
	if err != nil {
		return config, fmt.Errorf("resolve binary path: %w", err)
	}
	config.Binary = absolute
	return config, nil
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
	return filepath.Join(configHome, "systemd", "user", "zen.service"), nil
}

// systemdExecArgument quotes one ExecStart argument. systemd expands `%`
// specifiers even inside quotes, so it is doubled.
func systemdExecArgument(value string) string {
	escaped := strings.ReplaceAll(value, "%", "%%")
	escaped = strings.ReplaceAll(escaped, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	if escaped == "" || strings.ContainsAny(escaped, " \t") {
		return `"` + escaped + `"`
	}
	return escaped
}

func renderBootUnit(config bootConfig) (string, error) {
	arguments := []string{systemdExecArgument(config.Binary)}
	if strings.TrimSpace(config.StateDir) != "" {
		arguments = append(arguments, "-state-dir", systemdExecArgument(config.StateDir))
	}
	if strings.TrimSpace(config.Addr) != "" {
		arguments = append(arguments, "-addr", systemdExecArgument(config.Addr))
	}
	if config.LAN {
		arguments = append(arguments, "-lan")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	var builder strings.Builder
	builder.WriteString(bootManagedMarker + "\n")
	builder.WriteString("[Unit]\n")
	builder.WriteString("Description=Zen daemon (same runtime as `zen` and `zen-dev`)\n")
	builder.WriteString("After=default.target\n")
	builder.WriteString("\n[Service]\n")
	builder.WriteString("Type=simple\n")
	builder.WriteString("ExecStart=" + strings.Join(arguments, " ") + "\n")
	builder.WriteString("Environment=HOME=" + systemdExecArgument(home) + "\n")
	builder.WriteString("Restart=on-failure\n")
	builder.WriteString("RestartSec=5\n")
	builder.WriteString("\n[Install]\n")
	builder.WriteString("WantedBy=default.target\n")
	return builder.String(), nil
}

func bootInstall(config bootConfig, runner bootRunner, out io.Writer) error {
	if runtime.GOOS != "linux" {
		return errors.New("zen boot install requires Linux user systemd")
	}
	info, err := os.Stat(config.Binary)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("boot binary is not an executable regular file: %s", config.Binary)
	}
	unit, err := renderBootUnit(config)
	if err != nil {
		return err
	}
	path, err := bootUnitPath()
	if err != nil {
		return err
	}
	if existing, err := os.ReadFile(path); err == nil && !strings.Contains(string(existing), bootManagedMarker) {
		return fmt.Errorf("refusing to replace a unit not managed by zen boot: %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read existing boot unit: %w", err)
	}
	if config.DryRun {
		fmt.Fprint(out, unit)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create user unit directory: %w", err)
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, []byte(unit), 0o644); err != nil {
		return fmt.Errorf("write boot unit: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("commit boot unit: %w", err)
	}
	if output, err := runner.run("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %v: %s", err, strings.TrimSpace(string(output)))
	}
	if output, err := runner.run("systemctl", "--user", "enable", "--now", "zen.service"); err != nil {
		return fmt.Errorf("systemctl --user enable --now zen.service: %v: %s", err, strings.TrimSpace(string(output)))
	}
	linger, err := bootLingerState(runner)
	if err != nil {
		return err
	}
	if !linger {
		operator := "sudo loginctl enable-linger " + currentUserName()
		if output, err := runner.run("loginctl", "enable-linger", currentUserName()); err != nil {
			fmt.Fprintf(out, "Installed and started %s.\n", path)
			fmt.Fprintf(out, "Boot before login still requires lingering; run exactly:\n  %s\n", operator)
			return fmt.Errorf("boot linger required: %s (%s)", operator, strings.TrimSpace(string(output)))
		}
	}
	fmt.Fprintf(out, "Installed and started %s\n", path)
	fmt.Fprintf(out, "Binary: %s\n", config.Binary)
	fmt.Fprintf(out, "Linger: enabled (starts before an interactive login)\n")
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

func bootStatus(config bootConfig, runner bootRunner, out io.Writer) error {
	path, err := bootUnitPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintf(out, "zen.service: not installed (%s)\n", path)
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), bootManagedMarker) {
		fmt.Fprintf(out, "zen.service: present but not managed by zen boot (%s)\n", path)
		return nil
	}
	fmt.Fprintf(out, "unit: %s\n", path)
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			fmt.Fprintln(out, line)
		}
	}
	for _, query := range []struct {
		label string
		args  []string
	}{
		{"enabled", []string{"systemctl", "--user", "is-enabled", "zen.service"}},
		{"active", []string{"systemctl", "--user", "is-active", "zen.service"}},
	} {
		if output, err := runner.run(query.args[0], query.args[1:]...); err == nil || len(output) > 0 {
			fmt.Fprintf(out, "%s: %s\n", query.label, strings.TrimSpace(string(output)))
		}
	}
	if linger, err := bootLingerState(runner); err == nil {
		fmt.Fprintf(out, "linger: %t\n", linger)
	}
	health := bootHealth(config)
	fmt.Fprintf(out, "health: %s\n", health)
	return nil
}

func bootHealth(config bootConfig) string {
	address := strings.TrimSpace(config.Addr)
	if address == "" {
		address = "127.0.0.1:9876"
	}
	if strings.HasPrefix(address, "0.0.0.0:") {
		address = "127.0.0.1:" + strings.TrimPrefix(address, "0.0.0.0:")
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://" + address + "/health")
	if err != nil {
		return "unreachable (" + err.Error() + ")"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Sprintf("http %d", response.StatusCode)
	}
	return "ok"
}

func bootUninstall(runner bootRunner, out io.Writer) error {
	path, err := bootUnitPath()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintf(out, "zen.service not installed (%s)\n", path)
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), bootManagedMarker) {
		return fmt.Errorf("refusing to remove a unit not managed by zen boot: %s", path)
	}
	if output, err := runner.run("systemctl", "--user", "disable", "--now", "zen.service"); err != nil {
		fmt.Fprintf(out, "systemctl --user disable --now zen.service: %v: %s\n", err, strings.TrimSpace(string(output)))
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove boot unit: %w", err)
	}
	if output, err := runner.run("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl --user daemon-reload: %v: %s", err, strings.TrimSpace(string(output)))
	}
	fmt.Fprintf(out, "Removed %s; daemon state and pairing are untouched\n", path)
	return nil
}
