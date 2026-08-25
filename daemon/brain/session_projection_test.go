package brain

import (
	"errors"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

func newSessionProjectionService(t *testing.T, agents map[string]*classifier.Agent) *Service {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	all := make([]*classifier.Agent, 0, len(agents))
	for _, agent := range agents {
		all = append(all, agent)
	}
	fw := &fakeWatcher{sessions: agents, agents: all}
	return NewService(store, fw, nil)
}

func TestDelegatedSessionsOnlyUserVisibleDelegated(t *testing.T) {
	agents := map[string]*classifier.Agent{
		"host":       {ID: "host", Hidden: true, Delegated: true, State: classifier.StateRunning},
		"manual":     {ID: "manual", Delegated: false, State: classifier.StateRunning},
		"hidden-del": {ID: "hidden-del", Hidden: true, Delegated: true, State: classifier.StateRunning},
		"visible":    {ID: "visible", Name: "Codex worker", Delegated: true, State: classifier.StateRunning},
	}
	service := newSessionProjectionService(t, agents)
	sessions, err := service.DelegatedSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "visible" || sessions[0].Name != "Codex worker" {
		t.Fatalf("delegated sessions=%+v", sessions)
	}
}

func TestWorkForSessionPrefersLiveWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	liveItem, err := store.CreateWork(Work{
		Title: "Live", Objective: "Live work", CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	bootstrapAdmittedTurnFixture(t, store, liveItem.ID, watcher.AdmittedTurn{
		SessionID: "sess-1", TurnID: "sess-1:turn:1", AcceptedAt: acceptedAt,
	})
	service := NewService(store, &fakeWatcher{}, nil)
	workItem, found, err := service.WorkForSession("sess-1")
	if err != nil || !found || workItem.ID != liveItem.ID || workItem.AttemptSessionID != "sess-1" {
		t.Fatalf("live work=%+v found=%v err=%v", workItem, found, err)
	}
	if workItem.Status != WorkRunning {
		t.Fatalf("live work status=%s", workItem.Status)
	}
	_, found, err = service.WorkForSession("absent")
	if err != nil || found {
		t.Fatalf("absent work found=%v err=%v", found, err)
	}
}

func TestSubmitExternalSessionInputReceiptOutcomes(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		"sess-1": {ID: "sess-1", Delegated: true, State: classifier.StateRunning},
	}}
	service := NewService(store, fw, nil)
	disposition, err := service.SubmitExternalSessionInput("sess-1", wantReceipt, "direct text")
	if err != nil || disposition != ExternalInputAccepted {
		t.Fatalf("accepted disposition=%q err=%v", disposition, err)
	}
	if len(fw.sentCalls) != 1 || fw.sentCalls[0].sessionID != "sess-1" || fw.sentCalls[0].text != "direct text" {
		t.Fatalf("provider calls=%+v", fw.sentCalls)
	}

	// The watcher reports an ambiguous provider admission: the disposition is
	// uncertain and the channel must never replay.
	fw.sendErr = &watcher.InputSubmissionError{
		Result: watcher.InputResult{Outcome: watcher.InputAmbiguous, Receipt: wantReceipt + "-2"},
		Cause:  errors.New("provider admission unknown"),
	}
	disposition, err = service.SubmitExternalSessionInput("sess-1", wantReceipt+"-2", "maybe")
	if err == nil || disposition != ExternalInputUncertain {
		t.Fatalf("ambiguous disposition=%q err=%v", disposition, err)
	}

	// Absent/unknown Session, empty identity: fail closed as not submitted.
	if _, err := service.SubmitExternalSessionInput("ghost", wantReceipt, "text"); err == nil {
		t.Fatal("absent session admitted")
	}
	if _, err := service.SubmitExternalSessionInput("", wantReceipt, "text"); err == nil {
		t.Fatal("empty session admitted")
	}
	if _, err := service.SubmitExternalSessionInput("sess-1", "", "text"); err == nil {
		t.Fatal("empty receipt admitted")
	}
}

const wantReceipt = "telegram:update:7001:99"

