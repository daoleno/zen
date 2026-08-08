package watcher

import (
	"encoding/base64"
	"os"
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
			// shellQuoteForLaunch output: a spaced path is single-quote wrapped
			// exactly as EnsurePiSessionLaunchCommand emits it. The watcher must
			// strip the quotes for ownership detection and re-emit them in the
			// merged command so work.PiOwnedSessionPath recovers the same path.
			name:          "quoted spaced owned path survives refresh",
			previous:      `pi --session '/tmp/My Zen/owned file.jsonl'`,
			detected:      "pi",
			want:          `pi --session '/tmp/My Zen/owned file.jsonl'`,
			wantOwnedPath: "/tmp/My Zen/owned file.jsonl",
		},
		{
			name:          "quoted dollar metachar owned path survives refresh",
			previous:      `pi --session '/tmp/co$t/owned file.jsonl'`,
			detected:      "pi",
			want:          `pi --session '/tmp/co$t/owned file.jsonl'`,
			wantOwnedPath: "/tmp/co$t/owned file.jsonl",
		},
		{
			name:          "quoted backtick metachar owned path survives refresh",
			previous:      "pi --session '/tmp/back`tick/owned file.jsonl'",
			detected:      "pi",
			want:          "pi --session '/tmp/back`tick/owned file.jsonl'",
			wantOwnedPath: "/tmp/back`tick/owned file.jsonl",
		},
		{
			name:          "quoted double-quote metachar owned path survives refresh",
			previous:      `pi --session '/tmp/qu"ote/owned file.jsonl'`,
			detected:      "pi",
			want:          `pi --session '/tmp/qu"ote/owned file.jsonl'`,
			wantOwnedPath: "/tmp/qu\"ote/owned file.jsonl",
		},
		{
			name:          "quoted backslash metachar owned path survives refresh",
			previous:      `pi --session '/tmp/back\slash/owned file.jsonl'`,
			detected:      "pi",
			want:          `pi --session '/tmp/back\slash/owned file.jsonl'`,
			wantOwnedPath: `/tmp/back\slash/owned file.jsonl`,
		},
		{
			// shellQuoteForLaunch escapes an embedded single quote as '\'';
			// splitZenLaunchFields cannot keep such a value in one wrapped token
			// (the escape's first quote closes the span), so the watcher and the
			// work parser both fail closed here: ownership is not claimed and the
			// detected identity stays bare "pi" — never a wrong binding and never
			// an unstable command.
			name:          "quoted embedded apostrophe fails closed without ownership",
			previous:      `pi --session '/tmp/a'\''b/owned file.jsonl'`,
			detected:      "pi",
			want:          "pi",
			wantOwnedPath: "",
		},
		{
			name:          "quoted equals-form owned session survives refresh",
			previous:      `pi --session='/tmp/My Zen/owned file.jsonl'`,
			detected:      "pi",
			want:          `pi --session '/tmp/My Zen/owned file.jsonl'`,
			wantOwnedPath: "/tmp/My Zen/owned file.jsonl",
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
	w.registerCreatedSession("", "brain-agent-pi:@1", "/repo/zen", CreateSessionOptions{
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

// TestPollPreservesQuotedOwnedPiLaunchCommandAcrossRefresh reproduces the
// reviewed P2 parser divergence: a Zen-owned --session path that requires
// shell quoting (space and metacharacters, exactly as EnsurePiSessionLaunchCommand
// emits via shellQuoteForLaunch) must keep the owned binding across polls
// instead of degrading to bare "pi".
func TestPollPreservesQuotedOwnedPiLaunchCommandAcrossRefresh(t *testing.T) {
	spaced := filepath.Join(t.TempDir(), "My Zen", "owned file.jsonl")
	quoted := shellQuoteForLaunch(spaced)
	launchCommand := "env PATH=/x pi --session " + quoted
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
	})
	w.registerCreatedSession("", "brain-agent-pi-quoted:@1", "/repo/zen", CreateSessionOptions{
		Command:   launchCommand,
		Name:      "Pi quoted task",
		Delegated: true,
	}, time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC))
	drainWatcherEvents(w)

	restore := installFakePollSeams([]tmuxWindow{
		{target: "brain-agent-pi-quoted:@1", name: "pi", cwd: "/repo/zen", command: "pi", panePID: 448, delegated: true},
	}, map[string]string{
		"brain-agent-pi-quoted:@1": "pi v0.73.1\nworking\n",
	}, map[int]processInfo{
		448: {pid: 448, ppid: 1, pgid: 448, tpgid: 448, startedAt: time.Date(2026, 8, 7, 9, 0, 5, 0, time.UTC), comm: "pi", args: "pi"},
	})
	defer restore()

	wantCommand := "pi --session " + quoted
	for poll := 1; poll <= 2; poll++ {
		w.poll()
		drainWatcherEvents(w)
		agent := agentByID(w.Agents(), "brain-agent-pi-quoted:@1")
		if agent == nil {
			t.Fatalf("poll %d: agent missing", poll)
		}
		if agent.Command != wantCommand {
			t.Fatalf("poll %d: quoted owned launch command degraded: %q, want %q", poll, agent.Command, wantCommand)
		}
		if got := piOwnedLaunchPath(agent.Command); got != spaced {
			t.Fatalf("poll %d: owned path = %q, want %q (command %q)", poll, got, spaced, agent.Command)
		}
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
	w.registerCreatedSession("", "brain-agent-pi-a:@1", "/repo/zen", CreateSessionOptions{
		Command:   "pi --session " + ownedA,
		Name:      "Pi A",
		Delegated: true,
	}, time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC))
	w.registerCreatedSession("", "brain-agent-pi-b:@2", "/repo/zen", CreateSessionOptions{
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

// TestPollRediscoveredPiWindowRestoresOwnedLaunchCommandFromTmuxOption
// reproduces the daemon restart path at window re-discovery: the tmux window
// survives with the durable Pi ownership binding recorded at session create
// (@zen_agent_pi_session), while the pi process rewrites its argv to bare
// "pi". The fresh watcher (no in-memory launch record) must restore the owned
// --session path from the binding and keep it across later polls, so a
// reopen/reconnect subscription binds the exact durable transcript instead of
// degrading to transcript_not_found.
func TestPollRediscoveredPiWindowRestoresOwnedLaunchCommandFromTmuxOption(t *testing.T) {
	owned := filepath.Join(t.TempDir(), "My Zen", "owned file.jsonl")
	binding := EncodePiSessionBinding("--session", owned)
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
		time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC),
	})
	restore := installFakePollSeams([]tmuxWindow{
		{
			target: "brain-agent-pi-restart:@1", name: "pi", cwd: "/repo/zen",
			command: "pi", panePID: 900,
			piSessionBinding: binding,
		},
	}, map[string]string{
		"brain-agent-pi-restart:@1": "pi v0.73.1\nworking\n",
	}, map[int]processInfo{
		900: {pid: 900, ppid: 1, pgid: 900, tpgid: 900, startedAt: time.Date(2026, 8, 7, 9, 0, 5, 0, time.UTC), comm: "pi", args: "pi"},
	})
	defer restore()

	for poll := 1; poll <= 2; poll++ {
		w.poll()
		drainWatcherEvents(w)
		agent := agentByID(w.Agents(), "brain-agent-pi-restart:@1")
		if agent == nil {
			t.Fatalf("poll %d: rediscovered agent missing", poll)
		}
		wantCommand := "pi --session " + shellQuoteForLaunch(owned)
		if agent.Command != wantCommand {
			t.Fatalf("poll %d: owned launch command not restored: %q, want %q", poll, agent.Command, wantCommand)
		}
		if got := piOwnedLaunchPath(agent.Command); got != owned {
			t.Fatalf("poll %d: owned path = %q, want %q (command %q)", poll, got, owned, agent.Command)
		}
	}
}

