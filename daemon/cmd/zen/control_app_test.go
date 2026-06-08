package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type fakeControlWatcher struct {
	agents   map[string]*classifier.Agent
	created  []watcher.CreateSessionOptions
	sent     []fakeControlSend
	killed   []string
	captures map[string]string
	progress []fakeControlProgress
}

type fakeControlSend struct {
	id   string
	text string
}

type fakeControlProgress struct {
	id       string
	progress classifier.AgentProgress
}

func newFakeControlWatcher() *fakeControlWatcher {
	return &fakeControlWatcher{
		agents:   map[string]*classifier.Agent{},
		captures: map[string]string{},
	}
}

func (w *fakeControlWatcher) Agents() []*classifier.Agent {
	out := make([]*classifier.Agent, 0, len(w.agents))
	for _, agent := range w.agents {
		cp := *agent
		out = append(out, &cp)
	}
	return out
}

func (w *fakeControlWatcher) GetAgent(id string) *classifier.Agent {
	agent := w.agents[id]
	if agent == nil {
		return nil
	}
	cp := *agent
	return &cp
}

func (w *fakeControlWatcher) HasSession(target string) bool {
	_, ok := w.agents[target]
	return ok
}

func (w *fakeControlWatcher) CreateSession(_ string, opts watcher.CreateSessionOptions) (string, error) {
	id := "brain-agent-" + strings.ToLower(strings.ReplaceAll(opts.Name, " ", "-")) + ":@1"
	w.created = append(w.created, opts)
	w.agents[id] = &classifier.Agent{
		ID:        id,
		Name:      opts.Name + " (" + id + ")",
		State:     classifier.StateRunning,
		Summary:   "Session starting",
		Cwd:       opts.Cwd,
		Command:   opts.Command,
		Hidden:    opts.Hidden,
		Delegated: !opts.Hidden && !strings.HasPrefix(id, "brain-agent-brain-"),
		UpdatedAt: time.Date(2026, 5, 27, 9, 0, 0, 0, time.UTC),
	}
	return id, nil
}

func (w *fakeControlWatcher) UpdateAgentProgress(id string, progress classifier.AgentProgress) (*classifier.Agent, error) {
	agent := w.agents[id]
	if agent == nil {
		return nil, os.ErrNotExist
	}
	w.progress = append(w.progress, fakeControlProgress{id: id, progress: progress})
	classifier.ApplyProgress(agent, progress, time.Date(2026, 6, 8, 9, 0, 0, 0, time.UTC))
	cp := *agent
	return &cp, nil
}

func (w *fakeControlWatcher) SendInput(sessionID, text string) error {
	w.sent = append(w.sent, fakeControlSend{id: sessionID, text: text})
	return nil
}

func (w *fakeControlWatcher) SendInputWhenReady(sessionID, _ string, text string) error {
	return w.SendInput(sessionID, text)
}

func (w *fakeControlWatcher) KillSession(sessionID string) error {
	w.killed = append(w.killed, sessionID)
	delete(w.agents, sessionID)
	return nil
}

func (w *fakeControlWatcher) CapturePaneContent(sessionID string) (string, error) {
	return w.captures[sessionID], nil
}

