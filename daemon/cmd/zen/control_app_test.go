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
	agents    map[string]*classifier.Agent
	created   []watcher.CreateSessionOptions
	sent      []fakeControlSend
	killed    []string
	captures  map[string]string
	progress  []fakeControlProgress
	sendErr   error
	ready     []fakeControlSend
	submitted []fakeControlSend
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
		Delegated: opts.Delegated && !opts.Hidden,
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

func (w *fakeControlWatcher) SettleAgentInputAccepted(id string, handoffStartedAt time.Time, phase, summary string) (*classifier.Agent, error) {
	agent := w.agents[id]
	if agent == nil {
		return nil, os.ErrNotExist
	}
	if agent.LastProgressAt == nil || agent.LastProgressAt.Before(handoffStartedAt) {
		agent.State = classifier.StateRunning
		agent.Summary = summary
		agent.Phase = phase
		agent.Attention = "none"
		agent.NeedsAttention = false
		agent.TaskClass = ""
		agent.EventKind = ""
		agent.DetailsJSON = ""
		agent.LastProgressAt = nil
		agent.ExpectedNextCheckAt = nil
		agent.LeaseSeconds = 0
	}
	cp := *agent
	return &cp, nil
}

func (w *fakeControlWatcher) SendInput(sessionID, text string) error {
	w.sent = append(w.sent, fakeControlSend{id: sessionID, text: text})
	return w.sendErr
}

func (w *fakeControlWatcher) SendInputWhenReady(sessionID, _ string, text string) error {
	w.ready = append(w.ready, fakeControlSend{id: sessionID, text: text})
	return w.SendInput(sessionID, text)
}

