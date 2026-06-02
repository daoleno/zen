package brain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

type fakeWatcher struct {
	agents    []*classifier.Agent
	sessions  map[string]*classifier.Agent
	created   []createdCall
	sentCalls []sentCall
	killed    []string
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

func (w *fakeWatcher) KillSession(sessionID string) error {
	w.killed = append(w.killed, sessionID)
	if w.sessions != nil {
		delete(w.sessions, sessionID)
	}
	nextAgents := w.agents[:0]
	for _, agent := range w.agents {
		if agent.ID != sessionID {
			nextAgents = append(nextAgents, agent)
		}
	}
	w.agents = nextAgents
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
	if snapshot.HostAdapter == nil || snapshot.HostAdapter.Provider != "claude" || snapshot.HostAdapter.Runtime != work.AgentRuntimeTmux {
		t.Fatalf("host adapter = %#v", snapshot.HostAdapter)
	}
	if len(snapshot.Adapters) == 0 || !snapshot.Adapters[0].Preferred {
		t.Fatalf("adapters = %#v", snapshot.Adapters)
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

func TestServiceSnapshotUsesConfiguredDefaultAdapter(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		Default: "claude",
		ByName: map[string]work.Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
		},
	})

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostAdapter == nil || snapshot.HostAdapter.ID != "claude" {
		t.Fatalf("host adapter = %#v", snapshot.HostAdapter)
	}
	if len(fw.created) != 1 || !strings.HasPrefix(fw.created[0].opts.Command, "claude") {
		t.Fatalf("created host = %#v", fw.created)
	}
}

func TestServiceSnapshotHonorsHostAdapterOverride(t *testing.T) {
	t.Setenv("ZEN_BRAIN_HOST_ADAPTER", "claude")
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		Default: "codex",
		ByName: map[string]work.Executor{
			"codex":  {Name: "codex", Command: "codex"},
			"claude": {Name: "claude", Command: "claude"},
		},
	})

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostAdapter == nil || snapshot.HostAdapter.ID != "claude" || snapshot.HostAdapter.Provider != "claude" {
		t.Fatalf("host adapter = %#v", snapshot.HostAdapter)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created sessions = %#v", fw.created)
	}
	command := fw.created[0].opts.Command
	if !strings.HasPrefix(command, "claude") || !strings.Contains(command, " --add-dir ") {
		t.Fatalf("host command = %q", command)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.AdapterID != "claude" {
		t.Fatalf("host adapter id = %q", hostSession.AdapterID)
	}
	if !strings.Contains(fw.sentCalls[0].text, "Active adapter: claude") {
		t.Fatalf("bootstrap prompt did not include adapter metadata:\n%s", fw.sentCalls[0].text)
	}
}

func TestServiceResumeNativeThreadAsHostReplacesBrainHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("old-host", "codex"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetThreadPinned("codex:thread-1", true); err != nil {
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
  *'"method":"thread/resume"'*) ;;
  *) echo "bad request method: $req" >&2; exit 13 ;;
esac
case "$req" in
  *'"threadId":"thread-1"'*) ;;
  *) echo "bad thread id: $req" >&2; exit 14 ;;
esac
printf '%s\n' '{"id":"zen-1","result":{"thread":{"id":"thread-1","sessionId":"session-1","forkedFromId":null,"preview":"Resumed thread","ephemeral":false,"modelProvider":"openai","createdAt":1780331643,"updatedAt":1780333776,"status":{"type":"idle"},"path":"/repo/zen/.codex/thread.json","cwd":"/repo/zen","source":"cli","name":"Brain thread"}}}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			"old-host": {
				ID:      "old-host",
				Name:    "Brain",
				State:   classifier.StateRunning,
				Cwd:     store.WorkspacePath(),
				Command: "fake-codex --no-alt-screen -C '" + store.WorkspacePath() + "'",
				Hidden:  true,
			},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		Default: "codex",
		ByName: map[string]work.Executor{
			"codex": {Name: "codex", Command: script, Kind: "codex"},
		},
	})

	snapshot, thread, err := service.ResumeNativeThreadAsHost(context.Background(), "codex", "codex:thread-1", work.NativeThreadResumeOptions{
		Cwd: "/repo/zen",
	})
	if err != nil {
		t.Fatal(err)
	}
	if thread.ID != "codex:thread-1" || thread.NativeID != "thread-1" || thread.Status != "idle" {
		t.Fatalf("thread = %+v", thread)
	}
	if !thread.Pinned {
		t.Fatalf("expected resumed thread to keep Brain pin metadata: %+v", thread)
	}
	if len(fw.killed) != 1 || fw.killed[0] != "old-host" {
		t.Fatalf("killed sessions = %#v", fw.killed)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created sessions = %#v", fw.created)
	}
	created := fw.created[0].opts
	if created.Name != "Brain" || !created.Hidden || !created.Detached {
		t.Fatalf("created host = %+v", created)
	}
	if !strings.Contains(created.Command, "resume 'thread-1'") || !strings.Contains(created.Command, "--no-alt-screen") {
		t.Fatalf("resume command = %q", created.Command)
	}
	if snapshot.ChatThreadID != "codex:thread-1" {
		t.Fatalf("chat thread = %q", snapshot.ChatThreadID)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != fw.created[0].id {
		t.Fatalf("host agent = %#v created=%#v", snapshot.HostAgent, fw.created[0])
	}
}