func TestControlAppAgentSpawnCreatesVisibleDetachedSession(t *testing.T) {
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher: fw,
		execs: &work.ExecutorConfig{
			Default: "codex",
			ByName: map[string]work.Executor{
				"codex": {Name: "codex", Command: "codex --no-alt-screen"},
			},
		},
		stateDir: "/tmp/zen state",
	}
	promptPath := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptPath, []byte("implement this"), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	resp := app.HandleControlRequest(control.Request{
		Type:       "agent_spawn",
		Name:       "Franklin",
		Executor:   "codex",
		Cwd:        "/repo/zen",
		Prompt:     "delegated by Brain",
		PromptFile: promptPath,
	})

	if !resp.OK || resp.Agent == nil {
		t.Fatalf("spawn response = %#v", resp)
	}
	if resp.Agent.Name != "Franklin (brain-agent-franklin:@1)" {
		t.Fatalf("agent name = %q", resp.Agent.Name)
	}
	if resp.Agent.Hidden {
		t.Fatal("delegated agent should be visible by default")
	}
	if !resp.Agent.Delegated {
		t.Fatal("brain-spawned visible agent should be marked delegated")
	}
	if len(fw.created) != 1 {
		t.Fatalf("created sessions = %#v", fw.created)
	}
	created := fw.created[0]
	if created.Name != "Franklin" || created.Command != "codex --no-alt-screen" || created.Cwd != "/repo/zen" {
		t.Fatalf("create options = %+v", created)
	}
	if !created.Detached || created.Hidden {
		t.Fatalf("create visibility options = %+v", created)
	}
	if !created.ProgressEnv {
		t.Fatalf("progress env was not enabled: %+v", created)
	}
	if created.Env["ZEN_AGENT_PROGRESS_CMD"] != "zen agent progress" || created.Env["ZEN_STATE_DIR"] != "/tmp/zen state" {
		t.Fatalf("progress command env = %#v", created.Env)
	}
	if len(fw.sent) != 1 {
		t.Fatalf("sent calls = %#v", fw.sent)
	}
	if fw.sent[0].id != resp.Agent.ID {
		t.Fatalf("prompt sent to %q, want %q", fw.sent[0].id, resp.Agent.ID)
	}
	for _, want := range []string{
		"delegated by Brain\n\nimplement this",
		"Zen lifecycle protocol:",
		`$ZEN_AGENT_PROGRESS_CMD --status running --phase working --attention none --summary "Short current work" --lease 300`,
		"ZEN_AGENT_ID is already set for this session.",
		"Valid status values: running, done, failed, blocked.",
	} {
		if !strings.Contains(fw.sent[0].text, want) {
			t.Fatalf("prompt missing %q:\n%s", want, fw.sent[0].text)
		}
	}
	if strings.Contains(fw.sent[0].text, "[zen:"+"progress]") {
		t.Fatalf("prompt should not contain stdout marker protocol:\n%s", fw.sent[0].text)
	}
}

