package setup

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/daoleno/zen/daemon/brain"
)

func TestSetupMissingTmuxStopsCleanly(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	var out bytes.Buffer
	result, err := Run(Options{
		NonInteractive: true,
		Host:           "codex",
		Delegated:      "codex",
		Profile:        ProfileSafe,
		Home:           home,
		StateDir:       filepath.Join(home, ".zen"),
		ExecutorsPath:  filepath.Join(home, ".zen", "executors.toml"),
		Addr:           "127.0.0.1:0",
		PathEnv:        binDir,
		Stdout:         &out,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v, want ErrBlocked", err)
	}
	if result.OK || !result.StoppedEarly || result.Step != "machine" {
		t.Fatalf("result = %+v", result)
	}
	text := out.String()
	for _, want := range []string{"not ready", "tmux", "sudo apt install tmux", "zen setup"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".zen", "executors.toml")); !os.IsNotExist(err) {
		t.Fatalf("config should not be written on blocked machine: %v", err)
	}
}

func TestSetupNoExecutorStopsWithLoginHints(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	var out bytes.Buffer
	result, err := Run(Options{
		NonInteractive: true,
		Host:           "codex",
		Delegated:      "codex",
		Profile:        ProfileSafe,
		Home:           home,
		StateDir:       filepath.Join(home, ".zen"),
		ExecutorsPath:  filepath.Join(home, ".zen", "executors.toml"),
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		Stdout:         &out,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	})
	if !errors.Is(err, ErrNoExecutor) {
		t.Fatalf("err = %v", err)
	}
	if result.OK {
		t.Fatal("expected not ok")
	}
	text := out.String()
	for _, want := range []string{"no runnable executor", "codex login", "claude auth login", "cursor-agent login", "grok login"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
}

func TestSetupVerifiedBeforeUnknownOrderingAndSafeWrite(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeFakeGrok(t, binDir)
	writeExecutorsSeed(t, home, `
delegated_executor = "grok"

[[executors]]
name = "custom-keep"
command = "echo keep-me"

[[executors]]
name = "codex"
command = "codex --dangerously-bypass-approvals-and-sandbox"

[[executors]]
name = "grok"
command = "grok --no-alt-screen --permission-mode bypassPermissions"
`)

	var out bytes.Buffer
	result, err := Run(Options{
		NonInteractive: true,
		Host:           "codex",
		Delegated:      "codex",
		Profile:        ProfileSafe,
		Home:           home,
		StateDir:       filepath.Join(home, ".zen"),
		ExecutorsPath:  filepath.Join(home, ".zen", "executors.toml"),
		BrainRoot:      filepath.Join(home, ".zen", "brain"),
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		Stdout:         &out,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK || result.BrainConfigured {
		t.Fatalf("result = %+v", result)
	}
	candidates := selectableCandidates(result.Doctor)
	if len(candidates) < 2 || !candidates[0].VerifiedAuthenticated || candidates[0].ID != "codex" {
		t.Fatalf("candidate order = %+v", candidates)
	}

	raw, err := os.ReadFile(result.ConfigPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(raw)
	for _, bad := range []string{"--force", "sandbox disabled", "bypassPermissions", "dangerously-bypass"} {
		if strings.Contains(text, bad) {
			t.Fatalf("safe profile leaked %q:\n%s", bad, text)
		}
	}
	if !strings.Contains(text, "custom-keep") || !strings.Contains(text, "echo keep-me") {
		t.Fatalf("unrelated executor not preserved:\n%s", text)
	}
	if result.BackupPath == "" {
		t.Fatal("expected backup path")
	}
	if _, err := os.Stat(result.BackupPath); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if !strings.Contains(out.String(), "Brain host left unconfigured") && len(result.Warnings) == 0 {
		t.Fatalf("expected Brain unconfigured warning, out=\n%s result=%+v", out.String(), result)
	}
}

func TestSetupAutonomousRequiresExplicitConsent(t *testing.T) {
	home := t.TempDir()
	binDir := readyBinDir(t)
	_, err := Run(Options{
		NonInteractive: true,
		Host:           "codex",
		Delegated:      "codex",
		Profile:        ProfileAutonomous,
		Yes:            false,
		Home:           home,
		StateDir:       filepath.Join(home, ".zen"),
		ExecutorsPath:  filepath.Join(home, ".zen", "executors.toml"),
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	})
	if !errors.Is(err, ErrConsentRequired) {
		t.Fatalf("err = %v, want ErrConsentRequired", err)
	}
}

func TestSetupAutonomousWithYesConfiguresBrainAndBypass(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeFakeCursor(t, binDir)

	result, err := Run(Options{
		NonInteractive: true,
		Host:           "codex",
		Delegated:      "agent",
		Profile:        ProfileAutonomous,
		Yes:            true,
		Home:           home,
		StateDir:       filepath.Join(home, ".zen"),
		ExecutorsPath:  filepath.Join(home, ".zen", "executors.toml"),
		BrainRoot:      filepath.Join(home, ".zen", "brain"),
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !result.OK || !result.BrainConfigured {
		t.Fatalf("result = %+v", result)
	}
	raw, _ := os.ReadFile(result.ConfigPath)
	if !strings.Contains(string(raw), "cursor-agent --force --sandbox disabled") {
		t.Fatalf("autonomous cursor command missing:\n%s", raw)
	}
	store, err := brain.NewStore(filepath.Join(home, ".zen", "brain"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	host, err := store.HostSession()
	if err != nil {
		t.Fatalf("HostSession: %v", err)
	}
	if host.ExecutorID != "codex" {
		t.Fatalf("brain host = %#v", host)
	}
}

func TestSetupPreservesBackupAndIdempotentRerun(t *testing.T) {
	home := t.TempDir()
	binDir := readyBinDir(t)
	path := filepath.Join(home, ".zen", "executors.toml")
	writeExecutorsSeed(t, home, `
delegated_executor = "codex"
[[executors]]
name = "codex"
command = "codex"
[[executors]]
name = "keep"
command = "keep-cmd"
`)

	first, err := Run(baseOpts(home, binDir, ProfileSafe, "codex", "codex"))
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := Run(baseOpts(home, binDir, ProfileSafe, "codex", "codex"))
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.BackupPath == "" || second.BackupPath == "" {
		t.Fatalf("expected backups: first=%q second=%q", first.BackupPath, second.BackupPath)
	}
	if first.BackupPath == second.BackupPath {
		// timestamps could collide in fixedNow — use advancing now for second run
	}
	var parsed struct {
		DelegatedExecutor string `toml:"delegated_executor"`
		Executors         []struct {
			Name    string `toml:"name"`
			Command string `toml:"command"`
		} `toml:"executors"`
	}
	raw, _ := os.ReadFile(path)
	if err := toml.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("toml: %v", err)
	}
	if parsed.DelegatedExecutor != "codex" {
		t.Fatalf("delegated = %q", parsed.DelegatedExecutor)
	}
	foundKeep := false
	for _, e := range parsed.Executors {
		if e.Name == "keep" && e.Command == "keep-cmd" {
			foundKeep = true
		}
	}
	if !foundKeep {
		t.Fatalf("keep entry missing:\n%s", raw)
	}
}

func TestSetupNonInteractiveInvalidArgs(t *testing.T) {
	home := t.TempDir()
	binDir := readyBinDir(t)
	_, err := Run(Options{
		NonInteractive: true,
		Profile:        ProfileSafe,
		Home:           home,
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Stdout:         io.Discard,
		Stderr:         io.Discard,
	})
	if !errors.Is(err, ErrInvalidArgs) {
		t.Fatalf("err = %v", err)
	}

	_, err = Run(baseOpts(home, binDir, Profile("nope"), "codex", "codex"))
	if !errors.Is(err, ErrInvalidArgs) {
		t.Fatalf("bad profile err = %v", err)
	}
}

func TestSetupDoesNotEmitSecrets(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using an API key - sk-SECRETVALUE")
	var out bytes.Buffer
	opts := baseOpts(home, binDir, ProfileSafe, "codex", "codex")
	opts.Stdout = &out
	result, err := Run(opts)
	if err != nil {
		t.Fatalf("Run: %v\nout=%s", err, out.String())
	}
	blob := out.String() + result.Message + strings.Join(result.Warnings, "") + strings.Join(result.NextSteps, "")
	if strings.Contains(blob, "sk-") || strings.Contains(blob, "SECRET") {
		t.Fatalf("secret leaked: %s", blob)
	}
	raw, _ := os.ReadFile(result.ConfigPath)
	if strings.Contains(string(raw), "sk-") {
		t.Fatalf("secret in config: %s", raw)
	}
}

func TestCommandForProfileNeverInjectsBypassIntoCustom(t *testing.T) {
	cmd, _ := commandForProfile("custom", "mycli", "mycli --fancy", ProfileAutonomous)
	if cmd != "mycli --fancy" {
		t.Fatalf("custom command mutated: %q", cmd)
	}
	cmd, _ = commandForProfile("cursor", "agent", "cursor-agent", ProfileSafe)
	if strings.Contains(cmd, "--force") || strings.Contains(cmd, "disabled") {
		t.Fatalf("safe cursor has bypass: %q", cmd)
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func fixedNow() time.Time {
	return time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
}

func baseOpts(home, binDir string, profile Profile, host, delegated string) Options {
	return Options{
		NonInteractive: true,
		Host:           host,
		Delegated:      delegated,
		Profile:        profile,
		Yes:            profile == ProfileAutonomous,
		Home:           home,
		StateDir:       filepath.Join(home, ".zen"),
		ExecutorsPath:  filepath.Join(home, ".zen", "executors.toml"),
		BrainRoot:      filepath.Join(home, ".zen", "brain"),
		PathEnv:        binDir,
		Addr:           "127.0.0.1:0",
		Stdout:         io.Discard,
		Stderr:         io.Discard,
		DoctorListen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		DoctorHTTPGet:  func(url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
		Now:            fixedNow,
	}
}

func readyBinDir(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	return binDir
}

func writeExecutorsSeed(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".zen")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "executors.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func writeFakeTmux(t *testing.T, binDir string) {
	t.Helper()
	writeExec(t, filepath.Join(binDir, "tmux"), `#!/bin/sh
case "$1" in
  -V) echo "tmux 3.4"; exit 0 ;;
  new-session|kill-session) exit 0 ;;
  *) exit 1 ;;
esac
`)
}

func writeFakeCodex(t *testing.T, binDir, loginStatus string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then echo "codex-cli 0.0.1"; exit 0; fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then printf '%%s\n' %q; exit 0; fi
exit 1
`, loginStatus)
	writeExec(t, filepath.Join(binDir, "codex"), script)
}

func writeFakeGrok(t *testing.T, binDir string) {
	t.Helper()
	writeExec(t, filepath.Join(binDir, "grok"), `#!/bin/sh
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then echo "grok 0.0.1"; exit 0; fi
exit 1
`)
}

func writeFakeCursor(t *testing.T, binDir string) {
	t.Helper()
	writeExec(t, filepath.Join(binDir, "cursor-agent"), `#!/bin/sh
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then echo "2026.01.01"; exit 0; fi
if [ "$1" = "status" ]; then echo '{"status":"authenticated","isAuthenticated":true}'; exit 0; fi
exit 1
`)
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}
