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
}

type fakeControlSend struct {
	id   string
	text string
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
		UpdatedAt: time.Date(2026, 5, 27, 9, 0, 0, 0, time.UTC),
	}
	return id, nil
}

func (w *fakeControlWatcher) SendInput(sessionID, text string) error {
	w.sent = append(w.sent, fakeControlSend{id: sessionID, text: text})
	return nil
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
	if len(fw.sent) != 1 {
		t.Fatalf("sent calls = %#v", fw.sent)
	}
	wantPrompt := "delegated by Brain\n\nimplement this\n"
	if fw.sent[0].id != resp.Agent.ID || fw.sent[0].text != wantPrompt {
		t.Fatalf("prompt send = %#v, want id %q text %q", fw.sent[0], resp.Agent.ID, wantPrompt)
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
	if resp.Adapter.Capabilities.NativeThreads || !resp.Adapter.Capabilities.InteractiveTTY {
		t.Fatalf("claude capabilities = %+v", resp.Adapter.Capabilities)
	}
}

func TestControlAppBrainThreadsRejectsAdapterWithoutNativeThreads(t *testing.T) {
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
			},
		},
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_threads"})

	if resp.OK || resp.Error == nil || resp.Error.Code != "native_threads_unavailable" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestControlAppBrainThreadsUsesNativeSearch(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "fake-codex")
	body := `#!/bin/sh
set -eu
read init
case "$init" in
  *'"method":"initialize"'*) ;;
  *) echo "bad initialize: $init" >&2; exit 11 ;;
esac
printf '%s\n' '{"id":"zen-init","result":{"userAgent":"fake","codexHome":"/tmp/codex","platformFamily":"unix","platformOs":"linux"}}'
printf '%s\n' '{"method":"remoteControl/status/changed","params":{"status":"disabled"}}'
read ready
case "$ready" in
  *'"method":"initialized"'*) ;;
  *) echo "bad initialized: $ready" >&2; exit 12 ;;
esac
read req
case "$req" in
  *'"method":"thread/search"'*) ;;
  *) echo "bad request method: $req" >&2; exit 13 ;;
esac
case "$req" in
  *'"searchTerm":"Brain"'*) ;;
  *) echo "bad search term: $req" >&2; exit 14 ;;
esac
printf '%s\n' '{"id":"zen-1","result":{"data":[{"thread":{"id":"thread-1","sessionId":"session-1","forkedFromId":null,"preview":"Thread preview","ephemeral":false,"modelProvider":"openai","createdAt":1780331643,"updatedAt":1780333776,"status":{"type":"notLoaded"},"path":"/tmp/codex/rollout.jsonl","cwd":"/repo/zen","source":"cli","name":"Brain result"},"snippet":"Brain matched here"}],"nextCursor":"next","backwardsCursor":"back"}}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		brainStore: store,
		execs: &work.ExecutorConfig{
			Default: "codex",
			ByName: map[string]work.Executor{
				"codex": {Name: "codex", Command: script},
			},
		},
	}

	resp := app.HandleControlRequest(control.Request{
		Type:       "brain_threads",
		SearchTerm: "Brain",
		Limit:      3,
	})

	if !resp.OK || resp.Adapter == nil || resp.Adapter.ID != "codex" {
		t.Fatalf("response = %#v", resp)
	}
	if resp.NextCursor != "next" || resp.BackwardsCursor != "back" {
		t.Fatalf("cursors = %q / %q", resp.NextCursor, resp.BackwardsCursor)
	}
	if len(resp.Threads) != 1 {
		t.Fatalf("threads = %#v", resp.Threads)
	}
	thread := resp.Threads[0]
	if thread.ID != "codex:thread-1" || thread.Title != "Brain result" || thread.Snippet != "Brain matched here" {
		t.Fatalf("thread = %+v", thread)
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
