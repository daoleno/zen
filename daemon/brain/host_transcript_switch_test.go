package brain

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func TestHostSwitchBindsNewProviderConversationNotPreviousCodexIdentity(t *testing.T) {
	grokHome := t.TempDir()
	t.Setenv("HOME", grokHome)

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_host_switch"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}

	oldHostID := "brain-agent-brain-old:@1"
	if err := store.SetHostSession(oldHostID, "codex"); err != nil {
		t.Fatal(err)
	}

	codexRollout := filepath.Join(t.TempDir(), "rollout-old-codex.jsonl")
	writeCodexRolloutFixture(t, codexRollout, "old-codex-session", []map[string]any{
		{
			"timestamp": "2026-08-13T04:00:00Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "prior codex user",
			},
		},
		{
			"timestamp": "2026-08-13T04:00:01Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "agent_message",
				"message": "prior codex assistant",
			},
		},
	})
	if err := store.SetHostProviderTranscript("old-codex-session", codexRollout, filepath.Dir(codexRollout)); err != nil {
		t.Fatal(err)
	}

	service := NewService(store, nil, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex", Runtime: work.AgentRuntimeTmux},
		"grok":  {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions", Kind: "grok", Runtime: work.AgentRuntimeTmux},
	}))
	if err := service.MaterializeProviderConversation(threadID, mustHostBound(t, service)); err != nil {
		t.Fatal(err)
	}

	userBody := "please continue after switching host"
	receipt := "switch-admit-1"
	store.now = func() time.Time {
		return time.Date(2026, 8, 13, 4, 40, 0, 0, time.UTC)
	}
	if _, err := store.AdmitUserMessage(threadID, oldHostID, receipt, userBody); err != nil {
		t.Fatal(err)
	}

	item, err := store.CreateWork(Work{
		Title:            "zen-provider-save-test-model-id",
		Objective:        "Keep the work card after host switch",
		Status:           WorkDone,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	event, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID:     item.ID,
		Kind:       "session.done",
		DedupeKey:  "session:worker:@1:turn:one:session.done",
		Actionable: true,
		Summary:    "Fixed empty-model provider discover/test compile path.",
		SourceName: "worker",
		PayloadRef: "session:worker:@1",
	})
	if err != nil || !created {
		t.Fatalf("work event created=%v err=%v", created, err)
	}
	if _, materialized, err := store.MaterializeWorkCard(item, event); err != nil || !materialized {
		t.Fatalf("work card materialized=%v err=%v", materialized, err)
	}

	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldHostID: {
				ID:      oldHostID,
				Name:    "Brain",
				State:   classifier.StateRunning,
				Cwd:     store.WorkspacePath(),
				Command: "codex",
				Hidden:  true,
			},
		},
	}
	service = NewService(store, fw, work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex", Runtime: work.AgentRuntimeTmux},
		"grok":  {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions", Kind: "grok", Runtime: work.AgentRuntimeTmux},
	}))

	snapshot, err := service.SetHostExecutor("grok")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID == oldHostID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	newHostID := snapshot.HostAgent.ID
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ExecutorID != "grok" || host.ID != newHostID {
		t.Fatalf("host session after switch = %+v", host)
	}
	if host.ProviderSessionID == "old-codex-session" || host.TranscriptPath == codexRollout {
		t.Fatalf("new host kept previous Codex identity: %+v", host)
	}

	startedAt := time.Date(2026, 8, 13, 4, 41, 0, 0, time.UTC)
	grokSessionID := "grok-host-after-switch"
	agent := fw.GetAgent(newHostID)
	if agent == nil {
		t.Fatal("new grok host missing from watcher")
	}
	agent.StartedAt = startedAt
	agent.Cwd = store.WorkspacePath()
	agent.Command = strings.TrimSpace(agent.Command) + " --resume " + grokSessionID
	fw.sessions[newHostID] = agent

	grokDir := writeBrainGrokHostFixture(t, grokHome, store.WorkspacePath(), grokSessionID, startedAt, userBody)

	boundIdentity, err := service.BindHostProviderTranscript()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(boundIdentity.SessionID) == "old-codex-session" ||
		strings.TrimSpace(boundIdentity.Path) == codexRollout {
		t.Fatalf("bind kept previous Codex identity: %+v", boundIdentity)
	}
	if !strings.Contains(boundIdentity.Path, grokSessionID) && boundIdentity.SessionID != grokSessionID {
		t.Fatalf("bind did not attach new Grok conversation: %+v", boundIdentity)
	}

	bound, err := service.HostBoundProviderConversation()
	if err != nil {
		t.Fatal(err)
	}
	if !bound.Available {
		t.Fatalf("bound conversation unavailable: %+v", bound)
	}
	if bound.Path == codexRollout || strings.Contains(bound.Path, "rollout-old-codex") {
		t.Fatalf("bound load used previous Codex rollout: %+v", bound)
	}
	if !strings.Contains(bound.Path, grokDir) && bound.SessionID != grokSessionID {
		t.Fatalf("bound load is not the new Grok session: %+v", bound)
	}

	if err := service.MaterializeProviderConversation(threadID, bound); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}

	admissionCount := 0
	workCardCount := 0
	grokAssistantCount := 0
	codexAssistantCount := 0
	userBodies := map[string]int{}
	for _, item := range items {
		switch item.Kind {
		case "user_message":
			userBodies[item.Body]++
			if item.BrainAdmission && item.Body == userBody {
				admissionCount++
			}
			if strings.Contains(item.Body, "Brain host executor handoff:") ||
				strings.Contains(item.Body, "You are Brain inside zen") {
				t.Fatalf("handoff/bootstrap became a visible user row: %#v", item)
			}
		case "assistant_message":
			if strings.Contains(item.Body, "Handoff acknowledged") ||
				strings.Contains(item.Body, "Brain host executor handoff:") ||
				strings.Contains(item.Body, "Treat this bootstrap as a map") {
				t.Fatalf("handoff/bootstrap became a visible assistant row: %#v", item)
			}
			if item.Body == "real grok reply after switch" {
				grokAssistantCount++
			}
			if item.Body == "prior codex assistant" {
				codexAssistantCount++
			}
		case "work_card":
			workCardCount++
		}
	}
	if admissionCount != 1 {
		t.Fatalf("admitted user rows = %d want 1; timeline=%#v", admissionCount, items)
	}
	if userBodies[userBody] != 1 {
		t.Fatalf("duplicate user rows for admitted body: %#v", items)
	}
	if workCardCount != 1 {
		t.Fatalf("work cards = %d want 1; timeline=%#v", workCardCount, items)
	}
	if grokAssistantCount != 1 {
		t.Fatalf("new host assistant rows = %d want 1; timeline=%#v", grokAssistantCount, items)
	}
	if codexAssistantCount != 1 {
		t.Fatalf("previous Codex assistant should remain once, got %d; timeline=%#v", codexAssistantCount, items)
	}

	reopened, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := reopened.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	foundGrok := false
	for _, item := range restarted {
		if item.Kind == "assistant_message" && item.Body == "real grok reply after switch" {
			foundGrok = true
		}
	}
	if !foundGrok {
		t.Fatalf("cold reopen lost new host assistant rows: %#v", restarted)
	}
}

