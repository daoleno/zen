package work

import (
	"errors"
	"testing"
	"time"
)

type fakeSession struct {
	id      string
	project string
	cwd     string
	role    string
	state   string
}

type fakeRegistry struct {
	sessions []fakeSession
}

func (f *fakeRegistry) IdleSessions(role, cwd string) []SessionInfo {
	out := []SessionInfo{}
	for _, session := range f.sessions {
		if session.role == role && session.cwd == cwd && session.state == "idle" {
			out = append(out, SessionInfo{
				ID:      session.id,
				Project: session.project,
				Cwd:     session.cwd,
				Role:    session.role,
			})
		}
	}
	return out
}

type fakeRunner struct {
	spawnCalls int
	sendCalls  []string
	spawnErr   error
	newID      string
}

func (f *fakeRunner) Spawn(role, cwd, command string) (string, error) {
	f.spawnCalls++
	if f.spawnErr != nil {
		return "", f.spawnErr
	}
	return f.newID, nil
}

func (f *fakeRunner) Send(sessionID, text string) error {
	f.sendCalls = append(f.sendCalls, sessionID+"|"+text)
	return nil
}

func TestLauncher_StartUsesIdleSession(t *testing.T) {
	reg := &fakeRegistry{sessions: []fakeSession{{id: "claude-1", cwd: "/p", role: "claude", state: "idle"}}}
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}

	iss := &Item{
		Path:     "/tmp/t.md",
		Project:  "p",
		Body:     "@claude do it",
		Mentions: []Mention{{Role: "claude"}},
		Frontmatter: Frontmatter{
			ID:      "T",
			Created: time.Now(),
		},
	}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	launcher := NewLauncher(reg, run, execs)

	updated, err := launcher.Start(iss, proj)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.spawnCalls != 0 {
		t.Fatalf("spawnCalls = %d, want 0", run.spawnCalls)
	}
	if len(run.sendCalls) != 1 {
		t.Fatalf("sendCalls = %d, want 1", len(run.sendCalls))
	}
	if updated.Frontmatter.AgentSession != "claude-1" {
		t.Fatalf("agent_session = %q", updated.Frontmatter.AgentSession)
	}
	if updated.Frontmatter.Started == nil {
		t.Fatal("started should be set")
	}
}

func TestLauncher_StartSpawnsWhenNoIdle(t *testing.T) {
	reg := &fakeRegistry{}
	run := &fakeRunner{newID: "claude-new"}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Item{
		Path:     "/tmp/t.md",
		Project:  "p",
		Mentions: []Mention{{Role: "claude"}},
		Frontmatter: Frontmatter{
			ID:      "T",
			Created: time.Now(),
		},
	}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	launcher := NewLauncher(reg, run, execs)

	updated, err := launcher.Start(iss, proj)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.spawnCalls != 1 {
		t.Fatalf("spawnCalls = %d, want 1", run.spawnCalls)
	}
	if updated.Frontmatter.AgentSession != "claude-new" {
		t.Fatalf("agent_session = %q", updated.Frontmatter.AgentSession)
	}
}

func TestLauncher_StartAlreadyStartedError(t *testing.T) {
	now := time.Now()
	launcher := NewLauncher(&fakeRegistry{}, &fakeRunner{}, &ExecutorConfig{Default: "claude"})
	iss := &Item{Frontmatter: Frontmatter{ID: "T", Started: &now}}
	if _, err := launcher.Start(iss, Project{Cwd: "/p"}); !errors.Is(err, ErrAlreadyStarted) {
		t.Fatalf("err = %v, want ErrAlreadyStarted", err)
	}
}

func TestLauncher_StartNoExecutorError(t *testing.T) {
	execs := &ExecutorConfig{Default: "missing", ByName: map[string]Executor{}}
	iss := &Item{Frontmatter: Frontmatter{ID: "T"}, Mentions: []Mention{{Role: "missing"}}}
	launcher := NewLauncher(&fakeRegistry{}, &fakeRunner{}, execs)
	if _, err := launcher.Start(iss, Project{Cwd: "/p"}); !errors.Is(err, ErrExecutorNotConfigured) {
		t.Fatalf("err = %v, want ErrExecutorNotConfigured", err)
	}
}

func TestLauncher_StartRespectsSessionMention(t *testing.T) {
	reg := &fakeRegistry{sessions: []fakeSession{
		{id: "claude-1", cwd: "/p", role: "claude", state: "idle"},
		{id: "claude-2", cwd: "/p", role: "claude", state: "idle"},
	}}
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Item{
		Path:     "/tmp/t.md",
		Project:  "p",
		Mentions: []Mention{{Role: "claude", Session: "claude-2"}},
		Frontmatter: Frontmatter{
			ID:      "T",
			Created: time.Now(),
		},
	}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	launcher := NewLauncher(reg, run, execs)

	updated, err := launcher.Start(iss, proj)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if updated.Frontmatter.AgentSession != "claude-2" {
		t.Fatalf("agent_session = %q", updated.Frontmatter.AgentSession)
	}
}

func TestLauncher_StartSessionMentionSkipsProjectCwdRequirement(t *testing.T) {
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Item{
		Path:     "/tmp/t.md",
		Project:  "inbox",
		Mentions: []Mention{{Role: "claude", Session: "claude-inbox-1"}},
		Frontmatter: Frontmatter{
			ID:      "T",
			Created: time.Now(),
		},
	}
	launcher := NewLauncher(&fakeRegistry{}, run, execs)

	updated, err := launcher.Start(iss, Project{Name: "inbox"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if run.spawnCalls != 0 {
		t.Fatalf("spawnCalls = %d, want 0", run.spawnCalls)
	}
	if updated.Frontmatter.AgentSession != "claude-inbox-1" {
		t.Fatalf("agent_session = %q", updated.Frontmatter.AgentSession)
	}
	if len(run.sendCalls) != 1 {
		t.Fatalf("sendCalls = %d, want 1", len(run.sendCalls))
	}
}

func TestLauncher_RerunClearsFields(t *testing.T) {
	now := time.Now()
	reg := &fakeRegistry{sessions: []fakeSession{{id: "claude-1", cwd: "/p", role: "claude", state: "idle"}}}
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Item{
		Path:     "/tmp/t.md",
		Project:  "p",
		Mentions: []Mention{{Role: "claude"}},
		Frontmatter: Frontmatter{
			ID:           "T",
			Created:      time.Now(),
			Started:      &now,
			AgentSession: "old",
		},
	}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	launcher := NewLauncher(reg, run, execs)

	updated, err := launcher.Rerun(iss, proj)
	if err != nil {
		t.Fatalf("Rerun: %v", err)
	}
	if updated.Frontmatter.AgentSession != "claude-1" {
		t.Fatalf("agent_session = %q", updated.Frontmatter.AgentSession)
	}
	if updated.Frontmatter.Started == nil {
		t.Fatal("started should be set")
	}
}
