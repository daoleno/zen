package brain

import (
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
		ID:        id,
		Name:      opts.Name + " (" + id + ")",
		Cwd:       opts.Cwd,
		Command:   opts.Command,
		State:     classifier.StateRunning,
		Summary:   "Session starting",
		Hidden:    opts.Hidden,
		Delegated: opts.Delegated && !opts.Hidden,
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

func (w *fakeWatcher) SendInputWhenReady(sessionID, _ string, text string) error {
	return w.SendInput(sessionID, text)
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
	if !strings.Contains(fw.created[0].opts.Command, claudePermissionBypassFlag) {
		t.Fatalf("default Brain command should bypass Claude permissions: %q", fw.created[0].opts.Command)
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
	command := fw.created[0].opts.Command
	if !strings.Contains(command, codexFullAuthorizationFlag) {
		t.Fatalf("codex Brain host should bypass approvals and sandbox: %q", command)
	}
	if strings.Count(command, codexFullAuthorizationFlag) != 1 {
		t.Fatalf("codex full authorization flag duplicated: %q", command)
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
	if !strings.HasPrefix(command, "claude") || !strings.Contains(command, claudePermissionBypassFlag) || !strings.Contains(command, " --add-dir ") {
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

func TestServiceBootstrapPromptDefaultsToAutonomousScheduling(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, &work.ExecutorConfig{
		Default: "codex",
		ByName: map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		},
	})

	if _, err := service.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if len(fw.sentCalls) != 1 {
		t.Fatalf("bootstrap sends = %#v", fw.sentCalls)
	}
	prompt := fw.sentCalls[0].text
	for _, want := range []string{
		"Brain is the user's scheduler",
		"proactively create or reuse a visible delegated agent session",
		"For a single larger task, prefer reusing the same delegated agent session",
		"Zen CLI quick reference",
		"only sessions with delegated=true are Brain-owned",
		"agent spawn -name",
		"agent capture -id",
		"agent send -id",
		"agent close -id",
		"Delegated agent lifecycle",
		"Never close, kill, rename, repurpose, or otherwise manage sessions whose agent list entry does not have delegated=true",
		"Keep orchestration principles in Markdown, prompts, and agent instructions",
		"Treat Heartbeat wake messages as compact actionable deltas",
		"consolidate options and a recommendation",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("bootstrap prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "Only create or ask for a visible delegated agent session when the user explicitly asks") {
		t.Fatalf("bootstrap prompt still requires explicit delegation:\n%s", prompt)
	}
}

func TestServiceHeartbeatWakesExistingHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			hostID: {
				ID:      hostID,
				Name:    "Brain",
				State:   classifier.StateRunning,
				Hidden:  true,
				Command: "codex",
			},
		},
	}
	service := NewService(store, fw, nil)

	woke, err := service.Heartbeat(HeartbeatEvent{
		Reason:   "agent_state_change",
		AgentID:  "worker:@2",
		Name:     "Worker (worker:@2)",
		Status:   "blocked",
		Summary:  "Needs user input",
		Cwd:      "/repo",
		OldState: "running",
		NewState: "blocked",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !woke {
		t.Fatal("heartbeat did not wake existing host")
	}
	if len(fw.created) != 0 {
		t.Fatalf("heartbeat should not create host sessions, got %#v", fw.created)
	}
	if len(fw.sentCalls) != 1 || fw.sentCalls[0].sessionID != hostID {
		t.Fatalf("heartbeat sends = %#v", fw.sentCalls)
	}
	for _, want := range []string{
		"Heartbeat wake:",
		"reason: agent_state_change",
		"agent_id: worker:@2",
		"status: blocked",
		"Inspect the changed session if useful",
	} {
		if !strings.Contains(fw.sentCalls[0].text, want) {
			t.Fatalf("heartbeat message missing %q:\n%s", want, fw.sentCalls[0].text)
		}
	}
}

func TestServiceHeartbeatNoopsWithoutHost(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{}
	service := NewService(store, fw, nil)

	woke, err := service.Heartbeat(HeartbeatEvent{
		Reason:  "agent_state_change",
		AgentID: "worker:@2",
		Status:  "done",
	})
	if err != nil {
		t.Fatal(err)
	}
	if woke {
		t.Fatal("heartbeat unexpectedly woke without a host")
	}
	if len(fw.created) != 0 || len(fw.sentCalls) != 0 {
		t.Fatalf("heartbeat side effects created=%#v sent=%#v", fw.created, fw.sentCalls)
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

func TestServiceSnapshotPreservesCodexHostWithoutFullAuthorization(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	oldID := "old-brain-host:@1"
	if err := store.SetHostSession(oldID, "codex"); err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldID: {
				ID:      oldID,
				Name:    "Brain (" + oldID + ")",
				Cwd:     store.WorkspacePath(),
				Command: "codex --no-alt-screen -C '" + store.WorkspacePath() + "'",
				State:   classifier.StateRunning,
				Hidden:  true,
			},
		},
	}
	fw.agents = append(fw.agents, fw.sessions[oldID])
	service := NewService(store, fw, &work.ExecutorConfig{
		Default: "codex",
		ByName: map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		},
	})

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(fw.killed) != 0 {
		t.Fatalf("expected existing host to be preserved, killed %#v", fw.killed)
	}
	if len(fw.created) != 0 {
		t.Fatalf("expected no replacement host, got %#v", fw.created)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID != oldID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID != oldID || hostSession.AdapterID != "codex" {
		t.Fatalf("host session = %+v", hostSession)
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
	instructions, err := os.ReadFile(filepath.Join(root, "workspace", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(instructions), "Brain is the user's scheduler") {
		t.Fatalf("workspace instructions do not describe scheduler behavior:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "For a single larger task, prefer reusing the same delegated agent session") {
		t.Fatalf("workspace instructions do not describe delegated session reuse:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Keep orchestration principles in Markdown, prompts, and agent instructions") {
		t.Fatalf("workspace instructions do not describe prompt-first orchestration:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Treat Heartbeat wake messages as compact actionable deltas") {
		t.Fatalf("workspace instructions do not describe heartbeat handling:\n%s", instructions)
	}
	for _, want := range []string{"zen agent list --json", "zen agent spawn -name", "zen agent capture -id", "zen agent send -id", "zen agent close -id"} {
		if !strings.Contains(string(instructions), want) {
			t.Fatalf("workspace instructions missing %q:\n%s", want, instructions)
		}
	}
	if !strings.Contains(string(instructions), "Keep delegated agent lifecycle ownership") {
		t.Fatalf("workspace instructions missing lifecycle ownership:\n%s", instructions)
	}
	if !strings.Contains(string(instructions), "Never close, kill, rename, repurpose, or otherwise manage sessions whose agent list entry does not have delegated=true") {
		t.Fatalf("workspace instructions missing external session guard:\n%s", instructions)
	}
	if strings.Contains(string(instructions), "only when the user asks Brain to delegate real work") {
		t.Fatalf("workspace instructions still require explicit delegation:\n%s", instructions)
	}
}

func TestStoreUpgradesStaleWorkspaceInstructions(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	staleInstructions := `# Brain Workspace

Custom local note.

- Only create or ask for a visible delegated agent session when the user explicitly asks you to delegate real work.
`
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte(staleInstructions), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	instructions := string(raw)
	if !strings.Contains(instructions, "Custom local note.") {
		t.Fatalf("workspace instructions lost existing content:\n%s", instructions)
	}
	if strings.Contains(instructions, "explicitly asks you to delegate real work") {
		t.Fatalf("workspace instructions still contain stale explicit-only rule:\n%s", instructions)
	}
	for _, want := range currentWorkspaceInstructionMarkers {
		if !strings.Contains(instructions, want) {
			t.Fatalf("workspace instructions missing %q:\n%s", want, instructions)
		}
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