func TestBoundHostConversationSubmissionResolutionRecoversGrokUntimestampedWorkEvent(t *testing.T) {
	payload := work.FormatDirectWorkEventInput(work.DirectWorkEventInput{
		EventID:            "event-grok-delivery",
		WorkID:             "work-grok-delivery",
		WorkRevision:       1,
		HandlingID:         "handling-grok-delivery",
		ProviderTurnID:     "turn-grok-delivery",
		ResolutionRequired: true,
		ResolveCommand:     "zen brain work resolve --event-id event-grok-delivery --disposition continue",
		WorkTitle:          "Grok host delivery",
		Kind:               "session.done",
	})
	acceptedAt := time.Date(2026, 8, 13, 5, 10, 0, 0, time.UTC)
	startedAt := acceptedAt.Add(2 * time.Second)
	conversation := work.CodexConversation{
		Available: true,
		Source:    "grok_session",
		SessionID: "grok-after-switch",
		Path:      "/tmp/grok-after-switch",
		Activity: &work.ProviderActivity{
			ID:        "grok-activity-1",
			Status:    work.ProviderActivityRunning,
			StartedAt: startedAt.Format(time.RFC3339Nano),
		},
		Events: []work.CodexConversationEvent{
			{
				ID:              "handoff",
				Seq:             1,
				Kind:            "user_message",
				Body:            "Brain host executor handoff:\nWait for the next Work Event.",
				AdmissionSHA256: AdmissionDigest("handoff"),
			},
			{
				ID:              "work-event",
				Seq:             2,
				Kind:            "user_message",
				Body:            payload,
				AdmissionSHA256: AdmissionDigest("<user_query>\n" + payload + "\n</user_query>"),
			},
		},
	}
	_, _, matched := boundHostConversationSubmissionResolution(conversation, watcher.TurnSubmission{
		SessionID:      "brain-agent-grok:@1",
		ProposedTurnID: "turn-grok-delivery",
		Receipt:        "event-grok-delivery",
		PayloadSHA256:  AdmissionDigest(payload),
		AcceptedAt:     acceptedAt,
		State:          watcher.TurnSubmissionPending,
		ClaimToken:     "handling-grok-delivery",
		WorkID:         "work-grok-delivery",
	}, startedAt.Add(time.Second))
	if !matched {
		t.Fatal("Grok untimestamped Work Event user must resolve the pending Host submission")
	}
}