func TestSessionProjectionVisibleLifecycleAndSanitizedAssistant(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	liveItem, err := store.CreateWork(Work{
		Title: "Worker task", Objective: "Task", CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	bootstrapAdmittedTurnFixture(t, store, liveItem.ID, watcher.AdmittedTurn{
		SessionID: "sess-1", TurnID: "sess-1:turn:1", AcceptedAt: acceptedAt,
	})
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{
		"sess-1": {ID: "sess-1", Name: "Worker", Delegated: true, State: classifier.StateRunning, Command: "codex"},
	}}
	service := NewService(store, fw, nil)
	service.sessionConversationHook = func(_ *classifier.Agent, _ string, _ time.Time) (work.CodexConversation, error) {
		return work.CodexConversation{Events: []work.CodexConversationEvent{
			{ID: "tool-1", Kind: "tool", Title: "exec", Input: "secret tool payload", Output: "raw output"},
			{ID: "goal-1", Kind: "assistant_message", Body: "hidden goal context", Source: "goal"},
			{ID: "env-1", Kind: "user_message", Body: "<zen_work_event>\n{}\n</zen_work_event>"},
			{ID: "empty-1", Kind: "assistant_message", Body: ""},
			{ID: "visible-1", Kind: "assistant_message", Body: "**User-visible** answer", Timestamp: "2026-08-25T10:00:00Z", Partial: true},
			{ID: "visible-2", Kind: "assistant_message", Body: "Second answer", Timestamp: "2026-08-25T10:00:01Z", Partial: false},
		}}, nil
	}
	projection, err := service.SessionProjection("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !projection.Present || projection.Label != "Worker" || projection.Status != "running" {
		t.Fatalf("projection=%+v", projection)
	}
	if projection.WorkID != liveItem.ID || projection.WorkStatus != "running" || projection.WorkTitle != "Worker task" {
		t.Fatalf("work fields=%+v", projection)
	}
	if len(projection.Assistant) != 2 || projection.Assistant[0].ID != "visible-1" ||
		projection.Assistant[0].Body != "**User-visible** answer" || !projection.Assistant[0].Partial ||
		projection.Assistant[1].ID != "visible-2" {
		t.Fatalf("assistant projection=%+v", projection.Assistant)
	}
	for _, item := range projection.Assistant {
		if item.ID == "tool-1" || item.ID == "goal-1" || item.ID == "env-1" || item.ID == "empty-1" {
			t.Fatalf("hidden event leaked: %+v", item)
		}
	}
	if projection.Assistant[0].CreatedAt.IsZero() || !projection.Assistant[0].CreatedAt.Equal(time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("timestamp parse=%+v", projection.Assistant[0].CreatedAt)
	}
}

func TestSessionProjectionAbsentStillReportsTurnAndWork(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	doneItem, err := store.CreateWork(Work{
		Title: "Done task", Objective: "Done", CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 8, 25, 6, 0, 0, 0, time.UTC)
	// A work whose Session is gone is recorded in the ledger; the projection
	// must still surface the Work identity for staleness marking.
	bootstrapAdmittedTurnFixture(t, store, doneItem.ID, watcher.AdmittedTurn{
		SessionID: "gone-session", TurnID: "gone-session:turn:1", AcceptedAt: acceptedAt,
	})
	fw := &fakeWatcher{sessions: map[string]*classifier.Agent{}}
	service := NewService(store, fw, nil)
	projection, err := service.SessionProjection("gone-session")
	if err != nil {
		t.Fatal(err)
	}
	if projection.Present {
		t.Fatalf("absent session reports present: %+v", projection)
	}
	if projection.WorkID != doneItem.ID || projection.WorkStatus != "running" || projection.ThreadID == "" {
		t.Fatalf("absent work fields=%+v", projection)
	}
}

func TestSessionProjectionReadsNothingForCustomExecutorWithoutTranscript(t *testing.T) {
	service := newSessionProjectionService(t, map[string]*classifier.Agent{
		"sess-custom": {ID: "sess-custom", Name: "custom", Delegated: true, State: classifier.StateRunning, Command: "custom-agent"},
	})
	projection, err := service.SessionProjection("sess-custom")
	if err != nil {
		t.Fatal(err)
	}
	if !projection.Present || len(projection.Assistant) != 0 {
		t.Fatalf("custom projection=%+v", projection)
	}
}
