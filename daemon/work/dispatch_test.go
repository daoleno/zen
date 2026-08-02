package work

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	spawnCalls     int
	spawnRoles     []string
	spawnCwds      []string
	spawnCommands  []string
	sendReadyIDs   []string
	sendReadyCalls []string
	abortCalls     []string
	spawnErr       error
	sendReadyErr   error
	abortErr       error
	newID          string
}

func (f *fakeRunner) Spawn(role, cwd, command string) (string, error) {
	f.spawnCalls++
	f.spawnRoles = append(f.spawnRoles, role)
	f.spawnCwds = append(f.spawnCwds, cwd)
	f.spawnCommands = append(f.spawnCommands, command)
	if f.spawnErr != nil {
		return "", f.spawnErr
	}
	return f.newID, nil
}

func (f *fakeRunner) SendWhenReady(sessionID, command, text string) error {
	f.sendReadyIDs = append(f.sendReadyIDs, sessionID)
	f.sendReadyCalls = append(f.sendReadyCalls, sessionID+"|"+command+"|"+text)
	return f.sendReadyErr
}

func (f *fakeRunner) Abort(sessionID string) error {
	f.abortCalls = append(f.abortCalls, sessionID)
	return f.abortErr
}

func TestLauncher_StartDedicatedNeverReusesIdleOrMentionedSession(t *testing.T) {
	run := &fakeRunner{newID: "claude-scheduled"}
	execs := NewExecutorConfig("claude", map[string]Executor{
		"claude": {Name: "claude", Command: "claude"},
		"codex":  {Name: "codex", Command: "codex"},
	})
	item, err := ParseFile("/tmp/scheduled.md", []byte(`---
id: scheduled
created: 2026-07-17T00:00:00Z
---
# Scheduled action

Treat @codex#interactive-session as ordinary instruction text.
`), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	started, err := NewLauncher(run, execs).StartDedicated(item, "/p")
	if err != nil {
		t.Fatal(err)
	}
	if run.spawnCalls != 1 || started.Frontmatter.AgentSession != "claude-scheduled" {
		t.Fatalf("spawn calls = %d, session = %q", run.spawnCalls, started.Frontmatter.AgentSession)
	}
	if len(run.spawnRoles) != 1 || run.spawnRoles[0] != "claude" {
		t.Fatalf("spawn roles = %#v, want delegated claude", run.spawnRoles)
	}
	if len(run.sendReadyCalls) != 1 || run.sendReadyIDs[0] != "claude-scheduled" {
		t.Fatalf("ready sends = %#v", run.sendReadyCalls)
	}
	if len(run.abortCalls) != 0 {
		t.Fatalf("aborts = %#v, want none after successful launch", run.abortCalls)
	}
}

func TestLauncher_StartDedicatedUsesFreshConfiguredExecutor(t *testing.T) {
	for _, test := range []struct {
		name        string
		role        string
		executor    Executor
		wantCommand string
	}{
		{
			name:        "Claude direct",
			role:        "claude",
			executor:    Executor{Name: "claude", Command: "claude --flag"},
			wantCommand: "claude --flag --permission-mode bypassPermissions",
		},
		{
			name:        "Codex direct",
			role:        "codex",
			executor:    Executor{Name: "codex", Command: "codex --flag"},
			wantCommand: "codex --flag --dangerously-bypass-approvals-and-sandbox",
		},
		{
			name:        "Cursor direct",
			role:        "agent",
			executor:    Executor{Name: "agent", Command: "cursor-agent --force --sandbox disabled", Kind: "cursor"},
			wantCommand: "cursor-agent --force --sandbox disabled --trust --approve-mcps",
		},
		{
			name:        "Grok direct",
			role:        "grok",
			executor:    Executor{Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions"},
			wantCommand: "grok --no-alt-screen --permission-mode bypassPermissions --sandbox off",
		},
		{
			name:        "Claude env alias",
			role:        "claude",
			executor:    Executor{Name: "claude", Command: "env PROFILE=calendar -- cc --permission-mode=bypassPermissions"},
			wantCommand: "env PROFILE=calendar -- cc --permission-mode=bypassPermissions",
		},
		{
			name:        "Codex env attached aliases",
			role:        "codex",
			executor:    Executor{Name: "codex", Command: "env PROFILE=calendar codex -anever -sdanger-full-access"},
			wantCommand: "env PROFILE=calendar codex -anever -sdanger-full-access",
		},
		{
			name:        "Cursor env",
			role:        "agent",
			executor:    Executor{Name: "agent", Command: "env PROFILE=calendar -- cursor-agent --force --sandbox=disabled --trust --approve-mcps", Kind: "cursor"},
			wantCommand: "env PROFILE=calendar -- cursor-agent --force --sandbox=disabled --trust --approve-mcps",
		},
		{
			name:        "Grok env alias",
			role:        "grok",
			executor:    Executor{Name: "grok", Command: "env PROFILE=calendar grok-cli --permission-mode=bypassPermissions --sandbox=off"},
			wantCommand: "env PROFILE=calendar grok-cli --permission-mode=bypassPermissions --sandbox=off",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			role := test.role
			run := &fakeRunner{newID: role + "-scheduled"}
			execs := NewExecutorConfig(role, map[string]Executor{
				role: test.executor,
			})
			item := &Item{
				Path: "/tmp/scheduled.md",
				Frontmatter: Frontmatter{
					ID:      "scheduled",
					Created: time.Now(),
				},
			}

			started, err := NewLauncher(run, execs).StartDedicated(item, " /calendar ")
			if err != nil {
				t.Fatal(err)
			}
			if run.spawnCalls != 1 {
				t.Fatalf("spawn calls = %d, want 1", run.spawnCalls)
			}
			if run.spawnRoles[0] != role || run.spawnCwds[0] != "/calendar" || run.spawnCommands[0] != test.wantCommand {
				t.Fatalf("spawn = (%q, %q, %q)", run.spawnRoles[0], run.spawnCwds[0], run.spawnCommands[0])
			}
			if execs.ByName[role].Command != test.executor.Command {
				t.Fatalf("ordinary configured command mutated: got %q, want %q", execs.ByName[role].Command, test.executor.Command)
			}
			if len(run.sendReadyCalls) != 1 {
				t.Fatalf("ready sends = %#v", run.sendReadyCalls)
			}
			if !strings.Contains(run.sendReadyCalls[0], "|"+run.spawnCommands[0]+"|") {
				t.Fatalf("spawn command %q missing from handoff %q", run.spawnCommands[0], run.sendReadyCalls[0])
			}
			if len(run.abortCalls) != 0 {
				t.Fatalf("aborts = %#v, want none after successful launch", run.abortCalls)
			}
			if started.Frontmatter.Started == nil || started.Frontmatter.AgentSession != role+"-scheduled" {
				t.Fatalf("started item = %#v", started.Frontmatter)
			}
		})
	}
}

func TestLauncher_StartDedicatedRejectsProviderExecutableMismatchBeforeSpawn(t *testing.T) {
	for _, test := range []struct {
		name     string
		role     string
		executor Executor
	}{
		{
			name:     "Codex through sh",
			role:     "codex",
			executor: Executor{Name: "codex", Kind: AgentProviderCodex, Command: "sh -c codex"},
		},
		{
			name:     "Claude through bash",
			role:     "claude",
			executor: Executor{Name: "claude", Kind: AgentProviderClaude, Command: "bash -lc claude"},
		},
		{
			name:     "Cursor through other executable",
			role:     "cursor",
			executor: Executor{Name: "cursor", Kind: AgentProviderCursor, Command: "other cursor-agent --force --sandbox=disabled --trust --approve-mcps"},
		},
		{
			name:     "Grok through other executable",
			role:     "grok",
			executor: Executor{Name: "grok", Kind: AgentProviderGrok, Command: "other grok --permission-mode=bypassPermissions --sandbox=off"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := &fakeRunner{newID: test.role + "-scheduled"}
			execs := NewExecutorConfig(test.role, map[string]Executor{test.role: test.executor})
			_, err := NewLauncher(run, execs).StartDedicated(&Item{Path: "/tmp/scheduled.md"}, "/calendar")
			if !errors.Is(err, ErrScheduledActionUnattended) {
				t.Fatalf("error = %v, want ErrScheduledActionUnattended", err)
			}
			if run.spawnCalls != 0 || len(run.sendReadyCalls) != 0 || len(run.abortCalls) != 0 {
				t.Fatalf("runner effects: spawn=%d send=%#v abort=%#v", run.spawnCalls, run.sendReadyCalls, run.abortCalls)
			}
		})
	}
}

func TestLauncher_StartDedicatedRejectsInvalidScheduledArgvBeforeSpawn(t *testing.T) {
	for _, test := range []struct {
		name    string
		command string
	}{
		{name: "direct shell comment", command: "codex # configured-note"},
		{name: "env prefixed shell comment", command: "env PROFILE=calendar -- codex # configured-note"},
		{name: "attached approval conflict", command: "codex -aon-request -sdanger-full-access"},
		{name: "attached sandbox conflict", command: "codex -anever -sworkspace-write"},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := &fakeRunner{newID: "codex-scheduled"}
			execs := NewExecutorConfig("codex", map[string]Executor{
				"codex": {Name: "codex", Kind: AgentProviderCodex, Command: test.command},
			})
			_, err := NewLauncher(run, execs).StartDedicated(&Item{Path: "/tmp/scheduled.md"}, "/calendar")
			if !errors.Is(err, ErrScheduledActionUnattended) {
				t.Fatalf("error = %v, want ErrScheduledActionUnattended", err)
			}
			if run.spawnCalls != 0 || len(run.sendReadyCalls) != 0 || len(run.abortCalls) != 0 {
				t.Fatalf("runner effects: spawn=%d send=%#v abort=%#v", run.spawnCalls, run.sendReadyCalls, run.abortCalls)
			}
		})
	}
}

