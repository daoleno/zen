package issue

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

func TestDispatch_UsesIdleSession(t *testing.T) {
	reg := &fakeRegistry{sessions: []fakeSession{{id: "claude-1", cwd: "/p", role: "claude", state: "idle"}}}
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}

	iss := &Issue{
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
	dispatcher := NewDispatcher(reg, run, execs)

	updated, err := dispatcher.Dispatch(iss, proj)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
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
	if updated.Frontmatter.Dispatched == nil {
		t.Fatal("dispatched should be set")
	}
}

func TestDispatch_SpawnsWhenNoIdle(t *testing.T) {
	reg := &fakeRegistry{}
	run := &fakeRunner{newID: "claude-new"}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Issue{
		Path:     "/tmp/t.md",
		Project:  "p",
		Mentions: []Mention{{Role: "claude"}},
		Frontmatter: Frontmatter{
			ID:      "T",
			Created: time.Now(),
		},
	}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	dispatcher := NewDispatcher(reg, run, execs)

	updated, err := dispatcher.Dispatch(iss, proj)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if run.spawnCalls != 1 {
		t.Fatalf("spawnCalls = %d, want 1", run.spawnCalls)
	}
	if updated.Frontmatter.AgentSession != "claude-new" {
		t.Fatalf("agent_session = %q", updated.Frontmatter.AgentSession)
	}
}

func TestDispatch_AlreadyDispatchedError(t *testing.T) {
	now := time.Now()
	dispatcher := NewDispatcher(&fakeRegistry{}, &fakeRunner{}, &ExecutorConfig{Default: "claude"})
	iss := &Issue{Frontmatter: Frontmatter{ID: "T", Dispatched: &now}}
	if _, err := dispatcher.Dispatch(iss, Project{Cwd: "/p"}); !errors.Is(err, ErrAlreadyDispatched) {
		t.Fatalf("err = %v, want ErrAlreadyDispatched", err)
	}
}

func TestDispatch_NoExecutorError(t *testing.T) {
	execs := &ExecutorConfig{Default: "missing", ByName: map[string]Executor{}}
	iss := &Issue{Frontmatter: Frontmatter{ID: "T"}, Mentions: []Mention{{Role: "missing"}}}
	dispatcher := NewDispatcher(&fakeRegistry{}, &fakeRunner{}, execs)
	if _, err := dispatcher.Dispatch(iss, Project{Cwd: "/p"}); !errors.Is(err, ErrExecutorNotConfigured) {
		t.Fatalf("err = %v, want ErrExecutorNotConfigured", err)
	}
}

func TestDispatch_RespectsSessionMention(t *testing.T) {
	reg := &fakeRegistry{sessions: []fakeSession{
		{id: "claude-1", cwd: "/p", role: "claude", state: "idle"},
		{id: "claude-2", cwd: "/p", role: "claude", state: "idle"},
	}}
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Issue{
		Path:     "/tmp/t.md",
		Project:  "p",
		Mentions: []Mention{{Role: "claude", Session: "claude-2"}},
		Frontmatter: Frontmatter{
			ID:      "T",
			Created: time.Now(),
		},
	}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	dispatcher := NewDispatcher(reg, run, execs)

	updated, err := dispatcher.Dispatch(iss, proj)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if updated.Frontmatter.AgentSession != "claude-2" {
		t.Fatalf("agent_session = %q", updated.Frontmatter.AgentSession)
	}
}

func TestDispatch_SessionMentionSkipsProjectCwdRequirement(t *testing.T) {
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Issue{
		Path:     "/tmp/t.md",
		Project:  "inbox",
		Mentions: []Mention{{Role: "claude", Session: "claude-inbox-1"}},
		Frontmatter: Frontmatter{
			ID:      "T",
			Created: time.Now(),
		},
	}
	dispatcher := NewDispatcher(&fakeRegistry{}, run, execs)

	updated, err := dispatcher.Dispatch(iss, Project{Name: "inbox"})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
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

func TestDispatch_RedispatchClearsFields(t *testing.T) {
	now := time.Now()
	reg := &fakeRegistry{sessions: []fakeSession{{id: "claude-1", cwd: "/p", role: "claude", state: "idle"}}}
	run := &fakeRunner{}
	execs := &ExecutorConfig{Default: "claude", ByName: map[string]Executor{"claude": {Name: "claude", Command: "claude"}}}
	iss := &Issue{
		Path:     "/tmp/t.md",
		Project:  "p",
		Mentions: []Mention{{Role: "claude"}},
		Frontmatter: Frontmatter{
			ID:           "T",
			Created:      time.Now(),
			Dispatched:   &now,
			AgentSession: "old",
		},
	}
	proj := Project{Name: "p", Cwd: "/p", Executor: "claude"}
	dispatcher := NewDispatcher(reg, run, execs)

	updated, err := dispatcher.Redispatch(iss, proj)
	if err != nil {
		t.Fatalf("Redispatch: %v", err)
	}
	if updated.Frontmatter.AgentSession != "claude-1" {
		t.Fatalf("agent_session = %q", updated.Frontmatter.AgentSession)
	}
	if updated.Frontmatter.Dispatched == nil {
		t.Fatal("dispatched should be set")
	}
}
