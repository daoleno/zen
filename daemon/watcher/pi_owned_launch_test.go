package watcher

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// TestMergeAgentCommandOwnershipKeepsOnlyOwnedPiLaunch covers the launch
// ownership merge that prevents the watcher poll from destroying the injected
// absolute --session path. Only a Pi launch command that already carries an
// owned session path is preserved, and only while the observed process is
// still Pi; every other command keeps the detected identity exactly as
// before.
func TestMergeAgentCommandOwnershipKeepsOnlyOwnedPiLaunch(t *testing.T) {
	owned := filepath.Join(t.TempDir(), "owned.jsonl")
	cases := []struct {
		name          string
		previous      string
		detected      string
		want          string
		wantOwnedPath string
	}{
		{
			name:          "env-wrapped owned pi launch survives refresh",
			previous:      "env PATH=/x pi --session " + owned,
			detected:      "pi",
			want:          "pi --session " + owned,
			wantOwnedPath: owned,
		},
		{
			name:          "plain owned pi launch survives refresh",
			previous:      "pi --session " + owned,
			detected:      "pi",
			want:          "pi --session " + owned,
			wantOwnedPath: owned,
		},
		{
			name:          "owned session-dir survives refresh",
			previous:      "pi --session-dir " + filepath.Dir(owned),
			detected:      "pi",
			want:          "pi --session-dir " + filepath.Dir(owned),
			wantOwnedPath: filepath.Dir(owned),
		},
		{
			name:          "equals-form owned session survives refresh",
			previous:      "pi --session=" + owned,
			detected:      "pi",
			want:          "pi --session " + owned,
			wantOwnedPath: owned,
		},
		{
			name:          "unowned pi keeps detected identity",
			previous:      "pi",
			detected:      "pi",
			want:          "pi",
			wantOwnedPath: "",
		},
		{
			name:          "relative session is not owned and fails closed",
			previous:      "pi --session relative.jsonl",
			detected:      "pi",
			want:          "pi",
			wantOwnedPath: "",
		},
		{
			name:          "provider switch clears stale ownership",
			previous:      "pi --session " + owned,
			detected:      "codex",
			want:          "codex",
			wantOwnedPath: "",
		},
		{
			name:          "non-pi providers keep detected identity",
			previous:      "codex resume abc",
			detected:      "codex resume abc",
			want:          "codex resume abc",
			wantOwnedPath: "",
		},
		{
			name:          "opencode command is never pi-merged",
			previous:      "opencode -s ses_abc --auto",
			detected:      "opencode",
			want:          "opencode",
			wantOwnedPath: "",
		},
		{
			name:          "empty previous uses detected",
			previous:      "",
			detected:      "pi",
			want:          "pi",
			wantOwnedPath: "",
		},
		{
			name:          "empty detected stays empty",
			previous:      "pi --session " + owned,
			detected:      "",
			want:          "",
			wantOwnedPath: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeAgentCommandOwnership(tc.previous, tc.detected)
			if got != tc.want {
				t.Fatalf("mergeAgentCommandOwnership(%q, %q) = %q, want %q", tc.previous, tc.detected, got, tc.want)
			}
			if gotPath := piOwnedLaunchPath(got); gotPath != tc.wantOwnedPath {
				t.Fatalf("piOwnedLaunchPath(%q) = %q, want %q", got, gotPath, tc.wantOwnedPath)
			}
		})
	}
}

// TestPollPreservesOwnedPiLaunchCommandAcrossRefresh reproduces the live
// defect end to end at the watcher boundary: the session is created with the
// injected owned --session launch command, and repeated polls must keep that
// exact launch command instead of replacing it with the bare process identity
// "pi". A provider switch then clears stale ownership.
func TestPollPreservesOwnedPiLaunchCommandAcrossRefresh(t *testing.T) {
	owned := filepath.Join(t.TempDir(), "owned.jsonl")
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC),
	})
	launchCommand := "env PATH=/x pi --session " + owned
	w.registerCreatedSession("brain-agent-pi:@1", "/repo/zen", CreateSessionOptions{
		Command:   launchCommand,
		Name:      "Pi task",
		Delegated: true,
	}, time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC))
	drainWatcherEvents(w)

	windows := []tmuxWindow{
		{target: "brain-agent-pi:@1", name: "pi", cwd: "/repo/zen", command: "pi", panePID: 444, delegated: true},
	}
	processes := map[int]processInfo{
		444: {
			pid: 444, ppid: 1, pgid: 444, tpgid: 444,
			startedAt: time.Date(2026, 8, 7, 9, 0, 5, 0, time.UTC),
			comm:      "pi",
			args:      "pi",
		},
	}
	restore := installFakePollSeams(windows, map[string]string{
		"brain-agent-pi:@1": "pi v0.73.1\nworking\n",
	}, processes)
	defer restore()

	for poll := 1; poll <= 2; poll++ {
		w.poll()
		drainWatcherEvents(w)
		agent := agentByID(w.Agents(), "brain-agent-pi:@1")
		if agent == nil {
			t.Fatalf("poll %d: agent missing", poll)
		}
		wantCommand := "pi --session " + owned
		if agent.Command != wantCommand {
			t.Fatalf("poll %d: owned launch command lost: %q, want %q", poll, agent.Command, wantCommand)
		}
		if piOwnedLaunchPath(agent.Command) != owned {
			t.Fatalf("poll %d: owned path missing: %q", poll, agent.Command)
		}
		if agent.ProcessID != 444 {
			t.Fatalf("poll %d: process id = %d, want 444", poll, agent.ProcessID)
		}
	}

	// A provider switch clears the stale Pi ownership.
	restore()
	restore = installFakePollSeams([]tmuxWindow{
		{target: "brain-agent-pi:@1", name: "codex", cwd: "/repo/zen", command: "codex", panePID: 555, delegated: true},
	}, map[string]string{
		"brain-agent-pi:@1": "Codex\n",
	}, map[int]processInfo{
		555: {
			pid: 555, ppid: 1, pgid: 555, tpgid: 555,
			startedAt: time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
			comm:      "codex",
			args:      "codex",
		},
	})
	defer restore()
	w.poll()
	drainWatcherEvents(w)
	agent := agentByID(w.Agents(), "brain-agent-pi:@1")
	if agent == nil {
		t.Fatal("agent missing after provider switch")
	}
	if piOwnedLaunchPath(agent.Command) != "" || strings.Contains(agent.Command, "pi --session") {
		t.Fatalf("stale Pi ownership survived provider switch: %q", agent.Command)
	}
}

