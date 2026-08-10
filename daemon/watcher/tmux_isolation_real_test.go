package watcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealTmuxProviderPlainTmuxCannotReachSharedHostServer(t *testing.T) {
	h := newSharedTmuxHarness(t, false)
	ambientTarget := createHarnessPane(t, h.selected, "ambient-user", "exec /bin/sh")
	durableCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	// Provider PATH bypasses the test firewall so this proves the production
	// TMUX/TMUX_TMPDIR boundary itself. The watcher still reaches the host via
	// the firewall's explicit, test-owned selected socket.
	providerPath := filepath.Dir(h.realTmux) + string(os.PathListSeparator) + "/usr/bin:/bin"
	target, err := h.w.CreateSession("", CreateSessionOptions{
		Name:        "contained-provider",
		Cwd:         durableCwd,
		Command:     "tmux kill-server >/dev/null 2>&1; printf 'PROVIDER_TMUX_CONTAINED\\n'; exec /bin/sh",
		Detached:    true,
		Delegated:   true,
		ProgressEnv: true,
		Env:         map[string]string{"PATH": providerPath},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForHarness(t, "provider containment marker", func() bool {
		return strings.Contains(captureHarnessPane(t, h.selected, target), "PROVIDER_TMUX_CONTAINED")
	})

	if err := exec.Command(h.realTmux, "-S", h.physical, "has-session", "-t", target).Run(); err != nil {
		t.Fatalf("plain provider tmux killed its shared host Session: %v", err)
	}
	if err := exec.Command(h.realTmux, "-S", h.physical, "has-session", "-t", ambientTarget).Run(); err != nil {
		t.Fatalf("plain provider tmux killed ambient user Session: %v", err)
	}

	// The interactive shell inherits the post-bootstrap environment. Prove TMUX
	// stayed absent and plain tmux still resolves only under private scratch.
	envProof := filepath.Join(h.root, "provider-env.txt")
	probe := `printf '%s|%s' "${TMUX-unset}" "$TMUX_TMPDIR" > ` + shellQuote(envProof) + `; tmux kill-server >/dev/null 2>&1; echo SECOND_KILL_CONTAINED`
	if out, err := tmuxHarnessCommand(h.selected, "send-keys", "-t", target, probe, "Enter").CombinedOutput(); err != nil {
		t.Fatalf("send containment probe: %v: %s", err, out)
	}
	waitForHarness(t, "second contained kill", func() bool {
		return strings.Contains(captureHarnessPane(t, h.selected, target), "SECOND_KILL_CONTAINED")
	})
	envRaw, err := os.ReadFile(envProof)
	if err != nil || string(envRaw) != "unset|"+h.scratch {
		t.Fatalf("provider tmux environment = %q err=%v, want private scratch %q", envRaw, err, h.scratch)
	}
	for _, liveTarget := range []string{target, ambientTarget} {
		if err := exec.Command(h.realTmux, "-S", h.physical, "has-session", "-t", liveTarget).Run(); err != nil {
			t.Fatalf("second plain tmux command reached shared target %s: %v", liveTarget, err)
		}
	}
}
