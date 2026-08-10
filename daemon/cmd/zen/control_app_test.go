package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
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
	agents            map[string]*classifier.Agent
	created           []watcher.CreateSessionOptions
	sent              []fakeControlSend
	killed            []string
	captures          map[string]string
	receipts          map[string]string
	progress          []fakeControlProgress
	sendErr           error
	createErr         error
	killErr           error
	killLeavesLive    bool
	reportKillMissing bool
	probeErr          error
	probePresence     *watcher.SessionPresence
	dropAgentOnSend   bool
	ready             []fakeControlSend
	submitted         []fakeControlSend
	onCreate          func(string)
	turnStore         *brain.Store
	ownershipErr      error
	ownershipCalls    []string
}

type fakeControlSend struct {
	id   string
	text string
}

type fakeControlProgress struct {
	id       string
	progress classifier.AgentProgress
}

func assertDelegatedLifecyclePayload(t *testing.T, payload, original string) string {
	t.Helper()
	prefix := original + "\n\nZen delegated turn contract:\n- This prompt's turn identity is "
	if !strings.HasPrefix(payload, prefix) {
		t.Fatalf("delegated payload did not preserve original bytes or append the contract:\n%q", payload)
	}
	remainder := strings.TrimPrefix(payload, prefix)
	identity, _, found := strings.Cut(remainder, ".\n")
	if !found || !strings.HasPrefix(identity, "turn:") || strings.Count(payload, identity) < 3 {
		t.Fatalf("delegated payload lacks one repeated random turn identity: %q", payload)
	}
	if !strings.Contains(payload, "--turn-id "+identity) {
		t.Fatalf("delegated payload does not carry identity in command contract: %q", payload)
	}
	return identity
}

func newFakeControlWatcher() *fakeControlWatcher {
	return &fakeControlWatcher{
		agents:   map[string]*classifier.Agent{},
		captures: map[string]string{},
		receipts: map[string]string{},
	}
}

func newControlBrainStore(t *testing.T) *brain.Store {
	t.Helper()
	store, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func admitControlWorkOwner(t *testing.T, store *brain.Store, workID, sessionID string) string {
	t.Helper()
	acceptedAt := time.Now().UTC().Add(-time.Second)
	turnID := sessionID + ":turn:fixture"
	digest := strings.Repeat("a", 64)
	pending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: workID, SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: digest, ProcessIdentity: "process-identity", PaneGeneration: "pane-generation",
		AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created {
		t.Fatalf("prepare fixture owner=(%+v, %v, %v)", pending, created, err)
	}
	if _, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
		PayloadSHA256: digest, ActivityID: "activity-fixture",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "admission-fixture", Cursor: 1,
			SHA256: digest, At: acceptedAt.Add(time.Millisecond),
		},
		ResolvedAt: acceptedAt.Add(time.Millisecond),
	}); err != nil {
		t.Fatalf("resolve fixture owner: %v", err)
	}
	return turnID
}

func resolveControlHostClaim(t *testing.T, store *brain.Store, claimed brain.WorkEvent) {
	t.Helper()
	acceptedAt := time.Now().UTC()
	if claimed.ClaimedAt != nil {
		acceptedAt = claimed.ClaimedAt.UTC()
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("Host claim "+claimed.ID)))
	pending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: claimed.WorkID, SessionID: claimed.DeliveryHostSessionID,
		ProposedTurnID: claimed.ProviderTurnID, Receipt: claimed.ID, ClaimToken: claimed.HandlingID,
		PayloadSHA256: digest, ProcessIdentity: "host-process-identity", PaneGeneration: "host-pane-generation",
		AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created {
		t.Fatalf("prepare Host claim created=%v err=%v", created, err)
	}
	resolvedAt := acceptedAt.Add(time.Millisecond)
	if _, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: claimed.DeliveryHostSessionID, ProposedTurnID: claimed.ProviderTurnID,
		Receipt: claimed.ID, PayloadSHA256: pending.PayloadSHA256,
		ActivityID: "host-activity-" + claimed.ProviderTurnID,
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "host-admission-" + claimed.ProviderTurnID, Cursor: 1,
			SHA256: pending.PayloadSHA256, At: resolvedAt,
		},
		ResolvedAt: resolvedAt,
	}); err != nil {
		t.Fatalf("resolve Host claim: %v", err)
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
	presence, err := w.ProbeSession(target)
	return err == nil && presence == watcher.SessionPresencePresent
}

func (w *fakeControlWatcher) ProbeSession(target string) (watcher.SessionPresence, error) {
	if w.probeErr != nil {
		return watcher.SessionPresenceUnknown, w.probeErr
	}
	if w.probePresence != nil {
		return *w.probePresence, nil
	}
	if _, ok := w.agents[target]; ok {
		return watcher.SessionPresencePresent, nil
	}
	return watcher.SessionPresenceAbsent, nil
}

func (w *fakeControlWatcher) CreateSession(_ string, opts watcher.CreateSessionOptions) (string, error) {
	if w.createErr != nil {
		w.created = append(w.created, opts)
		return "", w.createErr
	}
	id := fmt.Sprintf(
		"brain-agent-%s:@%d",
		strings.ToLower(strings.ReplaceAll(opts.Name, " ", "-")),
		len(w.created)+1,
	)
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
	if w.onCreate != nil {
		w.onCreate(id)
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

func (w *fakeControlWatcher) RebindDelegatedTurnProjection(id string) (*classifier.Agent, error) {
	agent := w.agents[id]
	if agent == nil {
		return nil, os.ErrNotExist
	}
	agent.State = classifier.StateRunning
	agent.Summary = "Delegated turn running"
	agent.Attention = "none"
	agent.NeedsAttention = false
	agent.Phase = ""
	agent.TaskClass = ""
	agent.EventKind = ""
	agent.DetailsJSON = ""
	agent.LastProgressAt = nil
	agent.ExpectedNextCheckAt = nil
	agent.LeaseSeconds = 0
	cp := *agent
	return &cp, nil
}

func (w *fakeControlWatcher) RecordAgentInputDispatched(id, turnID string, handoffStartedAt time.Time, phase, summary string) (*classifier.Agent, error) {
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
	if w.dropAgentOnSend {
		delete(w.agents, sessionID)
	}
	return w.sendErr
}

func (w *fakeControlWatcher) SendInputWithReceiptResult(sessionID, text, receipt string) (watcher.InputResult, error) {
	if w.receipts[sessionID] == receipt {
		return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, nil
	}
	if err := w.SendInput(sessionID, text); err != nil {
		return watcher.InputResult{Outcome: watcher.InputOutcomeFromError(err), Receipt: receipt}, err
	}
	w.receipts[sessionID] = receipt
	return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, nil
}

func (w *fakeControlWatcher) InputReceiptResult(_ string, receipt string) (watcher.InputResult, bool, error) {
	for _, current := range w.receipts {
		if current == receipt {
			return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: receipt}, true, nil
		}
	}
	return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: receipt}, false, nil
}