func (w *fakeControlWatcher) SubmitInputWhenReady(sessionID, _ string, payload string) error {
	w.submitted = append(w.submitted, fakeControlSend{id: sessionID, text: payload})
	return w.SendInput(sessionID, payload)
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
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex --no-alt-screen"},
		}),
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
	if created.Name != "Franklin" || created.Cwd != "/repo/zen" {
		t.Fatalf("create options = %+v", created)
	}
	if got, want := created.Command, "codex --no-alt-screen --dangerously-bypass-approvals-and-sandbox"; got != want {
		t.Fatalf("delegated codex command = %q, want %q", got, want)
	}
	if !created.Detached || created.Hidden {
		t.Fatalf("create visibility options = %+v", created)
	}
	if !created.ProgressEnv {
		t.Fatalf("progress env was not enabled: %+v", created)
	}
	progressBin, ok := created.Env["ZEN_AGENT_PROGRESS_CMD"]
	if !ok || progressBin == "" {
		t.Fatalf("ZEN_AGENT_PROGRESS_CMD not set: %#v", created.Env)
	}
	if progressBin == "zen agent progress" {
		t.Fatalf("ZEN_AGENT_PROGRESS_CMD must not be the legacy space-separated command, got %q", progressBin)
	}
	// The value must be the current executable's path, not a stale "zen"
	// resolved via PATH (this guards dev daemons launched as zen-dev).
	if exe, err := os.Executable(); err == nil {
		if exe = strings.TrimSpace(exe); exe != "" && progressBin != exe {
			t.Fatalf("ZEN_AGENT_PROGRESS_CMD = %q, want current executable %q", progressBin, exe)
		}
	}
	if created.Env["ZEN_STATE_DIR"] != "/tmp/zen state" {
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
		`"$ZEN_AGENT_PROGRESS_CMD" agent progress --status running --phase working --attention none --summary "Short current work" --lease 300`,
		`"$ZEN_AGENT_PROGRESS_CMD" agent progress --status running --phase planning --attention none --task-class lasting_design --event-kind invariant`,
		"loop contract",
		"core invariants",
		"Respect Zen's resource boundary",
		"TMPDIR/TMP/TEMP",
		"$ZEN_BUILD_TMPDIR",
		"Never hard-code OS-global temp paths",
		`event-kind "needs_judgment"`,
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

func TestControlAppAgentSpawnFromBrainDefaultsToDelegatedExecutor(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("brain-agent-brain-hidden:@1", "claude"); err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs: work.NewExecutorConfig("grok", map[string]work.Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
			"grok":   {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{
		Type:    "agent_spawn",
		AgentID: "brain-agent-brain-hidden:@1",
		Name:    "Research",
		Cwd:     "/repo/zen",
		Prompt:  "look around",
	})

	if !resp.OK || resp.Agent == nil {
		t.Fatalf("response = %#v", resp)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created = %#v", fw.created)
	}
	if got := fw.created[0].Command; got != "grok --no-alt-screen --permission-mode bypassPermissions" {
		t.Fatalf("command = %q", got)
	}

	explicit := app.HandleControlRequest(control.Request{
		Type:     "agent_spawn",
		AgentID:  "brain-agent-brain-hidden:@1",
		Executor: "codex",
		Name:     "Patch",
		Cwd:      "/repo/zen",
		Prompt:   "make the scoped patch",
	})
	if !explicit.OK || explicit.Agent == nil {
		t.Fatalf("explicit response = %#v", explicit)
	}
	if got := fw.created[1].Command; got != "codex --dangerously-bypass-approvals-and-sandbox" {
		t.Fatalf("explicit command = %q", got)
	}

	regular := app.HandleControlRequest(control.Request{
		Type:   "agent_spawn",
		Name:   "General",
		Cwd:    "/repo/zen",
		Prompt: "general spawn",
	})
	if !regular.OK || regular.Agent == nil {
		t.Fatalf("regular response = %#v", regular)
	}
	if got := fw.created[2].Command; got != "grok --no-alt-screen --permission-mode bypassPermissions" {
		t.Fatalf("regular command = %q", got)
	}
}

func TestControlAppAgentSpawnFromBrainUsesDelegatedExecutorNotHost(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("brain-agent-brain-hidden:@1", "codex"); err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs: work.NewExecutorConfig("agent", map[string]work.Executor{
			"agent": {Name: "agent", Command: "cursor-agent --force --sandbox disabled", Kind: "cursor"},
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	brainSpawn := app.HandleControlRequest(control.Request{
		Type:    "agent_spawn",
		AgentID: "brain-agent-brain-hidden:@1",
		Name:    "Brain Delegated",
		Cwd:     "/repo/zen",
		Prompt:  "use delegated executor",
	})
	if !brainSpawn.OK || brainSpawn.Agent == nil {
		t.Fatalf("brain spawn response = %#v", brainSpawn)
	}
	if got := fw.created[0].Command; got != "cursor-agent --force --sandbox disabled" {
		t.Fatalf("brain delegated command = %q", got)
	}

	regular := app.HandleControlRequest(control.Request{
		Type:   "agent_spawn",
		Name:   "General",
		Cwd:    "/repo/zen",
		Prompt: "use delegated executor",
	})
	if !regular.OK || regular.Agent == nil {
		t.Fatalf("regular response = %#v", regular)
	}
	if got := fw.created[1].Command; got != "cursor-agent --force --sandbox disabled" {
		t.Fatalf("regular command = %q", got)
	}
}

func TestControlAppAgentSpawnFromBrainHonorsDelegatedExecutorEnvOverride(t *testing.T) {
	t.Setenv("ZEN_DELEGATED_EXECUTOR", "codex")
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetHostSession("brain-agent-brain-hidden:@1", "claude"); err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs: work.NewExecutorConfig("grok", map[string]work.Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
			"grok":   {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{
		Type:    "agent_spawn",
		AgentID: "brain-agent-brain-hidden:@1",
		Name:    "Env Delegated",
		Cwd:     "/repo/zen",
		Prompt:  "use env override",
	})

	if !resp.OK || resp.Agent == nil {
		t.Fatalf("response = %#v", resp)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created = %#v", fw.created)
	}
	if got := fw.created[0].Command; got != "codex --dangerously-bypass-approvals-and-sandbox" {
		t.Fatalf("command = %q", got)
	}
}

func TestControlAppAgentSpawnHardensDelegatedCodexAndPreservesOverrides(t *testing.T) {
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher: fw,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex":  {Name: "codex", Command: "codex"},
			"claude": {Name: "claude", Command: "claude"},
		}),
	}

	// Delegated Codex spawn is hardened so internal progress commands do not
	// block on approval prompts.
	codexResp := app.HandleControlRequest(control.Request{
		Type:     "agent_spawn",
		Executor: "codex",
		Name:     "Codex Worker",
		Cwd:      "/repo/zen",
		Prompt:   "run progress",
	})
	if !codexResp.OK || codexResp.Agent == nil {
		t.Fatalf("codex spawn response = %#v", codexResp)
	}
	if got := fw.created[0].Command; got != "codex --dangerously-bypass-approvals-and-sandbox" {
		t.Fatalf("delegated codex command = %q, want hardened", got)
	}

	// An explicit full-command override is user-authored and must be returned
	// verbatim, even if it deliberately chooses a less permissive sandbox.
	overrideResp := app.HandleControlRequest(control.Request{
		Type:    "agent_spawn",
		Command: "codex -s read-only -a on-request",
		Name:    "Pinned Codex",
		Cwd:     "/repo/zen",
		Prompt:  "stay restricted",
	})
	if !overrideResp.OK || overrideResp.Agent == nil {
		t.Fatalf("override response = %#v", overrideResp)
	}
	if got := fw.created[1].Command; got != "codex -s read-only -a on-request" {
		t.Fatalf("explicit command override mutated = %q", got)
	}

	// Claude delegated spawn is also hardened so internal progress commands do not
	// block on approval prompts.
	claudeResp := app.HandleControlRequest(control.Request{
		Type:     "agent_spawn",
		Executor: "claude",
		Name:     "Claude Worker",
		Cwd:      "/repo/zen",
		Prompt:   "use claude",
	})
	if !claudeResp.OK || claudeResp.Agent == nil {
		t.Fatalf("claude spawn response = %#v", claudeResp)
	}
	if got := fw.created[2].Command; got != "claude --permission-mode bypassPermissions" {
		t.Fatalf("delegated claude command = %q, want hardened", got)
	}

	// Explicit Claude command override is preserved.
	claudeOverrideResp := app.HandleControlRequest(control.Request{
		Type:    "agent_spawn",
		Command: "claude --permission-mode dontAsk",
		Name:    "Pinned Claude",
		Cwd:     "/repo/zen",
		Prompt:  "manual mode",
	})
	if !claudeOverrideResp.OK || claudeOverrideResp.Agent == nil {
		t.Fatalf("claude override response = %#v", claudeOverrideResp)
	}
	if got := fw.created[3].Command; got != "claude --permission-mode dontAsk" {
		t.Fatalf("explicit claude override mutated = %q", got)
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
	fw.agents["brain-agent-worker:@1"] = &classifier.Agent{
		ID:        "brain-agent-worker:@1",
		Name:      "Franklin",
		State:     classifier.StateRunning,
		Command:   "codex --no-alt-screen",
		Delegated: true,
	}
	fw.captures["brain-agent-worker:@1"] = "current pane"
	app := &controlApp{watcher: fw}

	sendResp := app.HandleControlRequest(control.Request{
		Type:    "agent_send",
		AgentID: "brain-agent-worker:@1",
		Text:    "continue",
		Submit:  true,
	})
	if !sendResp.OK {
		t.Fatalf("send response = %#v", sendResp)
	}
	if len(fw.sent) != 1 || fw.sent[0].text != "continue" {
		t.Fatalf("sent calls = %#v", fw.sent)
	}
	if len(fw.submitted) != 1 || fw.submitted[0].text != "continue" || len(fw.ready) != 0 {
		t.Fatalf("structured submits = %#v ready sends = %#v", fw.submitted, fw.ready)
	}

	captureResp := app.HandleControlRequest(control.Request{Type: "agent_capture", AgentID: "brain-agent-worker:@1"})
	if !captureResp.OK || captureResp.Text != "current pane" || captureResp.Agent == nil {
		t.Fatalf("capture response = %#v", captureResp)
	}
}

func TestControlAppAgentSendAllowsSubmitOnlyEnter(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.agents["brain-agent-worker:@1"] = &classifier.Agent{
		ID:        "brain-agent-worker:@1",
		Name:      "Franklin",
		State:     classifier.StateRunning,
		Delegated: true,
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{
		Type:    "agent_send",
		AgentID: "brain-agent-worker:@1",
		Submit:  true,
	})

	if !resp.OK {
		t.Fatalf("send response = %#v", resp)
	}
	if len(fw.sent) != 1 || fw.sent[0].text != "\n" {
		t.Fatalf("sent calls = %#v", fw.sent)
	}
}

func TestControlAppAgentSendPreservesNonCodexSubmitPath(t *testing.T) {
	for _, command := range []string{"cursor-agent --force", "claude", "grok", "custom-agent --interactive"} {
		t.Run(command, func(t *testing.T) {
			fw := newFakeControlWatcher()
			fw.agents["brain-agent-worker:@1"] = &classifier.Agent{
				ID:        "brain-agent-worker:@1",
				State:     classifier.StateRunning,
				Command:   command,
				Delegated: true,
			}
			app := &controlApp{watcher: fw}
			resp := app.HandleControlRequest(control.Request{
				Type:    "agent_send",
				AgentID: "brain-agent-worker:@1",
				Text:    "provider follow-up",
				Submit:  true,
			})
			if !resp.OK {
				t.Fatalf("response = %#v", resp)
			}
			if len(fw.sent) != 1 || len(fw.ready) != 0 {
				t.Fatalf("sent=%#v ready=%#v; non-Codex path changed", fw.sent, fw.ready)
			}
		})
	}
}

func TestControlAppAgentSendFailureMarksAgentFailedAttention(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.sendErr = os.ErrDeadlineExceeded
	fw.agents["brain-agent-worker:@1"] = &classifier.Agent{
		ID:        "brain-agent-worker:@1",
		Name:      "Franklin",
		State:     classifier.StateRunning,
		Command:   "codex --no-alt-screen",
		Delegated: true,
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{
		Type:    "agent_send",
		AgentID: "brain-agent-worker:@1",
		Text:    "continue safely",
		Submit:  true,
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != "send_failed" {
		t.Fatalf("response = %#v", resp)
	}
	agent := fw.agents["brain-agent-worker:@1"]
	if agent.State != classifier.StateFailed || agent.Attention != "failed" || !agent.NeedsAttention {
		t.Fatalf("agent after failed submission = %#v", agent)
	}
	if len(fw.submitted) != 1 || len(fw.ready) != 0 {
		t.Fatalf("structured submits = %#v ready sends = %#v", fw.submitted, fw.ready)
	}
}

func TestControlAppConfirmedCodexSendClearsStickyLaunchFailure(t *testing.T) {
	fw := newFakeControlWatcher()
	failedAt := time.Date(2026, 6, 8, 8, 59, 0, 0, time.UTC)
	fw.agents["brain-agent-worker:@1"] = &classifier.Agent{
		ID:             "brain-agent-worker:@1",
		Name:           "Franklin",
		State:          classifier.StateFailed,
		Summary:        "Initial delegated prompt was not submitted: Codex composer did not become ready within 30s",
		Phase:          "starting",
		Attention:      "failed",
		NeedsAttention: true,
		LastProgressAt: &failedAt,
		Command:        "codex --no-alt-screen",
		Delegated:      true,
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{
		Type:    "agent_send",
		AgentID: "brain-agent-worker:@1",
		Text:    "the exact initial delegated prompt",
		Submit:  true,
	})

	if !resp.OK || resp.Agent == nil {
		t.Fatalf("response = %#v", resp)
	}
	agent := fw.agents["brain-agent-worker:@1"]
	if agent.State != classifier.StateRunning || agent.Attention != "none" || agent.NeedsAttention {
		t.Fatalf("agent after confirmed recovery send = %#v", agent)
	}
	if strings.Contains(agent.Summary, "not submitted") {
		t.Fatalf("sticky launch failure survived confirmed provider acceptance: %#v", agent)
	}
	if len(fw.submitted) != 1 || len(fw.ready) != 0 || len(fw.sent) != 1 {
		t.Fatalf("submitted=%#v ready=%#v sent=%#v, want one handoff transaction", fw.submitted, fw.ready, fw.sent)
	}
}

func TestControlAppStructuredCodexSubmitPreservesFinalLineEnding(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.agents["brain-agent-worker:@1"] = &classifier.Agent{
		ID:        "brain-agent-worker:@1",
		State:     classifier.StateRunning,
		Command:   "codex --no-alt-screen",
		Delegated: true,
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{
		Type:    "agent_send",
		AgentID: "brain-agent-worker:@1",
		Text:    "alpha\r\nβ\n",
		Submit:  true,
	})
	if !resp.OK {
		t.Fatalf("response = %#v", resp)
	}
	if len(fw.submitted) != 1 || fw.submitted[0].text != "alpha\r\nβ\n" {
		t.Fatalf("structured payload = %#v", fw.submitted)
	}
}

func TestControlAppAgentSpawnSubmissionFailureReturnsErrorAndAttention(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.sendErr = os.ErrDeadlineExceeded
	app := &controlApp{
		watcher: fw,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{
		Type:   "agent_spawn",
		Name:   "Unsubmitted",
		Cwd:    "/repo/zen",
		Prompt: "must execute",
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != "send_prompt_failed" {
		t.Fatalf("response = %#v", resp)
	}
	agent := fw.agents["brain-agent-unsubmitted:@1"]
	if agent == nil || agent.State != classifier.StateFailed || agent.Attention != "failed" || !agent.NeedsAttention {
		t.Fatalf("agent after failed initial prompt = %#v", agent)
	}
}

func TestControlAppAgentSendRejectsExternalSessionWithoutForce(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.agents["brain-agent-user-owned:@1"] = &classifier.Agent{
		ID:        "brain-agent-user-owned:@1",
		Name:      "User owned",
		State:     classifier.StateRunning,
		Delegated: false,
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{
		Type:    "agent_send",
		AgentID: "brain-agent-user-owned:@1",
		Text:    "continue",
		Submit:  true,
	})

	if resp.OK || resp.Error == nil || resp.Error.Code != "agent_not_delegated" {
		t.Fatalf("send response = %#v", resp)
	}
	if len(fw.sent) != 0 {
		t.Fatalf("sent input = %#v", fw.sent)
	}

	forced := app.HandleControlRequest(control.Request{
		Type:    "agent_send",
		AgentID: "brain-agent-user-owned:@1",
		Text:    "continue",
		Submit:  true,
		Force:   true,
	})
	if !forced.OK || len(fw.sent) != 1 {
		t.Fatalf("forced send response = %#v sent=%#v", forced, fw.sent)
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
		TaskClass:           "lasting_design",
		EventKind:           "invariant",
		DetailsJSON:         `{"invariants":["durable state is canonical"]}`,
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
	if resp.Agent.TaskClass != "lasting_design" || resp.Agent.EventKind != "invariant" {
		t.Fatalf("agent semantic progress fields = %#v", resp.Agent)
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
		TaskClass:    "lasting_design",
		EventKind:    "needs_judgment",
		DetailsJSON:  `{"question":"choose root model"}`,
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
	if resp.Agent.TaskClass != "lasting_design" || resp.Agent.EventKind != "needs_judgment" || resp.Agent.DetailsJSON == "" {
		t.Fatalf("agent semantic metadata = %#v", resp.Agent)
	}
	if resp.Agent.LastProgressAt == nil || resp.Agent.ExpectedNextCheckAt == nil {
		t.Fatalf("progress timestamps missing: %#v", resp.Agent)
	}
	if len(fw.progress) != 1 || fw.progress[0].id != "brain-agent-worker:@1" {
		t.Fatalf("progress calls = %#v", fw.progress)
	}
	if fw.progress[0].progress.TaskClass != "lasting_design" || fw.progress[0].progress.EventKind != "needs_judgment" {
		t.Fatalf("watcher progress semantic fields = %#v", fw.progress[0].progress)
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

func TestControlAppAgentCloseRejectsExternalSessionWithoutForce(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.agents["brain-agent-user-owned:@1"] = &classifier.Agent{
		ID:        "brain-agent-user-owned:@1",
		Name:      "User owned",
		State:     classifier.StateDone,
		Delegated: false,
	}
	app := &controlApp{watcher: fw}

	resp := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: "brain-agent-user-owned:@1"})

	if resp.OK || resp.Error == nil || resp.Error.Code != "agent_not_delegated" {
		t.Fatalf("close response = %#v", resp)
	}
	if len(fw.killed) != 0 {
		t.Fatalf("killed sessions = %#v", fw.killed)
	}

	forced := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: "brain-agent-user-owned:@1", Force: true})
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

func TestControlAppBrainContextReturnsStructuredContext(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(store.WorkspacePath(), "current.md"), []byte("# Current Brain Context\n\n## Active Objective\n\nShip context.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(brain.ChatState{
		ThreadID: "thread-main",
	}); err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		brainStore: store,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_context"})

	if !resp.OK || resp.Context == nil {
		t.Fatalf("response = %#v", resp)
	}
	context, ok := resp.Context.(brain.BrainContext)
	if !ok {
		t.Fatalf("context type = %T", resp.Context)
	}
	if context.ThreadID != "thread-main" || !strings.Contains(context.Current, "Ship context.") {
		t.Fatalf("context = %#v", context)
	}
	if len(context.Playbooks) != 5 {
		t.Fatalf("playbooks = %#v", context.Playbooks)
	}
}

func TestControlAppBrainPlaybooksReturnsCatalog(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{brainStore: store}

	resp := app.HandleControlRequest(control.Request{Type: "brain_playbooks"})

	if !resp.OK || resp.Playbooks == nil {
		t.Fatalf("response = %#v", resp)
	}
	catalog, ok := resp.Playbooks.(brain.PlaybookCatalog)
	if !ok {
		t.Fatalf("playbooks type = %T", resp.Playbooks)
	}
	if len(catalog.Playbooks) != 5 {
		t.Fatalf("catalog = %#v", catalog)
	}
	foundAlign := false
	for _, entry := range catalog.Playbooks {
		if entry.Name == "align" && strings.Contains(entry.Description, "one question") {
			foundAlign = true
		}
	}
	if !foundAlign {
		t.Fatalf("catalog missing align entry: %#v", catalog.Playbooks)
	}
}

func TestControlAppBrainGCReturnsHousekeepingReport(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(store.WorkspacePath(), "current.md")); err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		brainStore: store,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_gc"})

	if !resp.OK || resp.Housekeeping == nil {
		t.Fatalf("response = %#v", resp)
	}
	report, ok := resp.Housekeeping.(brain.HousekeepingReport)
	if !ok {
		t.Fatalf("housekeeping type = %T", resp.Housekeeping)
	}
	if report.CurrentPath != "current.md" || len(report.ChangedPaths) != 1 ||
		report.ChangedPaths[0] != "current.md" {
		t.Fatalf("housekeeping report = %#v", report)
	}
	if _, err := os.Stat(filepath.Join(store.WorkspacePath(), "current.md")); err != nil {
		t.Fatalf("brain gc did not repair current.md: %v", err)
	}
}

func TestControlAppBrainExecutorsListCodexHostFallback(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		brainStore: store,
		execs: work.NewExecutorConfig("claude", map[string]work.Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_executors"})

	if !resp.OK || resp.Executor == nil || resp.Executor.ID != "codex" {
		t.Fatalf("response = %#v", resp)
	}
	if len(resp.Executors) != 2 || resp.Executors[0].ID != "codex" || !resp.Executors[0].Host {
		t.Fatalf("executors = %#v", resp.Executors)
	}
	if resp.DelegatedExecutor == nil || resp.DelegatedExecutor.ID != "claude" {
		t.Fatalf("delegated executor = %#v", resp.DelegatedExecutor)
	}
	if !resp.Executor.Capabilities.InteractiveTTY {
		t.Fatalf("codex capabilities = %+v", resp.Executor.Capabilities)
	}
}

func TestControlAppBrainExecutorsFallbackToCodexNotGeneralDefault(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		brainStore: store,
		execs: work.NewExecutorConfig("agent", map[string]work.Executor{
			"agent": {Name: "agent", Command: "cursor-agent --force --sandbox disabled", Kind: "cursor"},
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_executors"})

	if !resp.OK || resp.Executor == nil || resp.Executor.ID != "codex" {
		t.Fatalf("response = %#v", resp)
	}
	if len(resp.Executors) != 2 || resp.Executors[0].ID != "codex" || !resp.Executors[0].Host {
		t.Fatalf("executors = %#v", resp.Executors)
	}
	if resp.DelegatedExecutor == nil || resp.DelegatedExecutor.ID != "agent" {
		t.Fatalf("delegated executor = %#v", resp.DelegatedExecutor)
	}
}

func TestControlAppSetDelegatedExecutorSameProcessSwitch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "executors.toml")
	if err := os.WriteFile(path, []byte(`
delegated_executor = "codex"

[[executors]]
name = "codex"
command = "codex"

[[executors]]
name = "grok"
command = "grok --flag"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	execs, err := work.LoadExecutors(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := brain.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		watcher:    newFakeControlWatcher(),
		brainStore: store,
		execs:      execs,
	}

	before := app.HandleControlRequest(control.Request{Type: "brain_executors"})
	if !before.OK || before.DelegatedExecutor == nil || before.DelegatedExecutor.ID != "codex" {
		t.Fatalf("before = %#v", before)
	}

	resp := app.HandleControlRequest(control.Request{Type: "set_delegated_executor", ExecutorID: "grok"})
	if !resp.OK || resp.DelegatedExecutor == nil || resp.DelegatedExecutor.ID != "grok" {
		t.Fatalf("set response = %#v", resp)
	}
	if execs.GetDelegatedExecutor() != "grok" {
		t.Fatalf("live owner = %q", execs.GetDelegatedExecutor())
	}

	spawn := app.HandleControlRequest(control.Request{
		Type:   "agent_spawn",
		Cwd:    dir,
		Name:   "delegated-after-switch",
		Prompt: "use live delegated selection",
	})
	if !spawn.OK || spawn.Agent == nil {
		t.Fatalf("spawn = %#v", spawn)
	}
	if !strings.Contains(spawn.Agent.Command, "grok") {
		t.Fatalf("spawn command = %q, want grok", spawn.Agent.Command)
	}
}

func TestControlAppBrainSetExecutorPersistsConfiguredExecutor(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		brainStore: store,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_set_executor", ExecutorID: "claude"})

	if !resp.OK || resp.Executor == nil || resp.Executor.ID != "claude" {
		t.Fatalf("response = %#v", resp)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ExecutorID != "claude" {
		t.Fatalf("host executor id = %q", hostSession.ExecutorID)
	}
	if len(resp.Executors) != 2 || resp.Executors[0].ID != "claude" || !resp.Executors[0].Host {
		t.Fatalf("executors = %#v", resp.Executors)
	}
}

func TestControlAppBrainSetExecutorStartsSelectedHostWhenWatcherAvailable(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"claude": {Name: "claude", Command: "claude"},
			"codex":  {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_set_executor", ExecutorID: "claude"})

	if !resp.OK || resp.Executor == nil || resp.Executor.ID != "claude" {
		t.Fatalf("response = %#v", resp)
	}
	if len(fw.created) != 1 {
		t.Fatalf("created sessions = %#v", fw.created)
	}
	if !fw.created[0].Hidden || !strings.HasPrefix(fw.created[0].Command, "claude") {
		t.Fatalf("created host = %+v", fw.created[0])
	}
	if len(fw.sent) != 1 || !strings.Contains(fw.sent[0].text, "Host executor: claude") {
		t.Fatalf("bootstrap prompt = %#v", fw.sent)
	}
	hostSession, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if hostSession.ID == "" || hostSession.ExecutorID != "claude" {
		t.Fatalf("host session = %+v", hostSession)
	}
}

func TestResolveSpawnCommandHardensDefaults(t *testing.T) {
	tests := []struct {
		name  string
		req   control.Request
		execs *work.ExecutorConfig
		want  string
	}{
		{
			name: "explicit command unchanged",
			req: control.Request{
				Command: "claude --permission-mode dontAsk --profile custom",
			},
			want: "claude --permission-mode dontAsk --profile custom",
		},
		{
			name: "bare default Claude gets hardened",
			req:  control.Request{},
			execs: work.NewExecutorConfig("claude", map[string]work.Executor{
				"claude": {Name: "claude", Command: "claude"},
			}),
			want: "claude --permission-mode bypassPermissions",
		},
		{
			name: "configured Claude default gets hardened",
			req:  control.Request{Executor: "claude"},
			execs: work.NewExecutorConfig("codex", map[string]work.Executor{
				"claude": {Name: "claude", Command: "claude --profile my-profile"},
			}),
			want: "claude --profile my-profile --permission-mode bypassPermissions",
		},
		{
			name: "explicit Claude permission mode preserved",
			req:  control.Request{Executor: "claude"},
			execs: work.NewExecutorConfig("codex", map[string]work.Executor{
				"claude": {Name: "claude", Command: "claude --permission-mode dontAsk"},
			}),
			want: "claude --permission-mode dontAsk",
		},
		{
			name: "explicit Claude dangerously-skip-permissions preserved",
			req:  control.Request{Executor: "claude"},
			execs: work.NewExecutorConfig("codex", map[string]work.Executor{
				"claude": {Name: "claude", Command: "claude --dangerously-skip-permissions"},
			}),
			want: "claude --dangerously-skip-permissions",
		},
		{
			name: "no-config Claude executor name gets hardened",
			req:  control.Request{Executor: "claude"},
			want: "claude --permission-mode bypassPermissions",
		},
		{
			name: "custom executor unchanged",
			req:  control.Request{Executor: "my-agent"},
			execs: work.NewExecutorConfig("codex", map[string]work.Executor{
				"my-agent": {Name: "my-agent", Command: "/usr/local/bin/my-agent --flag"},
			}),
			want: "/usr/local/bin/my-agent --flag",
		},
		{
			name: "Codex default hardened unchanged",
			req:  control.Request{},
			execs: work.NewExecutorConfig("codex", map[string]work.Executor{
				"codex": {Name: "codex", Command: "codex"},
			}),
			want: "codex --dangerously-bypass-approvals-and-sandbox",
		},
		{
			name: "non-Claude provider unchanged",
			req:  control.Request{Executor: "grok"},
			execs: work.NewExecutorConfig("codex", map[string]work.Executor{
				"grok": {Name: "grok", Command: "grok --no-alt-screen"},
			}),
			want: "grok --no-alt-screen",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			app := &controlApp{execs: tc.execs}
			got, err := app.resolveSpawnCommand(tc.req)
			if err != nil {
				t.Fatalf("resolveSpawnCommand() err = %v", err)
			}
			if got != tc.want {
				t.Fatalf("resolveSpawnCommand() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestControlAppBrainSetExecutorRejectsUnknownExecutor(t *testing.T) {
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := &controlApp{
		brainStore: store,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{Type: "brain_set_executor", ExecutorID: "claude"})

	if resp.OK || resp.Error == nil || resp.Error.Code != "invalid_executor" {
		t.Fatalf("response = %#v", resp)
	}
}