// TestPiSessionBindingRoundTripsHostilePaths pins the delimiter-safe encoding
// contract: owned paths containing tabs, newlines, double quotes, backslashes,
// and shell metacharacters must round-trip exactly through the encoded tmux
// option value, and the encoded value itself must be a single delimiter-safe
// token that cannot corrupt the tab-separated list-windows projection. The
// canonical reconstructed command must also survive the watcher flag parser.
// An embedded apostrophe still round-trips exactly through the encoding; the
// command parser fails closed on it (documented shell-quoting contract),
// never binding a wrong transcript.
func TestPiSessionBindingRoundTripsHostilePaths(t *testing.T) {
	path := "/repo/My\tZen\nowne\"d file\\x$y.jsonl"
	binding := EncodePiSessionBinding("--session", path)
	if binding == "" {
		t.Fatal("valid owned path must encode")
	}
	if strings.ContainsAny(binding, " \t\n\r'\"") {
		t.Fatalf("encoded binding is not delimiter-safe: %q", binding)
	}
	flag, decoded, ok := DecodePiSessionBinding(binding)
	if !ok || flag != "--session" || decoded != path {
		t.Fatalf("round-trip = %q %q %v, want --session %q", flag, decoded, ok, path)
	}
	// The canonical reconstructed command must also survive the flag parser
	// that binds the transcript after rediscovery.
	command := "pi --session " + shellQuoteForLaunch(path)
	if got := piOwnedLaunchPath(command); got != path {
		t.Fatalf("piOwnedLaunchPath(%q) = %q, want %q", command, got, path)
	}
	// --session-dir binding round-trips too.
	dirBinding := EncodePiSessionBinding("--session-dir", path)
	flag, decoded, ok = DecodePiSessionBinding(dirBinding)
	if !ok || flag != "--session-dir" || decoded != path {
		t.Fatalf("session-dir round-trip = %q %q %v, want --session-dir %q", flag, decoded, ok, path)
	}
	// Embedded apostrophe: exact through the encoding, fail-closed through
	// the command parser (no wrong transcript can ever bind).
	apostrophePath := "/repo/o'wned file.jsonl"
	apostropheBinding := EncodePiSessionBinding("--session", apostrophePath)
	if apostropheBinding == "" {
		t.Fatal("apostrophe path must encode")
	}
	_, decoded, ok = DecodePiSessionBinding(apostropheBinding)
	if !ok || decoded != apostrophePath {
		t.Fatalf("apostrophe round-trip = %q %v, want %q", decoded, ok, apostrophePath)
	}
	if got := piOwnedLaunchPath("pi --session " + shellQuoteForLaunch(apostrophePath)); got != "" {
		t.Fatalf("apostrophe path must fail closed, got %q", got)
	}
}