func (w *fakeControlWatcher) SendInputWhenReady(sessionID, _ string, text string) error {
	w.ready = append(w.ready, fakeControlSend{id: sessionID, text: text})
	return w.SendInput(sessionID, text)
}

func (w *fakeControlWatcher) SubmitInputWhenReady(sessionID, _ string, payload string) error {
	w.submitted = append(w.submitted, fakeControlSend{id: sessionID, text: payload})
	return w.SendInput(sessionID, payload)
}

func (w *fakeControlWatcher) SubmitInput(sessionID, payload string) error {
	w.submitted = append(w.submitted, fakeControlSend{id: sessionID, text: payload})
	return w.SendInput(sessionID, payload)
}

func (w *fakeControlWatcher) SubmitDelegatedInput(
	sessionID, payload, turnID string,
	_ time.Time,
) (watcher.InputResult, error) {
	err := w.SubmitInput(sessionID, payload)
	return watcher.InputResult{
		Outcome: watcher.InputOutcomeFromError(err),
		Receipt: turnID,
	}, err
}

func (w *fakeControlWatcher) SubmitDelegatedInputWhenReady(
	sessionID, _ string, payload, workID, turnID string,
	acceptedAt time.Time,
) (watcher.InputResult, error) {
	if w.turnStore != nil {
		digest := strings.Repeat("b", 64)
		pending, created, prepareErr := w.turnStore.PrepareTurnSubmission(watcher.TurnSubmission{
			WorkID: workID, SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
			PayloadSHA256: digest, ProcessIdentity: "process-identity", PaneGeneration: "pane-generation",
			AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh, SignalProtocol: true,
		})
		if prepareErr != nil {
			result := watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: turnID, TurnID: turnID}
			return result, &watcher.InputSubmissionError{Result: result, Cause: prepareErr}
		}
		if !created {
			result := watcher.InputResult{Outcome: watcher.InputAmbiguous, Receipt: turnID, TurnID: turnID}
			return result, &watcher.InputSubmissionError{Result: result, Cause: errors.New("pending submission was not freshly prepared")}
		}
		if err := w.SubmitInputWhenReady(sessionID, "", payload); err != nil {
			outcome := watcher.InputOutcomeFromError(err)
			if outcome == watcher.InputNotSubmitted {
				if _, abortErr := w.turnStore.AbortTurnSubmission(sessionID, turnID, turnID, pending.PayloadSHA256); abortErr != nil {
					outcome = watcher.InputAmbiguous
					err = errors.Join(err, abortErr)
				}
			}
			result := watcher.InputResult{Outcome: outcome, Receipt: turnID, TurnID: turnID}
			return result, &watcher.InputSubmissionError{Result: result, Cause: err}
		}
		if _, err := w.turnStore.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
			SessionID: sessionID, ProposedTurnID: turnID, Receipt: turnID,
			PayloadSHA256: pending.PayloadSHA256, ActivityID: "activity-accepted",
			Admission: watcher.TurnAdmission{
				Stream: "provider", ID: "admission-accepted", Cursor: 1,
				SHA256: pending.PayloadSHA256, At: acceptedAt.Add(time.Millisecond),
			},
			ResolvedAt: acceptedAt.Add(time.Millisecond),
		}); err != nil {
			result := watcher.InputResult{Outcome: watcher.InputAmbiguous, Receipt: turnID, TurnID: turnID}
			return result, &watcher.InputSubmissionError{Result: result, Cause: err}
		}
		return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: turnID, TurnID: turnID}, nil
	}
	err := w.SubmitInputWhenReady(sessionID, "", payload)
	return watcher.InputResult{
		Outcome: watcher.InputOutcomeFromError(err),
		Receipt: turnID,
	}, err
}

func (w *fakeControlWatcher) SubmitBrainHostInput(
	sessionID, payload, eventID, claimToken, workID, providerTurnID string,
	acceptedAt time.Time,
) (watcher.InputResult, error) {
	if w.turnStore != nil {
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
		pending, created, err := w.turnStore.PrepareTurnSubmission(watcher.TurnSubmission{
			WorkID: workID, SessionID: sessionID, ProposedTurnID: providerTurnID,
			Receipt: eventID, ClaimToken: claimToken, PayloadSHA256: digest,
			ProcessIdentity: "host-process-identity", PaneGeneration: "host-pane-generation",
			AcceptedAt: acceptedAt.UTC(), Mode: watcher.TurnSubmissionFresh,
		})
		if err != nil {
			return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: eventID, TurnID: providerTurnID},
				err
		}
		if !created {
			return watcher.InputResult{Outcome: watcher.InputNotSubmitted, Receipt: eventID, TurnID: providerTurnID},
				errors.New("Host submission was not freshly prepared")
		}
		if err := w.SubmitInput(sessionID, payload); err != nil {
			return watcher.InputResult{Outcome: watcher.InputOutcomeFromError(err), Receipt: eventID, TurnID: providerTurnID}, err
		}
		resolvedAt := acceptedAt.Add(time.Millisecond).UTC()
		resolved, err := w.turnStore.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
			SessionID: sessionID, ProposedTurnID: providerTurnID, Receipt: eventID,
			PayloadSHA256: pending.PayloadSHA256, ActivityID: "host-activity-" + providerTurnID,
			Admission: watcher.TurnAdmission{
				Stream: "provider", ID: "host-admission-" + providerTurnID, Cursor: 1,
				SHA256: pending.PayloadSHA256, At: resolvedAt,
			},
			ResolvedAt: resolvedAt,
		})
		if err != nil {
			return watcher.InputResult{Outcome: watcher.InputAmbiguous, Receipt: eventID, TurnID: providerTurnID}, err
		}
		return watcher.InputResult{Outcome: watcher.InputAccepted, Receipt: eventID, TurnID: resolved.ResolvedTurnID}, nil
	}
	err := w.SubmitInput(sessionID, payload)
	return watcher.InputResult{
		Outcome: watcher.InputOutcomeFromError(err), Receipt: eventID, TurnID: providerTurnID,
	}, err
}

