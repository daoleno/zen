package doctor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunCleanPathNotReadyButValidJSON(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	stateDir := filepath.Join(home, ".zen")

	report, err := Run(Options{
		Home:     home,
		StateDir: stateDir,
		Addr:     "127.0.0.1:0",
		PathEnv:  binDir,
		Now:      func() time.Time { return time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC) },
		Listen: func(network, address string) (io.Closer, error) {
			return nopCloser{}, nil
		},
		HTTPGet: func(ctx context.Context, url string) (int, []byte, error) {
			return 0, nil, errors.New("refused")
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Ready {
		t.Fatal("expected not ready on clean PATH")
	}
	if report.Tmux.Status != StatusFail || report.Tmux.Remediation != RemediationInstallTmux {
		t.Fatalf("tmux = %+v", report.Tmux)
	}
	if report.Executors.Status != StatusFail || report.Executors.Remediation != RemediationInstallExecutor {
		t.Fatalf("executors = %+v", report.Executors)
	}
	if report.Executors.UsableCount != 0 {
		t.Fatalf("usable_count = %d", report.Executors.UsableCount)
	}
	if len(report.Tmux.InstallHints) != 4 {
		t.Fatalf("install hints = %d", len(report.Tmux.InstallHints))
	}

	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var roundtrip map[string]any
	if err := json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	for _, key := range []string{"ready", "platform", "tmux", "state_dir", "listen", "executors", "checks", "remediations"} {
		if _, ok := roundtrip[key]; !ok {
			t.Fatalf("missing JSON key %q in %s", key, raw)
		}
	}
	if strings.Contains(string(raw), "sk-") {
		t.Fatalf("JSON leaked secret-like token: %s", raw)
	}

	var human bytes.Buffer
	if err := WriteHuman(&human, report); err != nil {
		t.Fatalf("WriteHuman: %v", err)
	}
	text := human.String()
	for _, want := range []string{
		"NOT READY",
		"sudo apt install tmux",
		"sudo dnf install tmux",
		"sudo pacman -S tmux",
		"brew install tmux",
		"install_tmux",
		"install_executor",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("human output missing %q:\n%s", want, text)
		}
	}
}

func TestRunReadyWithTmuxAndAuthenticatedExecutor(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	stateDir := filepath.Join(home, ".zen")
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeExecutorsTOML(t, home, `delegated_executor = "codex"

[[executors]]
name = "codex"
command = "codex"
`)

	report, err := Run(Options{
		Home:          home,
		StateDir:      stateDir,
		ExecutorsPath: filepath.Join(home, ".zen", "executors.toml"),
		Addr:          "127.0.0.1:0",
		PathEnv:       binDir,
		Now:           func() time.Time { return time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC) },
		Listen: func(network, address string) (io.Closer, error) {
			return nopCloser{}, nil
		},
		HTTPGet: func(ctx context.Context, url string) (int, []byte, error) {
			return 0, nil, errors.New("refused")
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Ready {
		t.Fatalf("expected ready, report=%+v", report)
	}
	if report.Tmux.Status != StatusOK || !report.Tmux.Functional {
		t.Fatalf("tmux = %+v", report.Tmux)
	}
	if report.StateDir.Status != StatusOK || report.StateDir.Path != stateDir {
		t.Fatalf("state_dir = %+v", report.StateDir)
	}
	if report.Executors.UsableCount < 1 || report.Executors.RecommendedHost != "codex" {
		t.Fatalf("executors = %+v", report.Executors)
	}
	codex := findExecutor(report, "codex")
	if codex == nil || codex.Auth != AuthAuthenticated || !codex.Usable || !codex.VerifiedAuthenticated {
		t.Fatalf("codex = %+v", codex)
	}
	if report.Executors.RecommendationConfidence != ConfidenceVerified || report.Executors.VerifiedCount < 1 {
		t.Fatalf("executors = %+v", report.Executors)
	}
}

func TestAuthUnknownDistinctFromUnauthenticated(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Not logged in")
	writeFakeGrok(t, binDir)
	writeExecutorsTOML(t, home, `
[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "grok"
command = "grok"
`)

	report, err := Run(Options{
		Home:          home,
		StateDir:      filepath.Join(home, ".zen"),
		ExecutorsPath: filepath.Join(home, ".zen", "executors.toml"),
		Addr:          "127.0.0.1:0",
		PathEnv:       binDir,
		Listen:        func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		HTTPGet:       func(ctx context.Context, url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Grok is auth-unknown but runnable, so overall readiness is allowed with warnings.
	if !report.Ready {
		t.Fatalf("expected ready with grok unknown candidate, report=%+v", report)
	}
	if report.Executors.Status != StatusWarn {
		t.Fatalf("executors status = %s, want warn", report.Executors.Status)
	}
	codex := findExecutor(report, "codex")
	grok := findExecutor(report, "grok")
	if codex == nil || codex.Auth != AuthUnauthenticated || codex.Usable || codex.Runnable {
		t.Fatalf("codex = %+v", codex)
	}
	if grok == nil || grok.Auth != AuthUnknown || !grok.Usable || !grok.Runnable || grok.VerifiedAuthenticated {
		t.Fatalf("grok = %+v", grok)
	}
	if codex.Remediation != RemediationAuthenticateExec {
		t.Fatalf("codex remediation = %q", codex.Remediation)
	}
	if report.Executors.RecommendedHost != "grok" || report.Executors.RecommendationConfidence != ConfidenceUnverified {
		t.Fatalf("recommendations = host=%s confidence=%s", report.Executors.RecommendedHost, report.Executors.RecommendationConfidence)
	}
	if len(report.Warnings) == 0 {
		t.Fatal("expected warnings for unverified auth readiness")
	}
}

func TestGrokOnlyUnknownAuthReadyWithWarning(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeGrok(t, binDir)
	writeExecutorsTOML(t, home, `delegated_executor = "grok"

[[executors]]
name = "grok"
command = "grok"
`)

	report := mustRunDoctor(t, home, binDir)
	if !report.Ready {
		t.Fatalf("expected ready: %+v", report)
	}
	if report.Executors.Status != StatusWarn || report.Executors.VerifiedCount != 0 || report.Executors.UsableCount < 1 {
		t.Fatalf("executors = %+v", report.Executors)
	}
	grok := findExecutor(report, "grok")
	if grok == nil || grok.Auth != AuthUnknown || !grok.Usable || grok.VerifiedAuthenticated {
		t.Fatalf("grok = %+v", grok)
	}
	if report.Executors.RecommendedHost != "grok" || report.Executors.RecommendationConfidence != ConfidenceUnverified {
		t.Fatalf("host=%s confidence=%s", report.Executors.RecommendedHost, report.Executors.RecommendationConfidence)
	}
	var human bytes.Buffer
	_ = WriteHuman(&human, report)
	if !strings.Contains(human.String(), "READY (with warnings)") {
		t.Fatalf("human output:\n%s", human.String())
	}
	if !strings.Contains(human.String(), "unverified") {
		t.Fatalf("human output missing confidence:\n%s", human.String())
	}
}

func TestCustomExecutorUnknownAuthReadyWithWarning(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCustom(t, binDir, "mycli")
	writeExecutorsTOML(t, home, `delegated_executor = "mycli"

[[executors]]
name = "mycli"
command = "mycli"
`)

	report := mustRunDoctor(t, home, binDir)
	if !report.Ready {
		t.Fatalf("expected ready: %+v", report)
	}
	item := findExecutor(report, "mycli")
	if item == nil || item.Provider != "custom" || item.Auth != AuthUnknown || !item.Usable {
		t.Fatalf("mycli = %+v", item)
	}
	if report.Executors.Status != StatusWarn || report.Executors.RecommendationConfidence != ConfidenceUnverified {
		t.Fatalf("executors = %+v", report.Executors)
	}
	if report.Executors.RecommendedHost != "mycli" {
		t.Fatalf("recommended host = %q", report.Executors.RecommendedHost)
	}
}

func TestUnauthenticatedOnlyNotReady(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Not logged in")
	writeExecutorsTOML(t, home, `
[[executors]]
name = "codex"
command = "codex"
`)

	report := mustRunDoctor(t, home, binDir)
	if report.Ready {
		t.Fatal("expected not ready when only unauthenticated executors exist")
	}
	if report.Executors.Status != StatusFail || report.Executors.Remediation != RemediationAuthenticateExec {
		t.Fatalf("executors = %+v", report.Executors)
	}
	if report.Executors.UsableCount != 0 || report.Executors.RecommendationConfidence != ConfidenceNone {
		t.Fatalf("executors = %+v", report.Executors)
	}
	codex := findExecutor(report, "codex")
	if codex == nil || codex.Auth != AuthUnauthenticated || codex.Usable || codex.Runnable {
		t.Fatalf("codex = %+v", codex)
	}
}

func TestAuthenticatedPreferredOverUnknown(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeFakeGrok(t, binDir)
	writeExecutorsTOML(t, home, `delegated_executor = "grok"

[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "grok"
command = "grok"
`)

	report := mustRunDoctor(t, home, binDir)
	if !report.Ready {
		t.Fatalf("expected ready: %+v", report)
	}
	if report.Executors.Status != StatusOK || report.Executors.VerifiedCount < 1 {
		t.Fatalf("executors = %+v", report.Executors)
	}
	if report.Executors.RecommendedHost != "codex" {
		t.Fatalf("recommended host = %q, want codex over grok", report.Executors.RecommendedHost)
	}
	// Even though delegated_executor=grok, verified auth wins when selecting recommendations.
	if report.Executors.RecommendedDelegated != "codex" {
		t.Fatalf("recommended delegated = %q, want verified codex", report.Executors.RecommendedDelegated)
	}
	if report.Executors.RecommendationConfidence != ConfidenceVerified {
		t.Fatalf("confidence = %s", report.Executors.RecommendationConfidence)
	}
	grok := findExecutor(report, "grok")
	if grok == nil || !grok.Usable || grok.VerifiedAuthenticated {
		t.Fatalf("grok should remain usable unknown candidate: %+v", grok)
	}
}

func TestCodexAuthOutputNeverLeaksIntoReport(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using an API key - sk-RzC84SECRETVALUE")
	writeExecutorsTOML(t, home, `
[[executors]]
name = "codex"
command = "codex"
`)

	report, err := Run(Options{
		Home:          home,
		StateDir:      filepath.Join(home, ".zen"),
		ExecutorsPath: filepath.Join(home, ".zen", "executors.toml"),
		Addr:          "127.0.0.1:0",
		PathEnv:       binDir,
		Listen:        func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		HTTPGet:       func(ctx context.Context, url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(raw), "sk-") || strings.Contains(string(raw), "SECRET") {
		t.Fatalf("secret leaked into report JSON: %s", raw)
	}
	var human bytes.Buffer
	_ = WriteHuman(&human, report)
	if strings.Contains(human.String(), "sk-") || strings.Contains(human.String(), "SECRET") {
		t.Fatalf("secret leaked into human output: %s", human.String())
	}
}

func TestDetectsAlreadyRunningZen(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeExecutorsTOML(t, home, `
[[executors]]
name = "codex"
command = "codex"
`)

	report, err := Run(Options{
		Home:          home,
		StateDir:      filepath.Join(home, ".zen"),
		ExecutorsPath: filepath.Join(home, ".zen", "executors.toml"),
		Addr:          "127.0.0.1:19876",
		PathEnv:       binDir,
		Listen: func(network, address string) (io.Closer, error) {
			t.Fatal("Listen should not be called when Zen health succeeds")
			return nil, nil
		},
		HTTPGet: func(ctx context.Context, url string) (int, []byte, error) {
			return 200, []byte(`{"status":"ok","daemon_id":"abc123"}`), nil
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Listen.ZenRunning || report.Listen.DaemonID != "abc123" || report.Listen.Status != StatusOK {
		t.Fatalf("listen = %+v", report.Listen)
	}
	if !report.Ready {
		t.Fatalf("expected ready when Zen already running: %+v", report)
	}
}

func TestPortInUseByNonZen(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	writeFakeCodex(t, binDir, "Logged in using ChatGPT")
	writeExecutorsTOML(t, home, `
[[executors]]
name = "codex"
command = "codex"
`)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer ln.Close()

	report, err := Run(Options{
		Home:          home,
		StateDir:      filepath.Join(home, ".zen"),
		ExecutorsPath: filepath.Join(home, ".zen", "executors.toml"),
		Addr:          ln.Addr().String(),
		PathEnv:       binDir,
		HTTPGet:       func(ctx context.Context, url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Listen.Status != StatusFail || report.Listen.Remediation != RemediationPortInUse {
		t.Fatalf("listen = %+v", report.Listen)
	}
	if report.Ready {
		t.Fatal("expected not ready when port is blocked")
	}
}

func TestParseAuthHelpers(t *testing.T) {
	if got := parseCodexAuth([]byte("Logged in using an API key - sk-SECRET"), nil); got != AuthAuthenticated {
		t.Fatalf("codex auth = %s", got)
	}
	if got := parseCodexAuth([]byte("Not logged in"), nil); got != AuthUnauthenticated {
		t.Fatalf("codex auth = %s", got)
	}
	if got := parseCodexAuth(nil, errors.New("exit 1")); got != AuthUnknown {
		t.Fatalf("codex failed probe = %s", got)
	}
	if got := parseClaudeAuth([]byte(`{"loggedIn":true}`), nil); got != AuthAuthenticated {
		t.Fatalf("claude auth = %s", got)
	}
	if got := parseClaudeAuth([]byte(`{"loggedIn":false}`), nil); got != AuthUnauthenticated {
		t.Fatalf("claude auth = %s", got)
	}
	if got := parseClaudeAuth([]byte(`{}`), nil); got != AuthUnknown {
		t.Fatalf("claude empty object = %s, want unknown", got)
	}
	if got := parseClaudeAuth(nil, errors.New("exit 1")); got != AuthUnknown {
		t.Fatalf("claude failed probe = %s", got)
	}
	if got := parseCursorAuth([]byte(`{"status":"authenticated","isAuthenticated":true}`), nil); got != AuthAuthenticated {
		t.Fatalf("cursor auth = %s", got)
	}
	if got := parseCursorAuth([]byte(`{"status":"unauthenticated","isAuthenticated":false}`), nil); got != AuthUnauthenticated {
		t.Fatalf("cursor auth = %s", got)
	}
	if got := parseCursorAuth([]byte(`{}`), nil); got != AuthUnknown {
		t.Fatalf("cursor empty object = %s, want unknown", got)
	}
	if got := parseCursorAuth([]byte(`{"isAuthenticated":false}`), nil); got != AuthUnauthenticated {
		t.Fatalf("cursor explicit false = %s", got)
	}
	if got := parseCursorAuth(nil, errors.New("unsupported")); got != AuthUnknown {
		t.Fatalf("cursor failed probe = %s", got)
	}
}

func TestFailedOfficialAuthCommandIsUnknown(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	writeFakeTmux(t, binDir)
	// Codex auth command fails / returns empty — must not become unauthenticated.
	writeExec(t, filepath.Join(binDir, "codex"), `#!/bin/sh
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then echo "codex-cli 0.0.1"; exit 0; fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then exit 2; fi
exit 1
`)
	writeExecutorsTOML(t, home, `
[[executors]]
name = "codex"
command = "codex"
`)

	report := mustRunDoctor(t, home, binDir)
	codex := findExecutor(report, "codex")
	if codex == nil || codex.Auth != AuthUnknown || !codex.Usable {
		t.Fatalf("codex = %+v", codex)
	}
	if !report.Ready || report.Executors.Status != StatusWarn {
		t.Fatalf("expected ready with warning, got ready=%v executors=%+v", report.Ready, report.Executors)
	}
}

func TestPlatformSupportedOnCurrentOS(t *testing.T) {
	report, err := Run(Options{
		Home:     t.TempDir(),
		StateDir: t.TempDir(),
		PathEnv:  t.TempDir(),
		Listen:   func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		HTTPGet:  func(ctx context.Context, url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	switch runtime.GOOS {
	case "linux", "darwin":
		if !report.Platform.Supported || report.Platform.Status != StatusOK {
			t.Fatalf("platform = %+v", report.Platform)
		}
	default:
		if report.Platform.Supported || report.Platform.Status != StatusFail {
			t.Fatalf("platform = %+v", report.Platform)
		}
	}
}

type nopCloser struct{}

func (nopCloser) Close() error { return nil }

func mustRunDoctor(t *testing.T, home, binDir string) Report {
	t.Helper()
	report, err := Run(Options{
		Home:          home,
		StateDir:      filepath.Join(home, ".zen"),
		ExecutorsPath: filepath.Join(home, ".zen", "executors.toml"),
		Addr:          "127.0.0.1:0",
		PathEnv:       binDir,
		Listen:        func(network, address string) (io.Closer, error) { return nopCloser{}, nil },
		HTTPGet:       func(ctx context.Context, url string) (int, []byte, error) { return 0, nil, errors.New("refused") },
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return report
}

func writeExecutorsTOML(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, ".zen")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "executors.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func writeFakeTmux(t *testing.T, binDir string) {
	t.Helper()
	script := `#!/bin/sh
set -e
case "$1" in
  -V) echo "tmux 3.4"; exit 0 ;;
  new-session)
    # consume args; pretend success
    exit 0
    ;;
  kill-session) exit 0 ;;
  *) echo "unexpected: $*" >&2; exit 1 ;;
esac
`
	writeExec(t, filepath.Join(binDir, "tmux"), script)
}

func writeFakeCodex(t *testing.T, binDir, loginStatus string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
set -e
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then
  echo "codex-cli 0.0.1-test"
  exit 0
fi
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  printf '%%s\n' %q
  exit 0
fi
echo "unexpected: $*" >&2
exit 1
`, loginStatus)
	writeExec(t, filepath.Join(binDir, "codex"), script)
}

func writeFakeGrok(t *testing.T, binDir string) {
	t.Helper()
	script := `#!/bin/sh
set -e
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then
  echo "grok 0.0.1-test"
  exit 0
fi
echo "unexpected: $*" >&2
exit 1
`
	writeExec(t, filepath.Join(binDir, "grok"), script)
}

func writeFakeCustom(t *testing.T, binDir, name string) {
	t.Helper()
	script := `#!/bin/sh
set -e
if [ "$1" = "--version" ] || [ "$1" = "-v" ]; then
  echo "mycli 1.0.0-test"
  exit 0
fi
echo "unexpected: $*" >&2
exit 1
`
	writeExec(t, filepath.Join(binDir, name), script)
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

func findExecutor(report Report, id string) *ExecutorCheck {
	for i := range report.Executors.Items {
		if report.Executors.Items[i].ID == id {
			return &report.Executors.Items[i]
		}
	}
	return nil
}