func TestServiceSetHostAdapterPersistsAndStartsSelectedHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		Default: "codex",
		ByName: map[string]work.Executor{
			"codex":  {Name: "codex", Command: "codex"},
			"claude": {Name: "claude", Command: "claude"},
		},
	})

	snapshot, err := service.SetHostAdapter("claude")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostAdapter == nil || snapshot.HostAdapter.ID != "claude" {
		t.Fatalf("host adapter = %#v", snapshot.HostAdapter)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID == "" {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	if len(fw.created) != 1 || !strings.HasPrefix(fw.created[0].opts.Command, "claude") {
		t.Fatalf("created = %#v", fw.created)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.AdapterID != "claude" || hostSession.ID != fw.created[0].id {
		t.Fatalf("host session = %+v", hostSession)
	}
}

func TestServiceNewChatReplacesHostAndStartsFreshThread(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldHostID := "old-host"
	oldThreadID := "thread-old"
	if err := store.SetHostSession(oldHostID, "claude"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{
		ThreadID:       oldThreadID,
		SessionIDs:     []string{oldHostID},
		LastTranscript: "old transcript",
		UpdatedAt:      time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendChatMessage(ChatMessage{
		ID:        "old-message",
		ThreadID:  oldThreadID,
		SessionID: oldHostID,
		Role:      "user",
		Body:      "keep the old chat",
		CreatedAt: time.Date(2026, 6, 2, 10, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}

	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldHostID: {
				ID:      oldHostID,
				Name:    "Brain",
				State:   classifier.StateRunning,
				Cwd:     store.WorkspacePath(),
				Command: "claude --add-dir '" + store.WorkspacePath() + "'",
				Hidden:  true,
			},
		},
	}
	service := NewService(store, fw, &work.ExecutorConfig{
		Default: "claude",
		ByName: map[string]work.Executor{
			"claude": {Name: "claude", Command: "claude", Kind: "claude", Runtime: work.AgentRuntimeTmux},
		},
	})

	snapshot, err := service.NewChat()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.killed) != 1 || fw.killed[0] != oldHostID {
		t.Fatalf("killed sessions = %#v", fw.killed)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created sessions = %#v", fw.created)
	}
	created := fw.created[0]
	if !created.opts.Hidden || !created.opts.Detached || created.opts.Name != "Brain" {
		t.Fatalf("created host = %+v", created.opts)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != created.id {
		t.Fatalf("host agent = %#v created=%#v", snapshot.HostAgent, created)
	}
	if snapshot.HostAdapter == nil || snapshot.HostAdapter.ID != "claude" {
		t.Fatalf("host adapter = %#v", snapshot.HostAdapter)
	}
	if snapshot.ChatThreadID == "" || snapshot.ChatThreadID == oldThreadID {
		t.Fatalf("chat thread = %q, old = %q", snapshot.ChatThreadID, oldThreadID)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID != created.id || hostSession.AdapterID != "claude" {
		t.Fatalf("host session = %+v", hostSession)
	}
	messages, err := store.ChatMessages(snapshot.ChatThreadID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 0 {
		t.Fatalf("new thread messages = %#v", messages)
	}
	rawMessages, err := os.ReadFile(store.messagesPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rawMessages), "keep the old chat") {
		t.Fatalf("old chat message was not preserved:\n%s", rawMessages)
	}
	if len(fw.sentCalls) != 1 || fw.sentCalls[0].sessionID != created.id {
		t.Fatalf("bootstrap sends = %#v", fw.sentCalls)
	}
}

func TestServiceAnnotatesAndSortsPinnedNativeThreads(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetThreadPinned("codex:thread-2", true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetThreadReviewState("codex:thread-3", "needs_review"); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)

	got := service.annotateNativeThreads([]work.NativeThread{
		{ID: "codex:thread-1", Title: "First"},
		{ID: "codex:thread-3", Title: "Third"},
		{ID: "codex:thread-2", Title: "Second"},
	})
	if len(got) != 3 {
		t.Fatalf("threads = %#v", got)
	}
	if got[0].ID != "codex:thread-2" || !got[0].Pinned {
		t.Fatalf("pinned thread was not promoted: %#v", got)
	}
	if got[1].ID != "codex:thread-3" || got[1].ReviewState != "needs_review" {
		t.Fatalf("review thread was not queued after pinned threads: %#v", got)
	}
	if got[2].ID != "codex:thread-1" {
		t.Fatalf("remaining unpinned thread order changed: %#v", got)
	}
}

func TestServiceSetHostAdapterRejectsUnknownAdapter(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(store, &fakeWatcher{}, &work.ExecutorConfig{
		Default: "codex",
		ByName: map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		},
	})

	if _, err := service.SetHostAdapter("claude"); err == nil {
		t.Fatal("expected unknown adapter error")
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
	if len(fw.killed) != 1 || fw.killed[0] != oldID {
		t.Fatalf("expected mismatched host to be killed, got %#v", fw.killed)
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

func TestServiceSnapshotAttentionIncludesVisibleAgentLoad(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetThreadReviewState("codex:thread-1", "needs_review"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		agents: []*classifier.Agent{
			{
				ID:    "running:@1",
				Name:  "Codex (running:@1)",
				State: classifier.StateRunning,
			},
			{
				ID:    "unknown:@1",
				Name:  "Claude (unknown:@1)",
				State: classifier.StateUnknown,
			},
			{
				ID:    "blocked:@1",
				Name:  "Codex (blocked:@1)",
				State: classifier.StateBlocked,
			},
			{
				ID:    "done:@1",
				Name:  "Codex (done:@1)",
				State: classifier.StateDone,
			},
			{
				ID:     "hidden:@1",
				Name:   "Brain (hidden:@1)",
				State:  classifier.StateRunning,
				Hidden: true,
			},
		},
	}
	service := NewService(store, fw, nil)

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Attention.ReviewQueue != 1 {
		t.Fatalf("review queue = %d, want 1", snapshot.Attention.ReviewQueue)
	}
	if snapshot.Attention.ActiveAgents != 2 || snapshot.Attention.BlockedAgents != 1 {
		t.Fatalf("attention load = %+v", snapshot.Attention)
	}
	if snapshot.Attention.InFlightAgents != 3 || snapshot.Attention.AvailableAgentSlots != 0 {
		t.Fatalf("attention slots = %+v", snapshot.Attention)
	}
	if snapshot.Attention.CanStartAgent || snapshot.Attention.BackpressureReason != "blocked_agents_need_attention" {
		t.Fatalf("backpressure = %+v", snapshot.Attention)
	}
	if snapshot.Attention.Pressure != "blocked" {
		t.Fatalf("pressure = %q, want blocked", snapshot.Attention.Pressure)
	}
	if len(snapshot.AttentionQueue) != 2 {
		t.Fatalf("attention queue = %#v", snapshot.AttentionQueue)
	}
	if snapshot.AttentionQueue[0].Kind != "blocked_agent" || snapshot.AttentionQueue[0].AgentID != "blocked:@1" {
		t.Fatalf("first queue item = %+v", snapshot.AttentionQueue[0])
	}
	if snapshot.AttentionQueue[1].Kind != "review_thread" || snapshot.AttentionQueue[1].ThreadID != "codex:thread-1" {
		t.Fatalf("second queue item = %+v", snapshot.AttentionQueue[1])
	}
	for _, agent := range snapshot.Agents {
		if agent.ID == "hidden:@1" || agent.Hidden {
			t.Fatalf("hidden agent leaked into visible load: %#v", snapshot.Agents)
		}
	}
}

func TestServiceSnapshotAttentionAppliesConfiguredInFlightLimit(t *testing.T) {
	t.Setenv("ZEN_BRAIN_MAX_IN_FLIGHT_AGENTS", "2")
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		agents: []*classifier.Agent{
			{
				ID:    "running:@1",
				Name:  "Codex (running:@1)",
				State: classifier.StateRunning,
			},
			{
				ID:    "running:@2",
				Name:  "Claude (running:@2)",
				State: classifier.StateRunning,
			},
		},
	}
	service := NewService(store, fw, nil)

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Attention.MaxInFlightAgents != 2 || snapshot.Attention.InFlightAgents != 2 {
		t.Fatalf("attention limit = %+v", snapshot.Attention)
	}
	if snapshot.Attention.CanStartAgent || snapshot.Attention.BackpressureReason != "active_agent_limit_reached" {
		t.Fatalf("backpressure = %+v", snapshot.Attention)
	}
	if snapshot.Attention.Pressure != "loaded" {
		t.Fatalf("pressure = %q, want loaded", snapshot.Attention.Pressure)
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