func (w *fakeControlWatcher) KillSession(sessionID string) error {
	w.killed = append(w.killed, sessionID)
	if w.killLeavesLive && w.killErr != nil {
		return w.killErr
	}
	_, existed := w.agents[sessionID]
	delete(w.agents, sessionID)
	if w.killErr != nil {
		return w.killErr
	}
	if !existed && w.reportKillMissing {
		// Production KillSession treats target-missing as idempotent success.
		return nil
	}
	return nil
}

func (w *fakeControlWatcher) CapturePaneContent(sessionID string) (string, error) {
	return w.captures[sessionID], nil
}

func (w *fakeControlWatcher) LegacyDelegatedTurnMarkers() []watcher.LegacyDelegatedTurnMarker {
	return nil
}

func (w *fakeControlWatcher) ClearDelegatedTurnMarkers([]string) {}

func (w *fakeControlWatcher) ProbeProviderEvidence(string) (watcher.ProviderActivityObservation, bool, error) {
	return watcher.ProviderActivityObservation{}, false, nil
}

func (w *fakeControlWatcher) ResolveOwnedGeneration(sessionID string) (watcher.OwnedGeneration, error) {
	w.ownershipCalls = append(w.ownershipCalls, sessionID)
	if w.ownershipErr != nil {
		if agent := w.agents[sessionID]; agent != nil {
			agent.State = classifier.StateUnknown
			agent.Attention = "ownership_lost"
			agent.NeedsAttention = true
		}
		return watcher.OwnedGeneration{}, w.ownershipErr
	}
	return watcher.OwnedGeneration{SessionID: sessionID, Generation: "owned-generation"}, nil
}

func (w *fakeControlWatcher) ResolveDelegatedControl(sessionID string) (watcher.OwnedGeneration, error) {
	return w.ResolveOwnedGeneration(sessionID)
}

func TestControlAppAgentSpawnCreatesVisibleDetachedSession(t *testing.T) {
	fw := newFakeControlWatcher()
	store := newControlBrainStore(t)
	fw.turnStore = store
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
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
	if resp.BrainWork == nil ||
		resp.BrainWork.OwnerSessionID != resp.Agent.ID ||
		resp.BrainWork.Status != brain.WorkRunning ||
		resp.BrainWork.CompletionPolicy != brain.CompletionBounded {
		t.Fatalf("spawn Work = %#v", resp.BrainWork)
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
		"Zen delegated turn contract:",
		"--turn-id turn:",
	} {
		if !strings.Contains(fw.sent[0].text, want) {
			t.Fatalf("prompt missing %q:\n%s", want, fw.sent[0].text)
		}
	}
	if strings.Contains(fw.sent[0].text, "[zen:"+"progress]") {
		t.Fatalf("prompt should not contain stdout marker protocol:\n%s", fw.sent[0].text)
	}
}