func TestLauncher_StartDedicatedAlreadyStarted(t *testing.T) {
	now := time.Now()
	run := &fakeRunner{}
	launcher := NewLauncher(run, &ExecutorConfig{})
	item := &Item{Frontmatter: Frontmatter{ID: "scheduled", Started: &now}}

	if _, err := launcher.StartDedicated(item, "/calendar"); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("error = %v, want ErrAlreadyStarted", err)
	}
	if run.spawnCalls != 0 {
		t.Fatalf("spawn calls = %d, want 0", run.spawnCalls)
	}
}

func TestLauncher_StartDedicatedRequiresConfiguredDelegatedExecutor(t *testing.T) {
	run := &fakeRunner{}
	execs := NewExecutorConfig("missing", map[string]Executor{})

	_, err := NewLauncher(run, execs).StartDedicated(&Item{}, "/calendar")
	if !errors.Is(err, ErrExecutorNotConfigured) {
		t.Fatalf("error = %v, want ErrExecutorNotConfigured", err)
	}
	if run.spawnCalls != 0 {
		t.Fatalf("spawn calls = %d, want 0", run.spawnCalls)
	}
}

func TestLauncher_StartDedicatedReportsSpawnAndReadySendFailures(t *testing.T) {
	execs := NewExecutorConfig("claude", map[string]Executor{
		"claude": {Name: "claude", Command: "claude"},
	})
	item := &Item{Path: "/tmp/scheduled.md"}

	t.Run("spawn", func(t *testing.T) {
		run := &fakeRunner{spawnErr: errors.New("tmux failed")}
		if _, err := NewLauncher(run, execs).StartDedicated(item, "/calendar"); !errors.Is(err, ErrSpawnFailed) {
			t.Fatalf("error = %v, want ErrSpawnFailed", err)
		}
		if len(run.sendReadyCalls) != 0 {
			t.Fatalf("ready sends = %#v, want none", run.sendReadyCalls)
		}
		if len(run.abortCalls) != 0 {
			t.Fatalf("aborts = %#v, want none when spawn failed", run.abortCalls)
		}
	})

	t.Run("ready send", func(t *testing.T) {
		run := &fakeRunner{newID: "claude-scheduled", sendReadyErr: errors.New("send failed")}
		if _, err := NewLauncher(run, execs).StartDedicated(item, "/calendar"); !errors.Is(err, ErrSpawnFailed) {
			t.Fatalf("error = %v, want ErrSpawnFailed", err)
		}
		if run.spawnCalls != 1 || len(run.sendReadyCalls) != 1 {
			t.Fatalf("spawn calls = %d, ready sends = %#v", run.spawnCalls, run.sendReadyCalls)
		}
		if len(run.abortCalls) != 1 || run.abortCalls[0] != "claude-scheduled" {
			t.Fatalf("aborts = %#v, want fresh session once", run.abortCalls)
		}
	})

	t.Run("ready send and abort", func(t *testing.T) {
		run := &fakeRunner{
			newID:        "claude-scheduled",
			sendReadyErr: errors.New("send failed"),
			abortErr:     errors.New("kill failed"),
		}
		_, err := NewLauncher(run, execs).StartDedicated(item, "/calendar")
		if !errors.Is(err, ErrSpawnFailed) {
			t.Fatalf("error = %v, want ErrSpawnFailed", err)
		}
		if !strings.Contains(err.Error(), "send failed") || !strings.Contains(err.Error(), "abort fresh session: kill failed") {
			t.Fatalf("error = %q, want original launch failure and cleanup context", err)
		}
		if len(run.abortCalls) != 1 || run.abortCalls[0] != "claude-scheduled" {
			t.Fatalf("aborts = %#v, want one attempt", run.abortCalls)
		}
	})
}