func TestHostSwitchGrokWorkEventAmbiguousReceiptSettlesWithoutQuarantine(t *testing.T) {
	grokHome := t.TempDir()
	t.Setenv("HOME", grokHome)

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "brain_thread_grok_event_delivery"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}

	oldHostID := "brain-agent-brain-old:@event-delivery"
	if err := store.SetHostSession(oldHostID, "codex"); err != nil {
		t.Fatal(err)
	}
	codexRollout := filepath.Join(t.TempDir(), "rollout-old-codex.jsonl")
	writeCodexRolloutFixture(t, codexRollout, "old-codex-session", []map[string]any{
		{
			"timestamp": "2026-08-13T05:00:00Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "user_message",
				"message": "prior codex user",
			},
		},
		{
			"timestamp": "2026-08-13T05:00:01Z",
			"type":      "event_msg",
			"payload": map[string]any{
				"type":    "agent_message",
				"message": "prior codex assistant",
			},
		},
	})
	if err := store.SetHostProviderTranscript("old-codex-session", codexRollout, filepath.Dir(codexRollout)); err != nil {
		t.Fatal(err)
	}

	execs := work.NewExecutorConfig("codex", map[string]work.Executor{
		"codex": {Name: "codex", Command: "codex", Kind: "codex", Runtime: work.AgentRuntimeTmux},
		"grok":  {Name: "grok", Command: "grok --no-alt-screen --permission-mode bypassPermissions", Kind: "grok", Runtime: work.AgentRuntimeTmux},
	})
	fw := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			oldHostID: {
				ID:      oldHostID,
				Name:    "Brain",
				State:   classifier.StateRunning,
				Cwd:     store.WorkspacePath(),
				Command: "codex",
				Hidden:  true,
			},
		},
		outcomes: map[string]watcher.InputOutcome{},
	}
	service := NewService(store, fw, execs)
	snapshot, err := service.SetHostExecutor("grok")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.HostAgent == nil || snapshot.HostAgent.ID == oldHostID {
		t.Fatalf("host agent = %#v", snapshot.HostAgent)
	}
	hostID := snapshot.HostAgent.ID
	host, err := store.HostSession()
	if err != nil {
		t.Fatal(err)
	}
	if host.ExecutorID != "grok" || host.ID != hostID {
		t.Fatalf("host session after switch = %+v", host)
	}

	item := createSignalTestWork(t, store, "Grok host event delivery", "brain-agent-worker-grok-delivery:@1")
	event := appendSignalTestEvent(t, store, item, "grok-host-delivery")
	item, err = store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	claim, claimed, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !claimed || claim.ID != event.ID {
		t.Fatalf("claim=%+v claimed=%v err=%v", claim, claimed, err)
	}
	payload, err := marshalDirectWorkEventInput(claim, item)
	if err != nil {
		t.Fatal(err)
	}
	pending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: claim.WorkID, SessionID: hostID, ProposedTurnID: claim.ProviderTurnID,
		Receipt: claim.ID, ClaimToken: claim.HandlingID,
		PayloadSHA256:   pendingSubmissionDigest(payload),
		ProcessIdentity: "host-process-identity", PaneGeneration: "host-pane-generation",
		AcceptedAt: claim.ClaimedAt.UTC(), Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created {
		t.Fatalf("prepare pending created=%v submission=%+v err=%v", created, pending, err)
	}

	startedAt := claim.ClaimedAt.UTC().Add(-time.Second)
	grokSessionID := "grok-host-event-delivery"
	agent := fw.GetAgent(hostID)
	if agent == nil {
		t.Fatal("new grok host missing from watcher")
	}
	agent.StartedAt = startedAt
	agent.Cwd = store.WorkspacePath()
	agent.Command = strings.TrimSpace(agent.Command) + " --resume " + grokSessionID
	fw.sessions[hostID] = agent

	activityAt := claim.ClaimedAt.UTC().Add(time.Second)
	writeBrainGrokWorkEventHostFixture(t, grokHome, store.WorkspacePath(), grokSessionID, startedAt, activityAt, payload)
	if _, err := service.BindHostProviderTranscript(); err != nil {
		t.Fatal(err)
	}

	sendsBeforeReconcile := len(fw.sentCalls)
	fw.setReceiptOutcome(claim.ID, watcher.InputAmbiguous)
	woke, err := service.ReconcileHostLane()
	if err != nil {
		t.Fatalf("reconcile after Grok host switch: %v", err)
	}
	if woke {
		t.Fatal("ambiguous Grok receipt must consume the held claim without dispatching another Event")
	}
	if len(fw.sentCalls) != sendsBeforeReconcile {
		t.Fatalf("recovery replayed provider input: sends before=%d after=%d", sendsBeforeReconcile, len(fw.sentCalls))
	}

	delivered, found, err := store.WorkEvent(claim.ID)
	if err != nil || !found || delivered.DeliveredAt == nil || delivered.DeliveryWorkRevision != item.Revision {
		t.Fatalf("recovered Event=%+v found=%v err=%v", delivered, found, err)
	}
	notes, err := store.ListWorkEvents(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, note := range notes {
		if note.Kind == "delivery.ambiguous" {
			t.Fatalf("Grok Host Event went delivery.ambiguous: %#v", notes)
		}
	}
	if _, _, err := service.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID: delivered.ProviderTurnID, ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition: WorkDispositionComplete, Summary: "Grok host processed the Work Event.",
	}); err != nil {
		t.Fatalf("typed complete after Grok host delivery: %v", err)
	}

	bound, err := service.HostBoundProviderConversation()
	if err != nil {
		t.Fatal(err)
	}
	if err := service.MaterializeProviderConversation(threadID, bound); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, timelineItem := range items {
		if strings.Contains(timelineItem.Body, "Brain host executor handoff:") ||
			strings.Contains(timelineItem.Body, "You are Brain inside zen") ||
			strings.Contains(timelineItem.Body, "Treat this bootstrap as a map") ||
			strings.Contains(timelineItem.Body, "Handoff acknowledged") {
			t.Fatalf("handoff/bootstrap became a visible Interface row: %#v", timelineItem)
		}
	}
}

