package brain

import (
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

// TestMigrateTurnLedgerV1ImportsLegacyMarkersAsHints covers C.2.8: legacy
// tmux markers import as attached hints only — canonical status is
// Admitted/Running, never Done/Failed — so a false legacy done can never be
// exposed by migration.
func TestMigrateTurnLedgerV1ImportsLegacyMarkersAsHints(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-legacy:@1"
	if _, err := store.CreateWork(Work{
		Title:            "Legacy migration",
		Objective:        "Import markers as hints.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	}); err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	imports := []TurnLedgerImport{
		{
			SessionID:       sessionID,
			TurnID:          sessionID + ":turn:1",
			WorkID:          storeWorkID(t, store, sessionID),
			Status:          watcher.TurnRunning,
			AcceptedAt:      acceptedAt,
			ProcessIdentity: "legacy-proc",
			Summary:         "Legacy summary",
			Hint: &watcher.TurnHint{
				Kind:  "session.done",
				Class: watcher.EvidenceLegacy,
				At:    acceptedAt.Add(time.Minute),
			},
		},
		{
			SessionID:       "brain-agent-legacy:@2",
			TurnID:          "brain-agent-legacy:@2:turn:1",
			WorkID:          "missing-work",
			Status:          watcher.TurnAdmitted,
			AcceptedAt:      acceptedAt,
			ProcessIdentity: "legacy-proc-2",
		},
	}
	migrated, err := store.MigrateTurnLedgerV1(imports)
	if err != nil || !migrated {
		t.Fatalf("migration = %v err=%v", migrated, err)
	}
	snapshot, hasTurn, err := store.Turn(sessionID)
	if err != nil || !hasTurn {
		t.Fatalf("migrated turn missing: hasTurn=%v err=%v", hasTurn, err)
	}
	if snapshot.Status != watcher.TurnRunning {
		t.Fatalf("migrated canonical status = %s, want Running (never Done)", snapshot.Status)
	}
	if len(snapshot.Hints) != 1 || snapshot.Hints[0].Kind != "session.done" ||
		snapshot.Hints[0].Class != watcher.EvidenceLegacy {
		t.Fatalf("migrated hints = %+v", snapshot.Hints)
	}
	// The legacy hint is a non-actionable attached note, never a wake.
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+sessionID+":turn:1:session.done")
	if found && row.Actionable {
		t.Fatalf("legacy hint row is actionable: %+v", row)
	}
	// Import is idempotent: a second call imports no new rows.
	if migrated, err := store.MigrateTurnLedgerV1(imports); err != nil || migrated {
		t.Fatalf("migration reran rows: %v err=%v", migrated, err)
	}
	// The completion marker is persisted only by the explicit completion
	// phase (never by the import phase), and completion is idempotent.
	if err := store.CompleteTurnLedgerV1Migration(); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteTurnLedgerV1Migration(); err != nil {
		t.Fatalf("completion reran: %v", err)
	}
}

// TestMigrateTurnLedgerV1ReconcilesFalseDoneHint covers Phase 1b: provider
// history showing the turn still running drops the false legacy done hint and
// canonical stays Running.
func TestMigrateTurnLedgerV1ReconcilesFalseDoneHint(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-legacy:@3"
	if _, err := store.CreateWork(Work{
		Title:            "Legacy reconcile",
		Objective:        "Drop false hints on running history.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	}); err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	workID := storeWorkID(t, store, sessionID)
	if _, err := store.MigrateTurnLedgerV1([]TurnLedgerImport{
		{
			SessionID:       sessionID,
			TurnID:          sessionID + ":turn:1",
			WorkID:          workID,
			Status:          watcher.TurnRunning,
			AcceptedAt:      acceptedAt,
			ProcessIdentity: "legacy-proc",
			Hint: &watcher.TurnHint{
				Kind:  "session.done",
				Class: watcher.EvidenceLegacy,
				At:    acceptedAt.Add(time.Minute),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	// Phase 1b: bound provider running history drops the false hint through
	// the same canonical reducer (the marker was false — A.4.1 class).
	admission := providerAdmission("stream", "msg-1", 1, "sha", acceptedAt.Add(2*time.Second))
	snapshot, _, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID:  sessionID, TurnID: sessionID + ":turn:1",
		Class:      watcher.EvidenceProvider,
		Kind:       "running",
		SourceID:   "provider\x00" + sessionID + "\x00stream\x00activity-1\x001",
		Admission:  admission,
		ActivityID: "activity-1",
		StartedAt:  acceptedAt.Add(2 * time.Second),
		At:         acceptedAt.Add(3 * time.Second),
	})
	if err != nil || snapshot.Status != watcher.TurnRunning {
		t.Fatalf("phase 1b running = %+v err=%v", snapshot, err)
	}
	if len(snapshot.Hints) != 0 {
		t.Fatalf("false legacy done hint survived running history: %+v", snapshot.Hints)
	}
}

// TestMigrateTurnLedgerV1ServicePath exercises the service-side migration:
// legacy markers decode, import with canonical Running, and the reconciliation
// sweep runs without waking on false hints.
func TestMigrateTurnLedgerV1ServicePath(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-legacy:@4"
	if _, err := store.CreateWork(Work{
		Title:            "Legacy service migration",
		Objective:        "Import and reconcile through the service.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	}); err != nil {
		t.Fatal(err)
	}
	marker := `{"schema_version":1,"id":"` + sessionID + `:turn:1","status":"done","accepted_at":"2026-08-06T09:00:00Z","process_identity":"legacy-proc","pane_baseline":"b","settled_at":"2026-08-06T09:01:00Z","summary":"old marker"}`
	hostWatcher := &fakeWatcher{
		sessions: map[string]*classifier.Agent{
			sessionID: {ID: sessionID, State: classifier.StateRunning, Delegated: true, PaneAlive: true},
		},
	}
	service := NewService(store, hostWatcher, nil)
	targets, err := service.MigrateTurnLedgerV1([]watcher.LegacyDelegatedTurnMarker{
		{Target: sessionID, Raw: marker},
	}, []*classifier.Agent{
		{ID: sessionID, State: classifier.StateRunning, Delegated: true},
	})
	if err != nil || len(targets) != 1 || targets[0] != sessionID {
		t.Fatalf("service migration = targets=%v err=%v", targets, err)
	}
	snapshot, hasTurn, _ := store.Turn(sessionID)
	if !hasTurn || snapshot.Status != watcher.TurnRunning {
		t.Fatalf("service-migrated turn = %+v hasTurn=%v", snapshot, hasTurn)
	}
	// The marker must never be adopted as canonical Done: no actionable done.
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+sessionID+":turn:1:session.done")
	if found && row.Actionable {
		t.Fatalf("service migration produced actionable done: %+v", row)
	}
}

// TestMigrateTurnLedgerV1CrashBetweenPhasesResumes covers P1.4: a crash
// between the import phase and completion never skips the remaining
// migration; restart resumes import, Phase 1b, and completion idempotently,
// and the completion marker is never persisted before all phases finish.
func TestMigrateTurnLedgerV1CrashBetweenPhasesResumes(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-legacy:@5"
	if _, err := store.CreateWork(Work{
		Title:            "Legacy crash resume",
		Objective:        "Resume migration after a crash.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	}); err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC)
	imports := []TurnLedgerImport{{
		SessionID:       sessionID,
		TurnID:          sessionID + ":turn:1",
		WorkID:          storeWorkID(t, store, sessionID),
		Status:          watcher.TurnRunning,
		AcceptedAt:      acceptedAt,
		ProcessIdentity: "legacy-proc",
		Hint: &watcher.TurnHint{Kind: "session.done", Class: watcher.EvidenceLegacy, At: acceptedAt.Add(time.Minute)},
	}}
	// Phase 1 only — the daemon crashes before Phase 1b/completion.
	if imported, err := store.MigrateTurnLedgerV1(imports); err != nil || !imported {
		t.Fatalf("phase 1 import = %v err=%v", imported, err)
	}
	// The completion marker must NOT be persisted by the import phase.
	store.mu.Lock()
	database, err := store.loadOrchestrationLocked()
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if database.Migrations.TurnLedgerV1At != nil {
		t.Fatal("completion marker persisted before all phases finished")
	}

	// "Restart": a fresh store on the same root resumes all phases.
	restarted, err := NewStore(store.Root)
	if err != nil {
		t.Fatal(err)
	}
	if imported, err := restarted.MigrateTurnLedgerV1(imports); err != nil || imported {
		t.Fatalf("resume import duplicated rows: imported=%v err=%v", imported, err)
	}
	if err := restarted.CompleteTurnLedgerV1Migration(); err != nil {
		t.Fatal(err)
	}
	restarted.mu.Lock()
	database, err = restarted.loadOrchestrationLocked()
	restarted.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if database.Migrations.TurnLedgerV1At == nil {
		t.Fatal("completion marker missing after resume")
	}
	rows := 0
	for _, turn := range database.BrainTurns {
		if turn.SessionID == sessionID {
			rows++
		}
	}
	if rows != 1 {
		t.Fatalf("migrated turn rows = %d, want exactly one", rows)
	}
}

// TestMigrateTurnLedgerV1ServiceResumesAfterPartialPhase covers P1.4 at the
// service level: re-running the service migration after a partial run is
// idempotent and returns the cleanup targets every time.
func TestMigrateTurnLedgerV1ServiceResumesAfterPartialPhase(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-legacy:@6"
	if _, err := store.CreateWork(Work{
		Title:            "Legacy service resume",
		Objective:        "Service migration resumes idempotently.",
		Status:           WorkRunning,
		OwnerSessionID:   sessionID,
		CompletionPolicy: CompletionBounded,
		NextAction:       "Wait for the delegated Session.",
		WaitFor:          "Session " + sessionID,
	}); err != nil {
		t.Fatal(err)
	}
	marker := `{"schema_version":1,"id":"` + sessionID + `:turn:1","status":"done","accepted_at":"2026-08-06T09:00:00Z","process_identity":"legacy-proc","pane_baseline":"b","settled_at":"2026-08-06T09:01:00Z","summary":"old"}`
	service := NewService(store, &fakeWatcher{sessions: map[string]*classifier.Agent{
		sessionID: {ID: sessionID, State: classifier.StateRunning, Delegated: true, PaneAlive: true},
	}}, nil)
	markers := []watcher.LegacyDelegatedTurnMarker{{Target: sessionID, Raw: marker}}
	agents := []*classifier.Agent{{ID: sessionID, State: classifier.StateRunning, Delegated: true}}

	// Simulate a crash after the store import phase only.
	if _, err := store.MigrateTurnLedgerV1([]TurnLedgerImport{{
		SessionID:  sessionID,
		TurnID:     sessionID + ":turn:1",
		WorkID:     storeWorkID(t, store, sessionID),
		Status:     watcher.TurnRunning,
		AcceptedAt: time.Date(2026, 8, 6, 9, 0, 0, 0, time.UTC),
		Hint:       &watcher.TurnHint{Kind: "session.done", Class: watcher.EvidenceLegacy},
	}}); err != nil {
		t.Fatal(err)
	}
	// Restart: the service resumes all phases and returns cleanup targets.
	targets, err := service.MigrateTurnLedgerV1(markers, agents)
	if err != nil || len(targets) != 1 || targets[0] != sessionID {
		t.Fatalf("resumed service migration = targets=%v err=%v", targets, err)
	}
	// A second run (heartbeat retry) is a no-op that still reports targets.
	targets, err = service.MigrateTurnLedgerV1(markers, agents)
	if err != nil || len(targets) != 1 || targets[0] != sessionID {
		t.Fatalf("repeated service migration = targets=%v err=%v", targets, err)
	}
	snapshot, hasTurn, _ := store.Turn(sessionID)
	if !hasTurn || snapshot.Status != watcher.TurnRunning {
		t.Fatalf("resumed turn = %+v hasTurn=%v", snapshot, hasTurn)
	}
	// Provider history is unavailable here and the Session is live: per
	// C.2.8 the legacy hint stays attached and canonical stays Running —
	// it is never adopted as a terminal and never wakes.
	if len(snapshot.Hints) != 1 || snapshot.Hints[0].Kind != "session.done" {
		t.Fatalf("resumed hints = %+v, want retained legacy hint", snapshot.Hints)
	}
	workItem, _, _ := store.WorkByOwnerSession(sessionID)
	row, found := turnEvent(t, store, workItem.ID, "session:"+sessionID+":turn:"+sessionID+":turn:1:session.done")
	if found && row.Actionable {
		t.Fatalf("resumed migration produced actionable done from a legacy hint: %+v", row)
	}
}