func TestControlAppAgentSpawnRequiresExplicitWorkingDirectory(t *testing.T) {
	app := &controlApp{watcher: newFakeControlWatcher()}

	resp := app.HandleControlRequest(control.Request{Type: "agent_spawn", Name: "Franklin"})

	if resp.OK || resp.Error == nil || resp.Error.Code != "missing_cwd" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestControlAppAgentListFiltersHiddenAgents(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.agents["main:@1"] = &classifier.Agent{
		ID:        "main:@1",
		Name:      "Franklin",
		State:     classifier.StateRunning,
		UpdatedAt: time.Date(2026, 5, 27, 8, 0, 0, 0, time.UTC),
	}
	fw.agents["main:@2"] = &classifier.Agent{
		ID:        "main:@2",
		Name:      "Brain",
		State:     classifier.StateRunning,
		Hidden:    true,
		UpdatedAt: time.Date(2026, 5, 27, 9, 0, 0, 0, time.UTC),
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{Type: "agent_list"})

	if !resp.OK || len(resp.Agents) != 1 || resp.Agents[0].ID != "main:@1" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestControlAppAgentSendAndCapture(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.agents["main:@1"] = &classifier.Agent{ID: "main:@1", Name: "Franklin", State: classifier.StateRunning}
	fw.captures["main:@1"] = "current pane"
	app := &controlApp{watcher: fw}

	sendResp := app.HandleControlRequest(control.Request{
		Type:    "agent_send",
		AgentID: "main:@1",
		Text:    "continue",
		Submit:  true,
	})
	if !sendResp.OK {
		t.Fatalf("send response = %#v", sendResp)
	}
	if len(fw.sent) != 1 || fw.sent[0].text != "continue\n" {
		t.Fatalf("sent calls = %#v", fw.sent)
	}

	captureResp := app.HandleControlRequest(control.Request{Type: "agent_capture", AgentID: "main:@1"})
	if !captureResp.OK || captureResp.Text != "current pane" || captureResp.Agent == nil {
		t.Fatalf("capture response = %#v", captureResp)
	}
}

func TestControlAppAgentStatusReturnsProgressFields(t *testing.T) {
	fw := newFakeControlWatcher()
	now := time.Date(2026, 6, 7, 8, 0, 0, 0, time.UTC)
	nextCheck := now.Add(5 * time.Minute)
	fw.agents["main:@1"] = &classifier.Agent{
		ID:                  "main:@1",
		Name:                "Franklin",
		State:               classifier.StateRunning,
		Summary:             "Adding close guard",
		Phase:               "working",
		Attention:           "none",
		NeedsAttention:      false,
		LastProgressAt:      &now,
		ExpectedNextCheckAt: &nextCheck,
		LeaseSeconds:        300,
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{Type: "agent_status", AgentID: "main:@1"})

	if !resp.OK || resp.Agent == nil {
		t.Fatalf("status response = %#v", resp)
	}
	if resp.Agent.Phase != "working" || resp.Agent.Attention != "none" || resp.Agent.LeaseSeconds != 300 {
		t.Fatalf("agent progress fields = %#v", resp.Agent)
	}
	if resp.Agent.LastProgressAt == nil || !resp.Agent.LastProgressAt.Equal(now) {
		t.Fatalf("last progress = %#v, want %s", resp.Agent.LastProgressAt, now)
	}
}

func TestControlAppAgentProgressUpdatesAgent(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.agents["brain-agent-worker:@1"] = &classifier.Agent{
		ID:        "brain-agent-worker:@1",
		Name:      "Worker",
		State:     classifier.StateRunning,
		Delegated: true,
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{
		Type:         "agent_progress",
		AgentID:      "brain-agent-worker:@1",
		Status:       "blocked",
		Phase:        "working",
		Attention:    "user_input",
		Summary:      "Need a decision",
		LeaseSeconds: 300,
	})

	if !resp.OK || resp.Agent == nil {
		t.Fatalf("progress response = %#v", resp)
	}
	if resp.Agent.Status != "blocked" || resp.Agent.Phase != "working" || resp.Agent.Attention != "user_input" {
		t.Fatalf("agent response = %#v", resp.Agent)
	}
	if !resp.Agent.NeedsAttention || resp.Agent.Summary != "Need a decision" || resp.Agent.LeaseSeconds != 300 {
		t.Fatalf("agent progress metadata = %#v", resp.Agent)
	}
	if resp.Agent.LastProgressAt == nil || resp.Agent.ExpectedNextCheckAt == nil {
		t.Fatalf("progress timestamps missing: %#v", resp.Agent)
	}
	if len(fw.progress) != 1 || fw.progress[0].id != "brain-agent-worker:@1" {
		t.Fatalf("progress calls = %#v", fw.progress)
	}
}

func TestControlAppAgentProgressRejectsInvalidValues(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.agents["brain-agent-worker:@1"] = &classifier.Agent{ID: "brain-agent-worker:@1", State: classifier.StateRunning}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{
		Type:      "agent_progress",
		AgentID:   "brain-agent-worker:@1",
		Status:    "completed",
		Phase:     "working",
		Attention: "none",
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != "invalid_progress" {
		t.Fatalf("progress response = %#v", resp)
	}
	if len(fw.progress) != 0 {
		t.Fatalf("unexpected progress calls = %#v", fw.progress)
	}
}

func TestControlAppAgentCloseKillsSession(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.agents["brain-agent-worker:@1"] = &classifier.Agent{
		ID:        "brain-agent-worker:@1",
		Name:      "Worker",
		State:     classifier.StateDone,
		Delegated: true,
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: "brain-agent-worker:@1"})

	if !resp.OK || resp.Agent == nil {
		t.Fatalf("close response = %#v", resp)
	}
	if len(fw.killed) != 1 || fw.killed[0] != "brain-agent-worker:@1" {
		t.Fatalf("killed sessions = %#v", fw.killed)
	}
	if fw.HasSession("brain-agent-worker:@1") {
		t.Fatal("closed session still exists")
	}
	if resp.Agent.Status != string(classifier.StateRemoved) {
		t.Fatalf("closed status = %q", resp.Agent.Status)
	}
}

func TestControlAppAgentCloseRequiresForceForRunningDelegatedAgent(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.agents["brain-agent-worker:@1"] = &classifier.Agent{
		ID:        "brain-agent-worker:@1",
		Name:      "Worker",
		State:     classifier.StateRunning,
		Delegated: true,
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: "brain-agent-worker:@1"})

	if resp.OK || resp.Error == nil || resp.Error.Code != "agent_running_requires_force" {
		t.Fatalf("close response = %#v", resp)
	}
	if len(fw.killed) != 0 {
		t.Fatalf("killed sessions = %#v", fw.killed)
	}

	forced := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: "brain-agent-worker:@1", Force: true})
	if !forced.OK || len(fw.killed) != 1 {
		t.Fatalf("forced close response = %#v killed=%#v", forced, fw.killed)
	}
}