// TestPiSessionBindingFailsClosed pins the decode validation: malformed,
// wrong-version, non-absolute, and unknown-flag bindings must never produce a
// transcript binding, and invalid flags or relative paths must never encode.
func TestPiSessionBindingFailsClosed(t *testing.T) {
	valid := EncodePiSessionBinding("--session", "/repo/owned.jsonl")
	if valid == "" {
		t.Fatal("valid binding must encode")
	}
	for name, value := range map[string]string{
		"empty":         "",
		"not base64url": "not a binding!!!",
		"garbage json":  base64.RawURLEncoding.EncodeToString([]byte("garbage")),
		"wrong version": base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"flag":"--session","path":"/repo/x.jsonl"}`)),
		"missing flag":  base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"path":"/repo/x.jsonl"}`)),
		"unknown flag":  base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"flag":"--resume","path":"/repo/x.jsonl"}`)),
		"relative path": base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"flag":"--session","path":"repo/x.jsonl"}`)),
		"empty path":    base64.RawURLEncoding.EncodeToString([]byte(`{"v":1,"flag":"--session","path":""}`)),
	} {
		if flag, path, ok := DecodePiSessionBinding(value); ok {
			t.Fatalf("%s: decoded %q %q, want fail-closed", name, flag, path)
		}
	}
	if got := EncodePiSessionBinding("--resume", "/repo/x.jsonl"); got != "" {
		t.Fatalf("unknown flag encoded: %q", got)
	}
	if got := EncodePiSessionBinding("--session", "relative/x.jsonl"); got != "" {
		t.Fatalf("relative path encoded: %q", got)
	}
}

// TestPollPiSessionBindingFailsClosedOnCorruptedOption pins the rediscovery
// fail-closed rule: a corrupted @zen_agent_pi_session value must not bind any
// transcript; the agent keeps only the detected process identity.
func TestPollPiSessionBindingFailsClosedOnCorruptedOption(t *testing.T) {
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
	})
	restore := installFakePollSeams([]tmuxWindow{
		{
			target: "brain-agent-pi-corrupt:@1", name: "pi", cwd: "/repo/zen",
			command: "pi", panePID: 910,
			piSessionBinding: "corrupted value with \t tab",
		},
	}, map[string]string{
		"brain-agent-pi-corrupt:@1": "pi\n",
	}, map[int]processInfo{
		910: {pid: 910, ppid: 1, pgid: 910, tpgid: 910, startedAt: time.Date(2026, 8, 7, 9, 0, 5, 0, time.UTC), comm: "pi", args: "pi"},
	})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	agent := agentByID(w.Agents(), "brain-agent-pi-corrupt:@1")
	if agent == nil {
		t.Fatal("agent missing")
	}
	if agent.Command != "pi" || piOwnedLaunchPath(agent.Command) != "" {
		t.Fatalf("corrupted binding must fail closed, command = %q", agent.Command)
	}
}