// TestPollPiSiblingAgentsNeverShareOwnedPaths pins the fail-closed isolation
// rule at the watcher boundary: two same-CWD delegated Pi sessions launched
// with different owned paths keep their own exact launch commands across
// polls, so the reader can never cross-bind them.
func TestPollPiSiblingAgentsNeverShareOwnedPaths(t *testing.T) {
	dir := t.TempDir()
	ownedA := filepath.Join(dir, "a.jsonl")
	ownedB := filepath.Join(dir, "b.jsonl")
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
	})
	w.registerCreatedSession("brain-agent-pi-a:@1", "/repo/zen", CreateSessionOptions{
		Command:   "pi --session " + ownedA,
		Name:      "Pi A",
		Delegated: true,
	}, time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC))
	w.registerCreatedSession("brain-agent-pi-b:@2", "/repo/zen", CreateSessionOptions{
		Command:   "pi --session " + ownedB,
		Name:      "Pi B",
		Delegated: true,
	}, time.Date(2026, 8, 7, 9, 0, 1, 0, time.UTC))
	drainWatcherEvents(w)

	restore := installFakePollSeams([]tmuxWindow{
		{target: "brain-agent-pi-a:@1", name: "pi", cwd: "/repo/zen", command: "pi", panePID: 610, delegated: true},
		{target: "brain-agent-pi-b:@2", name: "pi", cwd: "/repo/zen", command: "pi", panePID: 620, delegated: true},
	}, map[string]string{
		"brain-agent-pi-a:@1": "pi\nworking A\n",
		"brain-agent-pi-b:@2": "pi\nworking B\n",
	}, map[int]processInfo{
		610: {pid: 610, ppid: 1, pgid: 610, tpgid: 610, startedAt: time.Date(2026, 8, 7, 9, 0, 5, 0, time.UTC), comm: "pi", args: "pi"},
		620: {pid: 620, ppid: 1, pgid: 620, tpgid: 620, startedAt: time.Date(2026, 8, 7, 9, 0, 6, 0, time.UTC), comm: "pi", args: "pi"},
	})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	agentA := agentByID(w.Agents(), "brain-agent-pi-a:@1")
	agentB := agentByID(w.Agents(), "brain-agent-pi-b:@2")
	if agentA == nil || agentB == nil {
		t.Fatalf("siblings missing: a=%v b=%v", agentA, agentB)
	}
	if got := piOwnedLaunchPath(agentA.Command); got != ownedA {
		t.Fatalf("sibling A owned path = %q, want %q (command %q)", got, ownedA, agentA.Command)
	}
	if got := piOwnedLaunchPath(agentB.Command); got != ownedB {
		t.Fatalf("sibling B owned path = %q, want %q (command %q)", got, ownedB, agentB.Command)
	}
	if agentA.Command == agentB.Command {
		t.Fatalf("sibling launch commands cross-bound: %q", agentA.Command)
	}
}

// TestPollDiscoveredPiWithoutLaunchCommandKeepsDetectedIdentity covers the
// non-owned path: a Pi window rediscovered from the process table alone (no
// launch record) keeps the bare detected identity, exactly as before the
// ownership merge.
func TestPollDiscoveredPiWithoutLaunchCommandKeepsDetectedIdentity(t *testing.T) {
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
	})
	restore := installFakePollSeams([]tmuxWindow{
		{target: "main:@0", name: "pi", cwd: "/repo/zen", command: "pi", panePID: 710},
	}, map[string]string{
		"main:@0": "pi\n",
	}, map[int]processInfo{
		710: {pid: 710, ppid: 1, pgid: 710, tpgid: 710, startedAt: time.Date(2026, 8, 7, 9, 0, 5, 0, time.UTC), comm: "pi", args: "pi"},
	})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	agent := agentByID(w.Agents(), "main:@0")
	if agent == nil || agent.Command != "pi" {
		t.Fatalf("rediscovered pi agent = %#v, want command %q", agent, "pi")
	}
	if piOwnedLaunchPath(agent.Command) != "" {
		t.Fatalf("unowned pi gained an owned path: %q", agent.Command)
	}
	if agent.State != classifier.StateUnknown && agent.State != classifier.StateRunning {
		t.Fatalf("rediscovered pi state = %s", agent.State)
	}
}
