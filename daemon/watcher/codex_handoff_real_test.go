package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealCodexIsolatedHandoff(t *testing.T) {
	if os.Getenv("ZEN_TEST_CODEX_HANDOFF") != "1" {
		t.Skip("set ZEN_TEST_CODEX_HANDOFF=1 to send one short prompt to installed Codex")
	}
	originalHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	settings := os.Getenv("CODEX_HOME")
	if settings == "" {
		settings = filepath.Join(originalHome, ".codex")
	}
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.toml", "auth.json"} {
		data, err := os.ReadFile(filepath.Join(settings, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(codexHome, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"ZEN_AGENT_ID", "ZEN_AGENT_PROGRESS_CMD", "ZEN_WORKER_ID", "ZEN_WORKER_PROGRESS_CMD", "TMUX", "TMUX_PANE"} {
		t.Setenv(key, "")
	}
	t.Setenv("HOME", home)
	t.Setenv("ZDOTDIR", home)
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("ZEN_STATE_DIR", filepath.Join(home, ".zen"))
	falseCommand, err := exec.LookPath("false")
	if err != nil {
		t.Fatal(err)
	}
	h := newSharedTmuxHarness(t, false)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	command := "codex --dangerously-bypass-approvals-and-sandbox"
	target, err := h.w.CreateSession("", CreateSessionOptions{
		Name: "isolated-handoff-proof", Cwd: cwd, Command: command,
		Detached: true, Delegated: true, ProgressEnv: true,
		Env: map[string]string{"HOME": home, "ZDOTDIR": home, "CODEX_HOME": codexHome, "ZEN_STATE_DIR": filepath.Join(home, ".zen"), "ZEN_WORKER_PROGRESS_CMD": falseCommand},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.w.KillSession(target) })
	prompt := "Do not use tools, run commands, edit files, or delegate. Reply with only the concatenation of ZEN_HANDOFF_ and VERIFIED."
	if err := h.w.SendInputWhenReadyBudgeted(target, command, prompt+"\n", 30*time.Second); err != nil {
		t.Fatalf("handoff failed: %v; pane: %s", err, captureHarnessPane(t, h.selected, target))
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		pane := captureHarnessPane(t, h.selected, target)
		if strings.Contains(pane, "ZEN_HANDOFF_VERIFIED") {
			t.Logf("production CreateSession + readiness/input queue received Codex reply on %s: ZEN_HANDOFF_VERIFIED", target)
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no Codex reply: %s", captureHarnessPane(t, h.selected, target))
}