// TestPollPiSessionBindingClearedOnProviderSwitch pins the ownership-clearing
// rule after rediscovery: a durable Pi binding must not survive a provider
// switch observed in the process table, exactly like the in-memory merge.
func TestPollPiSessionBindingClearedOnProviderSwitch(t *testing.T) {
	w := New(time.Second)
	w.pollNow = fakePollClock([]time.Time{
		time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
	})
	restore := installFakePollSeams([]tmuxWindow{
		{
			target: "brain-agent-pi-switch:@1", name: "codex", cwd: "/repo/zen",
			command: "codex", panePID: 920,
			piSessionBinding: EncodePiSessionBinding("--session", "/repo/owned.jsonl"),
		},
	}, map[string]string{
		"brain-agent-pi-switch:@1": "Codex\n",
	}, map[int]processInfo{
		920: {pid: 920, ppid: 1, pgid: 920, tpgid: 920, startedAt: time.Date(2026, 8, 7, 9, 0, 5, 0, time.UTC), comm: "codex", args: "codex"},
	})
	defer restore()

	w.poll()
	drainWatcherEvents(w)
	agent := agentByID(w.Agents(), "brain-agent-pi-switch:@1")
	if agent == nil {
		t.Fatal("agent missing")
	}
	if agent.Command != "codex" || strings.Contains(agent.Command, "--session") {
		t.Fatalf("stale Pi binding survived provider switch: %q", agent.Command)
	}
}

// TestMarkCreatedSessionPersistsOnlyValidPiBinding pins the write-side
// contract: only a validated Pi launch with an owned absolute --session path
// writes a @zen_agent_pi_session option; non-Pi commands (even secret-bearing)
// and Pi commands without an owned binding write nothing, so the raw launch
// command never reaches tmux.
func TestMarkCreatedSessionPersistsOnlyValidPiBinding(t *testing.T) {
	binDir := t.TempDir()
	tmuxPath := filepath.Join(binDir, "tmux")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$TMUX_OPTION_LOG\"\n" +
		"exit 0\n"
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "options.log")
	t.Setenv("TMUX_OPTION_LOG", logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	readOptions := func() []string {
		data, err := os.ReadFile(logPath)
		if err != nil {
			return nil
		}
		return strings.Split(strings.TrimSpace(string(data)), "\n")
	}

	owned := "/repo/.zen/provider-sessions/pi/owned.jsonl"
	cases := []struct {
		name    string
		command string
		want    string // expected encoded binding in the option log, "" for none
	}{
		{
			name:    "pi owned session",
			command: "env PATH=/x pi --session " + owned,
			want:    EncodePiSessionBinding("--session", owned),
		},
		{
			name:    "pi owned session-dir",
			command: "pi --session-dir /repo/.zen/provider-sessions/pi",
			want:    EncodePiSessionBinding("--session-dir", "/repo/.zen/provider-sessions/pi"),
		},
		{
			name:    "pi without binding",
			command: "pi",
		},
		{
			name:    "pi resume",
			command: "pi --continue",
		},
		{
			name:    "non-pi secret-bearing command",
			command: "codex --api-key secret-token --session /repo/session.jsonl",
		},
		{
			name:    "node secret-bearing command",
			command: "env PATH=/x node --token=secret app.js",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.Remove(logPath); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if err := markCreatedSession("", "main:@9", CreateSessionOptions{Command: tc.command}); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, option := range readOptions() {
				if strings.Contains(option, "@zen_agent_pi_session") {
					found = true
					if tc.want == "" {
						t.Fatalf("unexpected binding option written: %s", option)
					}
					if !strings.Contains(option, tc.want) {
						t.Fatalf("binding option = %s, want value %s", option, tc.want)
					}
				}
			}
			if tc.want != "" && !found {
				t.Fatalf("expected @zen_agent_pi_session option for %q", tc.command)
			}
			for _, option := range readOptions() {
				if strings.Contains(option, tc.command) {
					t.Fatalf("raw launch command leaked into tmux options: %s", option)
				}
			}
		})
	}
}

// TestListTmuxWindowsParsesPiSessionBinding pins the wire format of the
// durable Pi binding: list-windows must carry the encoded binding as its own
// field so rediscovery can restore owned Pi bindings after a daemon restart.
func TestListTmuxWindowsParsesPiSessionBinding(t *testing.T) {
	binDir := t.TempDir()
	binding := EncodePiSessionBinding("--session", filepath.Join(t.TempDir(), "My Zen", "owned file.jsonl"))
	line := "main:@1\tpi\t/repo/zen\tpi\t900\t\t\t\t\t" + binding
	tmuxPath := filepath.Join(binDir, "tmux")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"list-windows\" ]; then\n" +
		"  cat <<'EOF'\n" +
		line + "\n" +
		"EOF\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 1\n"
	if err := os.WriteFile(tmuxPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	windows, err := listTmuxWindowsOn("")
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 1 {
		t.Fatalf("windows = %#v, want 1", windows)
	}
	win := windows[0]
	if win.target != "main:@1" || win.command != "pi" || win.panePID != 900 {
		t.Fatalf("parsed window = %#v", win)
	}
	if win.piSessionBinding != binding {
		t.Fatalf("pi session binding = %q, want %q", win.piSessionBinding, binding)
	}
}