func mustHostBound(t *testing.T, service *Service) work.CodexConversation {
	t.Helper()
	bound, err := service.HostBoundProviderConversation()
	if err != nil {
		t.Fatal(err)
	}
	if !bound.Available {
		t.Fatalf("expected initial Codex bind, got %+v", bound)
	}
	return bound
}

func writeBrainGrokHostFixture(t *testing.T, home, cwd, sessionID string, startedAt time.Time, publicUser string) string {
	t.Helper()
	cwd = filepath.Clean(cwd)
	if !strings.HasPrefix(cwd, "/") {
		cwd = "/" + cwd
	}
	sessionDir := filepath.Join(home, ".grok", "sessions", url.PathEscape(cwd), sessionID)
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	created := startedAt.UTC().Add(2 * time.Second).Format(time.RFC3339Nano)
	updated := startedAt.UTC().Add(2 * time.Minute).Format(time.RFC3339Nano)
	summary, err := json.Marshal(map[string]any{
		"info": map[string]any{
			"id":  sessionID,
			"cwd": cwd,
		},
		"created_at": created,
		"updated_at": updated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.json"), summary, 0o600); err != nil {
		t.Fatal(err)
	}

	lines := []any{
		map[string]any{
			"type":    "user",
			"content": "You are Brain inside zen, the user's private second brain and agent orchestrator.\nTreat this bootstrap as a map, not the full context.",
		},
		map[string]any{
			"type":    "assistant",
			"content": "Bootstrap ready.",
		},
		map[string]any{
			"type":    "user",
			"content": "Brain host executor handoff:\nThe user switched Brain host executors. This is the same visible Brain chat, not a new conversation.\nWait for the next user message or direct Work Event input.",
		},
		map[string]any{
			"type":    "assistant",
			"content": "Handoff acknowledged, continuing.",
		},
		map[string]any{
			"type":    "user",
			"content": publicUser,
		},
		map[string]any{
			"type":    "assistant",
			"content": "real grok reply after switch",
		},
	}
	var builder strings.Builder
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		builder.Write(raw)
		builder.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "chat_history.jsonl"), []byte(builder.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return sessionDir
}

func writeBrainGrokWorkEventHostFixture(t *testing.T, home, cwd, sessionID string, startedAt, activityAt time.Time, payload string) string {
	t.Helper()
	sessionDir := writeBrainGrokHostFixture(t, home, cwd, sessionID, startedAt, "<user_query>\n"+payload+"\n</user_query>")
	streamStartMs := activityAt.UTC().UnixMilli()
	unixSeconds := activityAt.UTC().Unix()
	promptID := "prompt-" + sessionID
	updates := []any{
		grokHostUpdateRecord(sessionID, promptID, "user_message_chunk", streamStartMs, unixSeconds, map[string]any{
			"type": "text",
			"text": payload,
		}),
		grokHostUpdateRecord(sessionID, promptID, "agent_message_chunk", streamStartMs, unixSeconds+1, map[string]any{
			"type": "text",
			"text": "processing the Work Event",
		}),
	}
	var builder strings.Builder
	for _, line := range updates {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		builder.Write(raw)
		builder.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "updates.jsonl"), []byte(builder.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return sessionDir
}

func grokHostUpdateRecord(sessionID, promptID, kind string, streamStartMs, unixSeconds int64, content any) map[string]any {
	update := map[string]any{"sessionUpdate": kind}
	if content != nil {
		update["content"] = content
	}
	meta := map[string]any{}
	if kind != "turn_completed" {
		meta["promptId"] = promptID
		meta["streamStartMs"] = streamStartMs
	}
	return map[string]any{
		"timestamp": unixSeconds,
		"params": map[string]any{
			"sessionId": sessionID,
			"update":    update,
			"_meta":     meta,
		},
	}
}