func TestControlAppAgentSpawnWorkCASPreservesIncumbentAndKillsOnlyLoser(t *testing.T) {
	store := newControlBrainStore(t)
	item, err := store.CreateWork(brain.Work{
		Title:            "Race-owned Work",
		Objective:        "Preserve the first delegated Session owner.",
		Status:           brain.WorkOpen,
		CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	incumbentID := "brain-agent-incumbent:@9"
	fw.agents[incumbentID] = &classifier.Agent{
		ID:        incumbentID,
		State:     classifier.StateRunning,
		Delegated: true,
	}
	fw.onCreate = func(string) {
		_ = admitControlWorkOwner(t, store, item.ID, incumbentID)
	}
	fw.turnStore = store
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{
		Type:   "agent_spawn",
		Name:   "Losing worker",
		Cwd:    "/repo/zen",
		Prompt: "must lose the owner CAS",
		WorkID: item.ID,
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "conflict" {
		t.Fatalf("losing spawn response=%#v", resp)
	}
	if len(fw.created) != 1 || len(fw.killed) != 1 {
		t.Fatalf("created=%#v killed=%#v", fw.created, fw.killed)
	}
	loserID := "brain-agent-losing-worker:@1"
	if fw.killed[0] != loserID {
		t.Fatalf("killed %q, want only new loser %q", fw.killed[0], loserID)
	}
	if fw.GetAgent(incumbentID) == nil {
		t.Fatal("incumbent Session was terminated")
	}
	got, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerSessionID != incumbentID || got.Status != brain.WorkRunning ||
		len(fw.sent) != 0 {
		t.Fatalf("incumbent Work=%#v sent=%#v", got, fw.sent)
	}

	alreadyOwned := app.HandleControlRequest(control.Request{
		Type:   "agent_spawn",
		Name:   "Rejected before creation",
		Cwd:    "/repo/zen",
		Prompt: "must not create another Session",
		WorkID: item.ID,
	})
	if alreadyOwned.OK || alreadyOwned.Error == nil || alreadyOwned.Error.Code != "conflict" ||
		len(fw.created) != 1 || len(fw.killed) != 1 {
		t.Fatalf(
			"pre-owned spawn=%#v created=%#v killed=%#v",
			alreadyOwned,
			fw.created,
			fw.killed,
		)
	}
}

func TestPrepareSpawnWorkAllowsNamedSuccessorOnlyDuringDeliveredHandling(t *testing.T) {
	store := newControlBrainStore(t)
	item, err := store.CreateWork(brain.Work{
		Title: "Review correction", Objective: "Attach a successor only through disposition.",
		Status: brain.WorkOpen, CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	incumbentTurnID := admitControlWorkOwner(t, store, item.ID, "brain-agent-incumbent:@1")
	app := &controlApp{brainStore: store}
	req := control.Request{WorkID: item.ID}
	if _, err := app.prepareSpawnWork(req, "Correction", "correct it"); !errors.Is(err, brain.ErrWorkOwnerConflict) {
		t.Fatalf("spawn outside handling err=%v, want owner conflict", err)
	}
	incumbentTurn, found, err := store.Turn("brain-agent-incumbent:@1")
	if err != nil || !found || incumbentTurn.TurnID != incumbentTurnID {
		t.Fatalf("canonical incumbent Turn=%+v found=%v err=%v", incumbentTurn, found, err)
	}
	acceptedAt := incumbentTurn.AcceptedAt
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: "brain-agent-incumbent:@1", TurnID: incumbentTurnID,
		Class: watcher.EvidenceProvider, Kind: "done", Bound: true,
		SourceID: "provider-incumbent-done", Admission: incumbentTurn.Admission,
		ActivityID: incumbentTurn.ActivityID, StartedAt: acceptedAt.Add(time.Second),
		SettledAt: acceptedAt.Add(2 * time.Second), At: acceptedAt.Add(2 * time.Second),
	}); err != nil || !changed {
		t.Fatalf("terminalize incumbent changed=%v err=%v", changed, err)
	}
	claimed, ok, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@1")
	if err != nil || !ok {
		t.Fatalf("claim=%+v ok=%v err=%v", claimed, ok, err)
	}
	resolveControlHostClaim(t, store, claimed)
	if _, _, err := store.ConsumeClaimedWorkEvent(claimed.ID, claimed.HandlingID, claimed.WorkID, claimed.DeliveryHostSessionID, claimed.ProviderTurnID); err != nil {
		t.Fatal(err)
	}
	if _, err := app.prepareSpawnWork(req, "Correction", "correct it"); err != nil {
		t.Fatalf("delivered handling rejected successor: %v", err)
	}
	if _, err := app.prepareSpawnWork(req, "Correction", ""); !errors.Is(err, brain.ErrWorkOwnerConflict) {
		t.Fatalf("promptless successor err=%v, want owner conflict", err)
	}
}

func TestControlAppAgentSpawnRequiresExplicitWorkingDirectory(t *testing.T) {
	app := &controlApp{watcher: newFakeControlWatcher()}

	resp := app.HandleControlRequest(control.Request{Type: "agent_spawn", Name: "Franklin"})

	if resp.OK || resp.Error == nil || resp.Error.Code != "missing_cwd" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestControlAppHiddenSpawnCreatesNoWork(t *testing.T) {
	store := newControlBrainStore(t)
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		}),
	}
	resp := app.HandleControlRequest(control.Request{
		Type:   "agent_spawn",
		Name:   "Hidden host",
		Cwd:    "/repo/zen",
		Prompt: "host only",
		Hidden: true,
	})
	if !resp.OK || resp.Agent == nil || !resp.Agent.Hidden || resp.BrainWork != nil {
		t.Fatalf("hidden spawn response = %#v", resp)
	}
	items, err := store.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("hidden spawn Work = %#v", items)
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
		watcher:    fw,
		brainStore: newControlBrainStore(t),
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

func TestControlAppOwnedSurfacesShareGenerationResolverAndDeprojectBeforeRejecting(t *testing.T) {
	sessionID := "brain-agent-ownership-loss:@1"
	for _, test := range []struct {
		name      string
		request   control.Request
		wantOK    bool
		wantError string
	}{
		{name: "list", request: control.Request{Type: "agent_list"}, wantOK: true},
		{name: "status", request: control.Request{Type: "agent_status", AgentID: sessionID}, wantOK: true},
		{name: "capture", request: control.Request{Type: "agent_capture", AgentID: sessionID}, wantError: "agent_ownership_lost"},
		{name: "follow-up", request: control.Request{Type: "agent_send", AgentID: sessionID, Text: "continue", Submit: true}, wantError: "agent_ownership_lost"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fw := newFakeControlWatcher()
			fw.agents[sessionID] = &classifier.Agent{
				ID: sessionID, State: classifier.StateRunning, Delegated: true,
				Command: "codex", Attention: "none",
			}
			fw.ownershipErr = errors.New("owned generation no longer matches canonical turn")
			app := &controlApp{watcher: fw}

			response := app.HandleControlRequest(test.request)
			if response.OK != test.wantOK {
				t.Fatalf("response = %#v, want ok=%v", response, test.wantOK)
			}
			if test.wantError != "" && (response.Error == nil || response.Error.Code != test.wantError) {
				t.Fatalf("response = %#v, want error %q", response, test.wantError)
			}
			if len(fw.ownershipCalls) != 1 || fw.ownershipCalls[0] != sessionID {
				t.Fatalf("owned-generation resolutions = %#v", fw.ownershipCalls)
			}
			projected := fw.agents[sessionID]
			if projected.State != classifier.StateUnknown || projected.Attention != "ownership_lost" ||
				!projected.NeedsAttention {
				t.Fatalf("surface returned before ownership-loss deprojection: %+v", projected)
			}
			if test.name == "list" {
				if len(response.Agents) != 1 || response.Agents[0].Status != "unknown" ||
					response.Agents[0].Attention != "ownership_lost" {
					t.Fatalf("list projection = %#v", response.Agents)
				}
			}
			if test.name == "status" && (response.Agent == nil || response.Agent.Status != "unknown" ||
				response.Agent.Attention != "ownership_lost") {
				t.Fatalf("status projection = %#v", response)
			}
			if len(fw.sent) != 0 || len(fw.submitted) != 0 {
				t.Fatalf("rejected surface mutated provider: sent=%#v submitted=%#v", fw.sent, fw.submitted)
			}
		})
	}
}

func TestControlAppAgentListOrderingStableAcrossNoopAndFollowsActivity(t *testing.T) {
	fw := newFakeControlWatcher()
	agents := []*classifier.Agent{
		{
			ID:        "main:@1",
			Name:      "Franklin",
			State:     classifier.StateRunning,
			UpdatedAt: time.Date(2026, 8, 7, 10, 0, 1, 0, time.UTC),
		},
		{
			ID:        "main:@2",
			Name:      "Brain",
			State:     classifier.StateRunning,
			UpdatedAt: time.Date(2026, 8, 7, 10, 0, 2, 0, time.UTC),
		},
		{
			ID:        "main:@3",
			Name:      "Chrome",
			State:     classifier.StateRunning,
			UpdatedAt: time.Date(2026, 8, 7, 10, 0, 3, 0, time.UTC),
		},
	}
	for _, agent := range agents {
		fw.agents[agent.ID] = agent
	}
	app := &controlApp{watcher: fw}

	// No-op list: timestamps unchanged, ordering must be identical every call.
	var previous []string
	for attempt := 0; attempt < 3; attempt++ {
		resp := app.HandleControlRequest(control.Request{Type: "agent_list"})
		if !resp.OK {
			t.Fatalf("agent_list attempt %d failed: %#v", attempt, resp)
		}
		got := make([]string, 0, len(resp.Agents))
		for _, agent := range resp.Agents {
			got = append(got, agent.ID)
		}
		want := []string{"main:@3", "main:@2", "main:@1"}
		if len(got) != len(want) {
			t.Fatalf("agent_list attempt %d order = %#v, want %#v", attempt, got, want)
		}
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("agent_list attempt %d order = %#v, want %#v", attempt, got, want)
			}
		}
		if previous != nil && !slicesEqual(previous, got) {
			t.Fatalf("no-op agent_list reordered rows: %#v -> %#v", previous, got)
		}
		previous = got
	}

	// Newer meaningful activity on the oldest session must move it to the front.
	fw.agents["main:@1"].UpdatedAt = time.Date(2026, 8, 7, 10, 0, 5, 0, time.UTC)
	resp := app.HandleControlRequest(control.Request{Type: "agent_list"})
	got := make([]string, 0, len(resp.Agents))
	for _, agent := range resp.Agents {
		got = append(got, agent.ID)
	}
	want := []string{"main:@1", "main:@3", "main:@2"}
	if len(got) != len(want) {
		t.Fatalf("post-activity order = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("post-activity order = %#v, want %#v", got, want)
		}
	}
}

func slicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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
	fw.captures["brain-agent-worker:@1"] = "current pane\n<codex_internal_context source=\"goal\">hidden objective</codex_internal_context>"
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
	if len(fw.sent) != 1 {
		t.Fatalf("sent calls = %#v", fw.sent)
	}
	identity := assertDelegatedLifecyclePayload(t, fw.sent[0].text, "continue")
	if len(fw.submitted) != 1 || fw.submitted[0].text != fw.sent[0].text || len(fw.ready) != 0 {
		t.Fatalf("structured submits = %#v ready sends = %#v", fw.submitted, fw.ready)
	}
	if !strings.Contains(fw.submitted[0].text, "--turn-id "+identity) {
		t.Fatalf("structured submit lost turn identity: %q", fw.submitted[0].text)
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

func TestControlAppAgentSendUsesStructuredSubmitForEveryProvider(t *testing.T) {
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
			if len(fw.submitted) != 1 || len(fw.ready) != 0 {
				t.Fatalf("submitted=%#v ready=%#v; provider bypassed structured submit", fw.submitted, fw.ready)
			}
			assertDelegatedLifecyclePayload(t, fw.submitted[0].text, "provider follow-up")
		})
	}
}

func TestControlAppAgentSendFailurePreservesRunningLifecycle(t *testing.T) {
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
	if agent.State != classifier.StateRunning || agent.Attention != "" || agent.NeedsAttention {
		t.Fatalf("agent after failed submission = %#v", agent)
	}
	if len(fw.submitted) != 1 || len(fw.ready) != 0 {
		t.Fatalf("structured submits = %#v ready sends = %#v", fw.submitted, fw.ready)
	}
}

func TestAcceptedRunningTurnThenRejectedFollowUpKeepsExecutorLifecycle(t *testing.T) {
	fw := newFakeControlWatcher()
	const agentID = "brain-agent-worker:@1"
	fw.agents[agentID] = &classifier.Agent{
		ID: agentID, State: classifier.StateRunning, Command: "codex --no-alt-screen", Delegated: true,
	}
	app := &controlApp{watcher: fw}

	accepted := app.HandleControlRequest(control.Request{
		Type: "agent_send", AgentID: agentID, Text: "accepted running turn", Submit: true,
	})
	if !accepted.OK || fw.agents[agentID].State != classifier.StateRunning {
		t.Fatalf("accepted response=%#v agent=%#v", accepted, fw.agents[agentID])
	}
	fw.sendErr = os.ErrDeadlineExceeded
	rejected := app.HandleControlRequest(control.Request{
		Type: "agent_send", AgentID: agentID, Text: "rejected follow-up", Submit: true,
	})
	if rejected.OK || fw.agents[agentID].State != classifier.StateRunning ||
		fw.agents[agentID].Phase == "starting" || len(fw.progress) != 0 {
		t.Fatalf("rejected response=%#v agent=%#v progress=%#v", rejected, fw.agents[agentID], fw.progress)
	}

	// Only the executor lifecycle owner supplies the terminal fact.
	fw.agents[agentID].State = classifier.StateDone
	if got := fw.GetAgent(agentID); got == nil || got.State != classifier.StateDone {
		t.Fatalf("executor terminal state was not authoritative: %#v", got)
	}
}