func TestControlAppBrainWorkspaceReturnsStoreWorkspace(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{brainStore: store}

	resp := app.HandleControlRequest(control.Request{Type: "brain_workspace"})

	if !resp.OK || resp.Workspace != store.WorkspacePath() {
		t.Fatalf("response = %#v, want workspace %q", resp, store.WorkspacePath())
	}
}

func TestControlAppBrainAdaptersListsCurrentAdapter(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		brainStore: store,
		execs: &work.ExecutorConfig{
			Default: "claude",
			ByName: map[string]work.Executor{
				"claude": {Name: "claude", Command: "claude"},
				"codex":  {Name: "codex", Command: "codex"},
			},
		},
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_adapters"})

	if !resp.OK || resp.Adapter == nil || resp.Adapter.ID != "claude" {
		t.Fatalf("response = %#v", resp)
	}
	if len(resp.Adapters) != 2 || resp.Adapters[0].ID != "claude" || !resp.Adapters[0].Preferred {
		t.Fatalf("adapters = %#v", resp.Adapters)
	}
	if !resp.Adapter.Capabilities.InteractiveTTY {
		t.Fatalf("claude capabilities = %+v", resp.Adapter.Capabilities)
	}
}

func TestControlAppBrainSetAdapterPersistsConfiguredAdapter(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		brainStore: store,
		execs: &work.ExecutorConfig{
			Default: "codex",
			ByName: map[string]work.Executor{
				"claude": {Name: "claude", Command: "claude"},
				"codex":  {Name: "codex", Command: "codex"},
			},
		},
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_set_adapter", AdapterID: "claude"})

	if !resp.OK || resp.Adapter == nil || resp.Adapter.ID != "claude" {
		t.Fatalf("response = %#v", resp)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.AdapterID != "claude" {
		t.Fatalf("adapter id = %q", hostSession.AdapterID)
	}
	if len(resp.Adapters) != 2 || resp.Adapters[0].ID != "claude" || !resp.Adapters[0].Preferred {
		t.Fatalf("adapters = %#v", resp.Adapters)
	}
}

func TestControlAppBrainSetAdapterStartsSelectedHostWhenWatcherAvailable(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs: &work.ExecutorConfig{
			Default: "codex",
			ByName: map[string]work.Executor{
				"claude": {Name: "claude", Command: "claude"},
				"codex":  {Name: "codex", Command: "codex"},
			},
		},
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_set_adapter", AdapterID: "claude"})

	if !resp.OK || resp.Adapter == nil || resp.Adapter.ID != "claude" {
		t.Fatalf("response = %#v", resp)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created sessions = %#v", fw.created)
	}
	if !fw.created[0].Hidden || !strings.HasPrefix(fw.created[0].Command, "claude") {
		t.Fatalf("created host = %+v", fw.created[0])
	}
	if len(fw.sent) != 1 || !strings.Contains(fw.sent[0].text, "Active adapter: claude") {
		t.Fatalf("bootstrap prompt = %#v", fw.sent)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID == "" || hostSession.AdapterID != "claude" {
		t.Fatalf("host session = %+v", hostSession)
	}
}

func TestControlAppBrainSetAdapterRejectsUnknownAdapter(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		brainStore: store,
		execs: &work.ExecutorConfig{
			Default: "codex",
			ByName: map[string]work.Executor{
				"codex": {Name: "codex", Command: "codex"},
			},
		},
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_set_adapter", AdapterID: "claude"})

	if resp.OK || resp.Error == nil || resp.Error.Code != "invalid_adapter" {
		t.Fatalf("response = %#v", resp)
	}
}
