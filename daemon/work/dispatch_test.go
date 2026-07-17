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
	execs := &ExecutorConfig{
		DelegatedExecutor: "claude",
		ByName: map[string]Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
		},
	}
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
	for _, role := range []string{"claude", "codex"} {
		t.Run(role, func(t *testing.T) {
			run := &fakeRunner{newID: role + "-scheduled"}
			execs := &ExecutorConfig{
				DelegatedExecutor: role,
				ByName: map[string]Executor{
					role: {Name: role, Command: role + " --flag"},
				},
			}
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
			if run.spawnRoles[0] != role || run.spawnCwds[0] != "/calendar" || run.spawnCommands[0] != role+" --flag" {
				t.Fatalf("spawn = (%q, %q, %q)", run.spawnRoles[0], run.spawnCwds[0], run.spawnCommands[0])
			}
			if len(run.sendReadyCalls) != 1 {
				t.Fatalf("ready sends = %#v", run.sendReadyCalls)
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
	execs := &ExecutorConfig{DelegatedExecutor: "missing", ByName: map[string]Executor{}}

	_, err := NewLauncher(run, execs).StartDedicated(&Item{}, "/calendar")
	if !errors.Is(err, ErrExecutorNotConfigured) {
		t.Fatalf("error = %v, want ErrExecutorNotConfigured", err)
	}
	if run.spawnCalls != 0 {
		t.Fatalf("spawn calls = %d, want 0", run.spawnCalls)
	}
}

func TestLauncher_StartDedicatedReportsSpawnAndReadySendFailures(t *testing.T) {
	execs := &ExecutorConfig{
		DelegatedExecutor: "claude",
		ByName: map[string]Executor{
			"claude": {Name: "claude", Command: "claude"},
		},
	}
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
