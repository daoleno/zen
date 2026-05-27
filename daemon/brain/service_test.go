package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type fakeWatcher struct {
	agents    []*classifier.Agent
	sessions  map[string]*classifier.Agent
	created   []createdCall
	sentCalls []sentCall
}

type createdCall struct {
	id   string
	opts watcher.CreateSessionOptions
}

type sentCall struct {
	sessionID string
	text      string
}

func (w *fakeWatcher) Agents() []*classifier.Agent {
	out := make([]*classifier.Agent, 0, len(w.agents))
	for _, agent := range w.agents {
		cp := *agent
		out = append(out, &cp)
	}
	return out
}

func (w *fakeWatcher) GetAgent(id string) *classifier.Agent {
	if w.sessions != nil {
		if agent, ok := w.sessions[id]; ok {
			cp := *agent
			return &cp
		}
	}
	return nil
}

func (w *fakeWatcher) HasSession(target string) bool {
	if w.sessions == nil {
		return false
	}
	_, ok := w.sessions[target]
	return ok
}

func (w *fakeWatcher) CreateSession(_ string, opts watcher.CreateSessionOptions) (string, error) {
	if w.sessions == nil {
		w.sessions = map[string]*classifier.Agent{}
	}
	id := "brain-agent-" + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(opts.Name), " ", "-"))
	if opts.Hidden {
		id += "-hidden"
	}
	id += fmt.Sprintf(":@%d", len(w.created)+1)
	agent := &classifier.Agent{
		ID:      id,
		Name:    opts.Name + " (" + id + ")",
		Cwd:     opts.Cwd,
		Command: opts.Command,
		State:   classifier.StateRunning,
		Summary: "Session starting",
		Hidden:  opts.Hidden,
	}
	w.created = append(w.created, createdCall{id: id, opts: opts})
	w.sessions[id] = agent
	w.agents = append(w.agents, agent)
	return id, nil
}

func (w *fakeWatcher) SendInput(sessionID, text string) error {
	w.sentCalls = append(w.sentCalls, sentCall{sessionID: sessionID, text: text})
	return nil
}

func TestServiceSnapshotCreatesHiddenHostSession(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, nil)

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("snapshot should create exactly one host session, got %#v", fw.created)
	}
	if !fw.created[0].opts.Hidden || !fw.created[0].opts.Detached {
		t.Fatalf("host session options = %+v", fw.created[0].opts)
	}
	if snapshot.Workspace != store.WorkspacePath() {
		t.Fatalf("workspace = %q, want %q", snapshot.Workspace, store.WorkspacePath())
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != fw.created[0].id {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	if len(fw.sentCalls) == 0 || fw.sentCalls[0].sessionID != fw.created[0].id {
		t.Fatalf("expected bootstrap prompt to be sent to host, got %#v", fw.sentCalls)
	}
}

func TestServiceSnapshotReusesMatchingHostSession(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		},
	})

	first, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("expected existing codex host to be reused, got %#v", fw.created)
	}
	if first.HostAgent == nil || second.HostAgent == nil || first.HostAgent.ID != second.HostAgent.ID {
		t.Fatalf("host agents = %#v / %#v", first.HostAgent, second.HostAgent)
	}
}

func TestServiceSnapshotReplacesMismatchedHostSession(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "brain-agent-brain-old:@1"
	if err := store.SetHostSessionID(oldID); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {
				ID:      oldID,
				Name:    "Brain (" + oldID + ")",
				Cwd:     store.WorkspacePath(),
				Command: "claude",
				State:   classifier.StateRunning,
				Hidden:  true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[oldID])
	service := NewService(store, fw, &work.ExecutorConfig{
		ByName: map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		},
	})

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.created) != 1 {
		t.Fatalf("expected mismatched host to be replaced, got %#v", fw.created)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID == oldID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
}

func TestServiceSnapshotFiltersHiddenHostFromVisibleAgents(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		agents: []*classifier.Agent{{
			ID:      "main:@1",
			Name:    "Codex (main:@1)",
			State:   classifier.StateRunning,
			Summary: "working",
		}},
	}
	service := NewService(store, fw, nil)

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Agents) != 1 || snapshot.Agents[0].ID != "main:@1" {
		t.Fatalf("visible agents = %#v", snapshot.Agents)
	}
	if snapshot.HostAgent == nil || !snapshot.HostAgent.Hidden {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
}

func TestStoreUsesStateAndWorkspaceDirectories(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace := filepath.Join(root, "workspace")
	if store.WorkspacePath() != wantWorkspace {
		t.Fatalf("workspace path = %q, want %q", store.WorkspacePath(), wantWorkspace)
	}
	if !pathExists(filepath.Join(root, "state", "messages.jsonl")) {
		t.Fatalf("missing state messages file")
	}
	if !pathExists(filepath.Join(root, "state", "reminders.json")) {
		t.Fatalf("missing state reminders file")
	}
	if !pathExists(filepath.Join(root, "workspace", "memory.md")) {
		t.Fatalf("missing workspace memory file")
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