func TestControlAppConfirmedProviderSendClearsStickyLaunchFailure(t *testing.T) {
	fw := newFakeControlWatcher()
	failedAt := time.Date(2026, 6, 8, 8, 59, 0, 0, time.UTC)
	fw.agents["brain-agent-worker:@1"] = &classifier.Agent{
		ID:             "brain-agent-worker:@1",
		Name:           "Franklin",
		State:          classifier.StateFailed,
		Summary:        "Initial delegated prompt was not submitted: provider startup did not become ready",
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

func TestControlAppStructuredSubmitPreservesOriginalBytesBeforeTurnContract(t *testing.T) {
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
	if len(fw.submitted) != 1 {
		t.Fatalf("structured payload = %#v", fw.submitted)
	}
	assertDelegatedLifecyclePayload(t, fw.submitted[0].text, "alpha\r\nβ\n")
}

func TestControlAppAgentSpawnSubmissionFailureReturnsErrorAndAttention(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.sendErr = os.ErrDeadlineExceeded
	app := &controlApp{
		watcher:    fw,
		brainStore: newControlBrainStore(t),
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
	// A definite non-submission must not falsely terminalize the Session:
	// the canonical turn stays Admitted and the projection stays running.
	agent := fw.agents["brain-agent-unsubmitted:@1"]
	if agent == nil || agent.State != classifier.StateRunning || agent.NeedsAttention {
		t.Fatalf("agent after failed initial prompt = %#v", agent)
	}
	items, err := app.brainStore.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != brain.WorkNeedsInput ||
		!strings.Contains(items[0].WaitFor, os.ErrDeadlineExceeded.Error()) {
		t.Fatalf("Work after failed initial prompt = %#v", items)
	}
}

func TestControlAppSpawnSubmissionFailureReconcilesExactlyOnceAcrossMigrationAndRestart(t *testing.T) {
	for _, test := range []struct {
		name                   string
		migrationCompleteFirst bool
	}{
		{name: "migration complete", migrationCompleteFirst: true},
		{name: "migration incomplete", migrationCompleteFirst: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := brain.NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			if test.migrationCompleteFirst {
				complete, processed, err := store.MigrateSignalSystemV1(8)
				if err != nil || !complete || processed != 0 {
					t.Fatalf("complete empty migration=%v processed=%d err=%v", complete, processed, err)
				}
			}

			fw := newFakeControlWatcher()
			fw.turnStore = store
			fw.sendErr = &watcher.InputSubmissionError{
				Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted},
				Cause:  errors.New("provider composer was not ready"),
			}
			app := &controlApp{
				watcher: fw, brainStore: store,
				execs: work.NewExecutorConfig("codex", map[string]work.Executor{
					"codex": {Name: "codex", Command: "codex"},
				}),
			}
			resp := app.HandleControlRequest(control.Request{
				Type: "agent_spawn", Name: "Failed admission", Cwd: "/repo/zen",
				Prompt: "must execute exactly once",
			})
			if resp.OK || resp.Error == nil || resp.Error.Code != "send_prompt_failed" {
				t.Fatalf("spawn failure response=%#v", resp)
			}
			items, err := store.ListWork()
			if err != nil || len(items) != 1 || items[0].Status != brain.WorkNeedsInput {
				t.Fatalf("failed spawn Work=%+v err=%v", items, err)
			}
			if len(fw.sent) != 1 || len(fw.submitted) != 1 || len(fw.created) != 1 {
				t.Fatalf("provider effects sent=%d submitted=%d created=%d, want one each",
					len(fw.sent), len(fw.submitted), len(fw.created))
			}
			reopened, err := brain.NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			if !test.migrationCompleteFirst {
				complete, processed, err := reopened.MigrateSignalSystemV1(8)
				if err != nil || complete || processed != 1 {
					t.Fatalf("incomplete migration pass=%v processed=%d err=%v", complete, processed, err)
				}
				complete, processed, err = reopened.MigrateSignalSystemV1(8)
				if err != nil || !complete || processed != 0 {
					t.Fatalf("migration completion=%v processed=%d err=%v", complete, processed, err)
				}
			}
			events, err := reopened.ListWorkEvents(items[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			reconciles := 0
			notAdmitted := 0
			var notAdmittedEvent brain.WorkEvent
			for _, event := range events {
				if event.Kind == "brain.reconcile_required" && event.HandledAt == nil &&
					event.DiscardedAt == nil && event.Resolution == "" {
					reconciles++
				}
				if event.Kind == "brain.submission_not_admitted" && event.Actionable &&
					event.HandledAt == nil && event.DiscardedAt == nil && event.Resolution == "" {
					notAdmitted++
					notAdmittedEvent = event
				}
				if strings.HasPrefix(event.Kind, "session.") {
					t.Fatalf("definite non-submission projected a Session result: %+v", events)
				}
			}
			wantEvents := 1
			if test.migrationCompleteFirst {
				wantEvents = 2
			}
			if len(events) != wantEvents || reconciles != 0 || notAdmitted != 1 {
				t.Fatalf("final Events=%+v; count=%d want=%d reconcile=%d not_admitted=%d",
					events, len(events), wantEvents, reconciles, notAdmitted)
			}
			if test.migrationCompleteFirst && (events[0].Kind != "brain.reconcile_required" ||
				events[0].Resolution != brain.EventResolutionDiscard || events[0].DiscardedAt == nil ||
				events[1].Kind != "brain.submission_not_admitted") {
				t.Fatalf("initial reconcile was not atomically replaced by submission abort: %+v", events)
			}
			turnID := strings.TrimPrefix(notAdmittedEvent.DedupeKey, "brain:submission-abort:")
			if turnID == notAdmittedEvent.DedupeKey || !strings.Contains(fw.sent[0].text, "--turn-id "+turnID) {
				t.Fatalf("submission abort identity is absent from exact provider payload: event=%+v", notAdmittedEvent)
			}
			submission, found, err := reopened.TurnSubmission(notAdmittedEvent.SourceName, turnID)
			if err != nil || !found || submission.State != watcher.TurnSubmissionAborted ||
				submission.Receipt != turnID {
				t.Fatalf("aborted submission=%+v found=%v err=%v", submission, found, err)
			}
			if turn, found, err := reopened.Turn(notAdmittedEvent.SourceName); err != nil || found {
				t.Fatalf("definite non-submission created Turn=%+v found=%v err=%v", turn, found, err)
			}

			hostID := "brain-agent-brain-hidden:@spawn-failure"
			claimed, ok, err := reopened.ClaimNextActionableEvent(hostID)
			if err != nil || !ok || claimed.Kind != "brain.submission_not_admitted" {
				t.Fatalf("submission-not-admitted claim=%+v ok=%v err=%v", claimed, ok, err)
			}
			resolveControlHostClaim(t, reopened, claimed)
			delivered, _, err := reopened.ConsumeClaimedWorkEvent(
				claimed.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID,
			)
			if err != nil {
				t.Fatal(err)
			}
			if _, resolved, err := reopened.ResolveWorkEvent(brain.WorkEventDispositionRequest{
				EventID: delivered.ID, HandlingID: delivered.HandlingID,
				ProviderTurnID:       delivered.ProviderTurnID,
				ExpectedWorkRevision: delivered.DeliveryWorkRevision,
				Disposition:          brain.WorkDispositionCancel,
			}); err != nil || resolved.Status != brain.WorkCancelled {
				t.Fatalf("typed reconcile resolution Work=%+v err=%v", resolved, err)
			}

			reopenedAgain, err := brain.NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			if replay, claimed, err := reopenedAgain.ClaimNextActionableEvent(hostID); err != nil || claimed {
				t.Fatalf("resolved spawn failure replayed: event=%+v claimed=%v err=%v", replay, claimed, err)
			}
			finalEvents, err := reopenedAgain.ListWorkEvents(items[0].ID)
			if err != nil || len(finalEvents) != wantEvents ||
				finalEvents[len(finalEvents)-1].Kind != "brain.submission_not_admitted" ||
				finalEvents[len(finalEvents)-1].HandledAt == nil ||
				finalEvents[len(finalEvents)-1].Disposition != brain.WorkDispositionCancel {
				t.Fatalf("resolved final Events=%+v err=%v", finalEvents, err)
			}
			if len(fw.sent) != 1 || len(fw.submitted) != 1 || len(fw.created) != 1 {
				t.Fatalf("restart replayed Session effect: sent=%d submitted=%d created=%d",
					len(fw.sent), len(fw.submitted), len(fw.created))
			}
		})
	}
}

func TestControlAppAmbiguousLiveSpawnReturnsPendingAndExactProgressConvergesAfterRestart(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.sendErr = &watcher.InputSubmissionError{
		Result: watcher.InputResult{Outcome: watcher.InputAmbiguous},
		Cause:  fmt.Errorf("provider Session disappeared"),
	}
	fw.dropAgentOnSend = true
	present := watcher.SessionPresencePresent
	fw.probePresence = &present
	store := newControlBrainStore(t)
	const threadID = "brain_thread_spawn_admission_pending"
	if err := store.SetChatState(brain.ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	fw.turnStore = store
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{
		Type:   "agent_spawn",
		Name:   "Ambiguous",
		Cwd:    "/repo/zen",
		Prompt: "must not be replayed",
	})

	if !resp.OK || resp.Error != nil || resp.Agent == nil || resp.Agent.Status != string(classifier.StateRunning) {
		t.Fatalf("response = %#v", resp)
	}
	if !strings.Contains(resp.Confirmation, "awaiting exact turn-scoped admission") {
		t.Fatalf("pending confirmation = %q", resp.Confirmation)
	}
	if resp.BrainWork == nil || resp.BrainWork.Status != brain.WorkRunning ||
		resp.BrainWork.OwnerSessionID != resp.Agent.ID ||
		!strings.Contains(resp.BrainWork.NextAction, "Wait for the delegated Session") {
		t.Fatalf("pending spawn Work = %#v", resp.BrainWork)
	}
	if len(fw.sent) != 1 {
		t.Fatalf("ambiguous spawn submitted the prompt %d times, want exactly once", len(fw.sent))
	}
	pending, found, err := store.PendingTurnSubmission(resp.Agent.ID)
	if err != nil || !found || pending.State != watcher.TurnSubmissionPending ||
		!pending.SignalProtocol || pending.ProposedTurnID == "" {
		t.Fatalf("pending submission = %+v found=%v err=%v", pending, found, err)
	}
	if !strings.Contains(fw.sent[0].text, "--turn-id "+pending.ProposedTurnID) {
		t.Fatalf("submitted prompt lacks pending identity %q", pending.ProposedTurnID)
	}
	if turn, found, err := store.Turn(resp.Agent.ID); err != nil || found {
		t.Fatalf("ambiguous submission created a Turn: %+v found=%v err=%v", turn, found, err)
	}
	if events, err := store.ListWorkEvents(resp.BrainWork.ID); err != nil || len(events) != 0 {
		t.Fatalf("pending spawn events = %+v err=%v", events, err)
	}

	// The durable pending row is the restart owner. A stale/mismatched signal
	// cannot claim it; the exact prompt-carried identity admits the existing
	// transaction without replay, and terminal progress settles it once.
	restarted, err := brain.NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	factAt := time.Now().UTC().Add(time.Second)
	mismatched, err := restarted.ApplyDelegatedTurnProgress(watcher.TurnFact{
		SessionID: resp.Agent.ID, TurnID: "turn:stale", Class: watcher.EvidenceControl,
		Kind: "running", SourceID: "control\x00stale-progress", At: factAt,
	})
	if err != nil || !mismatched.Owned || mismatched.Matched || mismatched.Changed {
		t.Fatalf("mismatched progress = (%+v, %v)", mismatched, err)
	}
	runningFact := watcher.TurnFact{
		SessionID: resp.Agent.ID, TurnID: pending.ProposedTurnID, Class: watcher.EvidenceControl,
		Kind: "running", SourceID: "control\x00exact-running", At: factAt,
		Summary: "Exact signal admitted the existing prompt",
	}
	running, err := restarted.ApplyDelegatedTurnProgress(runningFact)
	if err != nil || !running.Owned || !running.Matched || !running.Changed ||
		running.Turn.Status != watcher.TurnRunning {
		t.Fatalf("exact running progress = (%+v, %v)", running, err)
	}
	doneFact := watcher.TurnFact{
		SessionID: resp.Agent.ID, TurnID: pending.ProposedTurnID, Class: watcher.EvidenceControl,
		Kind: "done", SourceID: "control\x00exact-done", At: factAt.Add(time.Second),
		Summary: "REVIEW_READY: exact signal completion",
	}
	done, err := restarted.ApplyDelegatedTurnProgress(doneFact)
	if err != nil || !done.Owned || !done.Matched || !done.Changed || done.Turn.Status != watcher.TurnDone {
		t.Fatalf("exact done progress = (%+v, %v)", done, err)
	}
	if replay, err := restarted.ApplyDelegatedTurnProgress(doneFact); err != nil || replay.Changed ||
		!replay.Owned || !replay.Matched || replay.Turn.Status != watcher.TurnDone {
		t.Fatalf("replayed done progress = (%+v, %v)", replay, err)
	}
	if len(fw.sent) != 1 {
		t.Fatalf("progress convergence replayed provider input: sent=%#v", fw.sent)
	}
	events, err := restarted.ListWorkEvents(resp.BrainWork.ID)
	if err != nil {
		t.Fatal(err)
	}
	doneEvents := 0
	for _, event := range events {
		if event.Kind == "session.done" {
			doneEvents++
		}
		if event.Kind == "session.failed" || event.Kind == "brain.successor_launch_failed" {
			t.Fatalf("ambiguous admission projected failure Event: %+v", events)
		}
	}
	if doneEvents != 1 {
		t.Fatalf("session.done Events = %d, want one: %+v", doneEvents, events)
	}
	cards, err := restarted.ThreadTimeline(threadID, 0)
	if err != nil || len(cards) != 1 || cards[0].EventKind != "session.done" || cards[0].ID != events[0].ID {
		t.Fatalf("completion cards = %+v events=%+v err=%v", cards, events, err)
	}
}

func TestControlAppAmbiguousSpawnWithVanishedOwnedSessionStillReturnsFailure(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.sendErr = &watcher.InputSubmissionError{
		Result: watcher.InputResult{Outcome: watcher.InputAmbiguous},
		Cause:  fmt.Errorf("provider Session disappeared"),
	}
	fw.dropAgentOnSend = true
	store := newControlBrainStore(t)
	fw.turnStore = store
	app := &controlApp{
		watcher: fw, brainStore: store,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Vanished", Cwd: "/repo/zen", Prompt: "cannot remain live",
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "send_prompt_failed" {
		t.Fatalf("vanished response = %#v", resp)
	}
	items, err := store.ListWork()
	if err != nil || len(items) != 1 || items[0].Status != brain.WorkNeedsInput {
		t.Fatalf("vanished Work = %+v err=%v", items, err)
	}
	if len(fw.sent) != 1 {
		t.Fatalf("vanished submission calls = %d, want one", len(fw.sent))
	}
}

func TestControlAppPreSubmitLaunchFailureRemainsDefinitive(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.createErr = errors.New("injected tmux launch failure")
	store := newControlBrainStore(t)
	app := &controlApp{
		watcher: fw, brainStore: store,
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Never launched", Cwd: "/repo/zen", Prompt: "must not submit",
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "spawn_failed" ||
		!strings.Contains(resp.Error.Message, fw.createErr.Error()) {
		t.Fatalf("pre-submit response = %#v", resp)
	}
	if len(fw.sent) != 0 {
		t.Fatalf("pre-submit failure sent input: %#v", fw.sent)
	}
	items, err := store.ListWork()
	if err != nil || len(items) != 1 || items[0].Status != brain.WorkNeedsInput {
		t.Fatalf("pre-submit Work = %+v err=%v", items, err)
	}
}

func TestControlAppDefinitelyNotSubmittedSpawnFailureStillProjectsFailure(t *testing.T) {
	fw := newFakeControlWatcher()
	fw.sendErr = &watcher.InputSubmissionError{
		Result: watcher.InputResult{Outcome: watcher.InputNotSubmitted},
		Cause:  fmt.Errorf("target provider could not be proven"),
	}
	app := &controlApp{
		watcher:    fw,
		brainStore: newControlBrainStore(t),
		execs: work.NewExecutorConfig("codex", map[string]work.Executor{
			"codex": {Name: "codex", Command: "codex"},
		}),
	}

	resp := app.HandleControlRequest(control.Request{
		Type:   "agent_spawn",
		Name:   "NotSubmitted",
		Cwd:    "/repo/zen",
		Prompt: "cannot be delivered",
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != "send_prompt_failed" {
		t.Fatalf("response = %#v", resp)
	}
	// The input provably never reached the provider: the Session must not be
	// falsely terminalized; the Work surfaces the launch failure.
	agent := fw.agents["brain-agent-notsubmitted:@1"]
	if agent == nil || agent.State != classifier.StateRunning || agent.NeedsAttention {
		t.Fatalf("definitely-not-submitted spawn projected a false failure: %#v", agent)
	}
	items, err := app.brainStore.ListWork()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Status != brain.WorkNeedsInput ||
		!strings.Contains(items[0].NextAction, "Resolve the delegated Session launch failure") {
		t.Fatalf("Work after not-submitted spawn = %#v", items)
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
		TurnID:       "turn:control-app",
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
	if fw.progress[0].progress.TurnID != "turn:control-app" {
		t.Fatalf("watcher progress turn identity = %#v", fw.progress[0].progress)
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

func TestControlAppBrainWorkCRUDAndEventAPI(t *testing.T) {
	store := newControlBrainStore(t)
	service := brain.NewService(store, nil, nil)
	app := &controlApp{brainStore: store, brainService: service}

	created := app.HandleControlRequest(control.Request{
		Type: "brain_work_create",
		BrainWork: &brain.Work{
			Title:            "API Work",
			Objective:        "Exercise the narrow Work API.",
			CompletionPolicy: brain.CompletionBounded,
		},
	})
	if !created.OK || created.BrainWork == nil {
		t.Fatalf("create response = %#v", created)
	}
	status := brain.WorkWaiting
	waitFor := "external result"
	updated := app.HandleControlRequest(control.Request{
		Type:       "brain_work_update",
		WorkID:     created.BrainWork.ID,
		BrainWork:  &brain.Work{Status: status, WaitFor: waitFor},
		WorkFields: []string{"status", "wait_for"},
	})
	if !updated.OK || updated.BrainWork == nil ||
		updated.BrainWork.Status != brain.WorkWaiting ||
		updated.BrainWork.WaitFor != waitFor {
		t.Fatalf("update response = %#v", updated)
	}
	recorded := app.HandleControlRequest(control.Request{
		Type: "brain_work_event",
		BrainWorkEvent: &brain.WorkEvent{
			WorkID:     created.BrainWork.ID,
			Kind:       "external.changed",
			DedupeKey:  "external:1",
			Actionable: true,
		},
	})
	if !recorded.OK || recorded.BrainWorkEvent == nil {
		t.Fatalf("event response = %#v", recorded)
	}
	duplicate := app.HandleControlRequest(control.Request{
		Type:           "brain_work_event",
		BrainWorkEvent: recorded.BrainWorkEvent,
	})
	if !duplicate.OK || duplicate.Confirmation != "Duplicate event already recorded." {
		t.Fatalf("duplicate response = %#v", duplicate)
	}
	claimed, ok, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@1")
	if err != nil || !ok {
		t.Fatalf("claim = %+v ok=%v err=%v", claimed, ok, err)
	}
	resolveControlHostClaim(t, store, claimed)
	delivered, _, err := store.ConsumeClaimedWorkEvent(claimed.ID, claimed.HandlingID, claimed.WorkID, claimed.DeliveryHostSessionID, claimed.ProviderTurnID)
	if err != nil {
		t.Fatal(err)
	}
	resolved := app.HandleControlRequest(control.Request{
		Type: "brain_work_resolve",
		BrainWorkDisposition: &brain.WorkEventDispositionRequest{
			EventID: delivered.ID, HandlingID: delivered.HandlingID,
			ProviderTurnID:       delivered.ProviderTurnID,
			ExpectedWorkRevision: delivered.DeliveryWorkRevision,
			Disposition:          brain.WorkDispositionComplete,
		},
	})
	if !resolved.OK || resolved.BrainWork == nil || resolved.BrainWork.Status != brain.WorkDone ||
		resolved.BrainWorkEvent == nil || resolved.BrainWorkEvent.HandledAt == nil {
		t.Fatalf("resolve response = %#v", resolved)
	}
	listed := app.HandleControlRequest(control.Request{
		Type:   "brain_work_list",
		WorkID: created.BrainWork.ID,
	})
	if !listed.OK || listed.BrainWork == nil ||
		len(listed.BrainWorkEvents) != 1 {
		t.Fatalf("list response = %#v", listed)
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
		if entry.Name == "align" && strings.Contains(entry.Description, "decision frontier") {
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
