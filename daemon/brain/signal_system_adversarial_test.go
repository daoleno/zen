package brain

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestSignalAdversarialSchemasTwoThroughSixMigrateBoundedlyWithoutReplay(t *testing.T) {
	for schema := 2; schema <= 6; schema++ {
		t.Run(fmt.Sprintf("schema-%d", schema), func(t *testing.T) {
			root := t.TempDir()
			stateDir := filepath.Join(root, "state")
			if err := os.MkdirAll(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			at := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
			document := map[string]any{
				"schema_version": schema,
				"migrations":     map[string]any{},
				"brain_work": []any{map[string]any{
					"work_id": "legacy-work", "title": "Legacy Work", "objective": "Reconcile once.",
					"status": "waiting", "completion_policy": "bounded", "created_at": at, "updated_at": at,
				}},
				"brain_work_events": []any{map[string]any{
					"event_id": "historical-delivery", "work_id": "legacy-work", "kind": "legacy.result",
					"dedupe_key": "legacy:result", "actionable": true, "created_at": at,
					"claimed_at": at, "delivery_host_session_id": "old-host", "consumed_at": at,
				}},
			}
			if schema >= 3 {
				document["brain_turns"] = []any{}
			}
			if schema >= 6 {
				document["brain_turn_submissions"] = []any{}
			}
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(stateDir, "orchestration.json"), raw, 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			complete, processed, err := store.MigrateSignalSystemV1(1)
			if err != nil || complete || processed != 1 {
				t.Fatalf("first bounded batch complete=%v processed=%d err=%v", complete, processed, err)
			}
			complete, processed, err = store.MigrateSignalSystemV1(1)
			if err != nil || !complete || processed != 0 {
				t.Fatalf("completion batch complete=%v processed=%d err=%v", complete, processed, err)
			}
			claimed, ok, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@1")
			if err != nil || !ok || claimed.ID == "historical-delivery" || claimed.Kind != "brain.reconcile_required" {
				t.Fatalf("migration replayed historical bytes: claimed=%+v ok=%v err=%v", claimed, ok, err)
			}
			projected := activeWorkByID(t, store, "legacy-work")
			if projected.ProgressMode != WorkProgressReady {
				t.Fatalf("migrated Work mode=%q projection=%+v", projected.ProgressMode, projected)
			}
			restarted, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			events, _ := restarted.ListWorkEvents("legacy-work")
			if countUnhandledEventKind(events, "brain.reconcile_required") != 1 {
				t.Fatalf("restart duplicated migration attention: %+v", events)
			}
		})
	}
}

// A retained legacy owner is not an execution owner unless the Turn Ledger
// contains its exact live non-Host Turn. Migration must retire missing and
// immutable owner links in the same replacement that creates ready attention.
func TestSignalAdversarialSchemasTwoThroughSixRetainedOwnersConvergeToReady(t *testing.T) {
	type ownerShape struct {
		name         string
		terminalTurn bool
	}
	for schema := 2; schema <= 6; schema++ {
		shapes := []ownerShape{{name: "missing Turn"}}
		if schema >= 3 {
			shapes = append(shapes, ownerShape{name: "immutable terminal Turn", terminalTurn: true})
		}
		for _, shape := range shapes {
			t.Run(fmt.Sprintf("schema-%d/%s", schema, shape.name), func(t *testing.T) {
				root := t.TempDir()
				stateDir := filepath.Join(root, "state")
				if err := os.MkdirAll(stateDir, 0o700); err != nil {
					t.Fatal(err)
				}
				at := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
				document := map[string]any{
					"schema_version": schema,
					"migrations":     map[string]any{},
					"brain_work": []any{map[string]any{
						"work_id": "legacy-retained-owner", "title": "Legacy retained owner",
						"objective": "Converge through canonical Turn authority.", "status": "waiting",
						"owner_session_id": "brain-agent-legacy-owner:@1", "owner_delegated": true,
						"completion_policy": "bounded", "created_at": at, "updated_at": at,
					}},
					"brain_work_events": []any{},
				}
				if schema >= 3 {
					turns := []any{}
					if shape.terminalTurn {
						turns = append(turns, map[string]any{
							"session_id": "brain-agent-legacy-owner:@1", "turn_id": "legacy-turn-1",
							"work_id": "legacy-retained-owner", "status": "done",
							"accepted_at": at, "settled_at": at.Add(time.Second),
							"updated_at": at.Add(time.Second), "facts": []any{},
						})
					}
					document["brain_turns"] = turns
				}
				if schema >= 6 {
					document["brain_turn_submissions"] = []any{}
				}
				raw, err := json.Marshal(document)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(stateDir, "orchestration.json"), raw, 0o600); err != nil {
					t.Fatal(err)
				}

				store, err := NewStore(root)
				if err != nil {
					t.Fatal(err)
				}
				complete, processed, err := store.MigrateSignalSystemV1(1)
				if err != nil || complete || processed != 1 {
					t.Fatalf("first bounded batch complete=%v processed=%d err=%v", complete, processed, err)
				}
				complete, processed, err = store.MigrateSignalSystemV1(1)
				if err != nil || !complete || processed != 0 {
					t.Fatalf("completion batch complete=%v processed=%d err=%v", complete, processed, err)
				}
				item, err := store.Work("legacy-retained-owner")
				if err != nil {
					t.Fatal(err)
				}
				if item.OwnerSessionID != "" || item.OwnerDelegated {
					t.Fatalf("historical owner remained active after migration: %+v", item)
				}
				events, err := store.ListWorkEvents(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				if countUnhandledEventKind(events, "brain.reconcile_required") != 1 {
					t.Fatalf("migration attention count != 1: %+v", events)
				}
				if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressReady {
					t.Fatalf("migrated retained owner did not become ready: %+v", projected)
				}

				restarted, err := NewStore(root)
				if err != nil {
					t.Fatal(err)
				}
				if complete, processed, err = restarted.MigrateSignalSystemV1(1); err != nil || !complete || processed != 0 {
					t.Fatalf("idempotent restart complete=%v processed=%d err=%v", complete, processed, err)
				}
				events, err = restarted.ListWorkEvents(item.ID)
				if err != nil || countUnhandledEventKind(events, "brain.reconcile_required") != 1 {
					t.Fatalf("restart duplicated migration attention: events=%+v err=%v", events, err)
				}
			})
		}
	}
}

func resolveAdversarialEvent(
	t *testing.T,
	store *Store,
	event WorkEvent,
	disposition WorkDisposition,
	wake *WorkWake,
	successor string,
) (WorkEvent, Work) {
	t.Helper()
	resolved, item, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID:              event.ID,
		HandlingID:           event.HandlingID,
		ProviderTurnID:       event.ProviderTurnID,
		ExpectedWorkRevision: event.DeliveryWorkRevision,
		Disposition:          disposition,
		SuccessorSessionID:   successor,
		Wake:                 wake,
	})
	if err != nil {
		t.Fatalf("resolve event: %v", err)
	}
	return resolved, item
}

func activeWorkByID(t *testing.T, store *Store, workID string) ActiveWork {
	t.Helper()
	items, err := store.ActiveWork()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == workID {
			return item
		}
	}
	t.Fatalf("active Work %s not found in %+v", workID, items)
	return ActiveWork{}
}

func delegatedSubmissionCandidate(workID, sessionID, turnID, payload string, acceptedAt time.Time) watcher.TurnSubmission {
	return watcher.TurnSubmission{
		WorkID:          workID,
		SessionID:       sessionID,
		ProposedTurnID:  turnID,
		Receipt:         turnID,
		PayloadSHA256:   pendingSubmissionDigest(payload),
		ProcessIdentity: "process-identity",
		PaneGeneration:  "pane-generation",
		AcceptedAt:      acceptedAt,
		Mode:            watcher.TurnSubmissionFresh,
	}
}

func resolveDelegatedSubmission(t *testing.T, store *Store, pending watcher.TurnSubmission, activityID string, at time.Time) watcher.TurnSubmission {
	t.Helper()
	resolved, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: pending.SessionID, ProposedTurnID: pending.ProposedTurnID,
		Receipt: pending.Receipt, PayloadSHA256: pending.PayloadSHA256,
		ActivityID: activityID,
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "admission-" + activityID, Cursor: 1,
			SHA256: pending.PayloadSHA256, At: at.UTC(),
		},
		ResolvedAt: at.UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func seedContinueAuthorityTurn(
	t *testing.T,
	store *Store,
	workID, sessionID, turnID string,
	status watcher.TurnStatus,
	admission watcher.TurnAdmission,
	acceptedAt time.Time,
) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	database, err := store.loadOrchestrationLocked()
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range database.BrainTurns {
		if turn.SessionID == sessionID && turn.TurnID == turnID {
			t.Fatalf("continue authority Turn %s/%s already exists", sessionID, turnID)
		}
	}
	payloadDigest := pendingSubmissionDigest("continue authority " + turnID)
	record := TurnRecord{
		SessionID: sessionID, TurnID: turnID, WorkID: workID,
		Status: status, Receipt: "receipt-" + turnID,
		PayloadSHA256: payloadDigest, Admission: admission,
		AcceptedAt: acceptedAt.UTC(), LeaseDeadline: acceptedAt.Add(turnLeaseGrace).UTC(),
		UpdatedAt: store.nowUTC(), Facts: []TurnFactRecord{},
	}
	if !admission.Empty() {
		record.ActivityID = "activity-" + turnID
		record.Facts = append(record.Facts, TurnFactRecord{
			FactID: "acceptance-" + turnID, Kind: "admission", Class: watcher.EvidenceReceipt,
			At: admission.At.UTC(), Summary: "Provider accepted the exact input",
		})
	} else {
		switch status {
		case watcher.TurnBlocked:
			record.Attention = "user_input"
			record.Facts = append(record.Facts, TurnFactRecord{
				FactID: "control-attention-" + turnID, Kind: "attention", Class: watcher.EvidenceControl,
				At: acceptedAt.Add(time.Second), Summary: "Blocked without provider acceptance",
			})
		case watcher.TurnRunning:
			record.ActivityID = "unaccepted-activity-" + turnID
			record.Facts = append(record.Facts, TurnFactRecord{
				FactID: "provider-running-" + turnID, Kind: "running", Class: watcher.EvidenceProvider,
				At: acceptedAt.Add(time.Second), Summary: "Running derived without provider admission",
			})
		}
	}
	if watcher.TurnTerminal(status) {
		settledAt := acceptedAt.Add(time.Minute).UTC()
		record.SettledAt = &settledAt
	}
	database.BrainTurns = append(database.BrainTurns, record)
	if err := store.persistOrchestrationLocked(database); err != nil {
		t.Fatal(err)
	}
}

func bindContinueAuthorityReservation(t *testing.T, store *Store, workID, sessionID, providerTurnID string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	database, err := store.loadOrchestrationLocked()
	if err != nil {
		t.Fatal(err)
	}
	index := workIndex(database.BrainWork, workID)
	if index < 0 {
		t.Fatalf("continue authority Work %s not found", workID)
	}
	reservation := database.BrainWork[index].SuccessorReservation
	if reservation == nil || reservation.SessionID != sessionID {
		t.Fatalf("continue authority reservation=%+v, want Session %s", reservation, sessionID)
	}
	reservation.ProviderTurnID = providerTurnID
	database.BrainWork[index].SuccessorReservation = reservation
	if err := store.persistOrchestrationLocked(database); err != nil {
		t.Fatal(err)
	}
}

func continueAuthorityDurableBytes(t *testing.T, store *Store, workID, eventID string) (string, string, string) {
	t.Helper()
	store.mu.Lock()
	defer store.mu.Unlock()
	database, err := store.loadOrchestrationLocked()
	if err != nil {
		t.Fatal(err)
	}
	workIndex := workIndex(database.BrainWork, workID)
	eventIndex := workEventIndex(database.BrainWorkEvents, eventID)
	if workIndex < 0 || eventIndex < 0 {
		t.Fatalf("continue authority state missing Work=%s Event=%s", workID, eventID)
	}
	workBytes, err := json.Marshal(database.BrainWork[workIndex])
	if err != nil {
		t.Fatal(err)
	}
	eventBytes, err := json.Marshal(database.BrainWorkEvents[eventIndex])
	if err != nil {
		t.Fatal(err)
	}
	submissionBytes, err := json.Marshal(database.BrainTurnSubmissions)
	if err != nil {
		t.Fatal(err)
	}
	return string(workBytes), string(eventBytes), string(submissionBytes)
}

// Continue may promote only the requested Session's current canonical
// provider-accepted Turn. Status alone is not acceptance authority: a
// pre-receipt Admitted row can derive Blocked from Control attention or
// Running from provider activity while its canonical admission tuple remains
// empty. Every rejection must precede Event, Work, submission, and persistence
// mutation and remain byte-equivalent after reopen.
func TestSignalAdversarialContinueRequiresCurrentCanonicalProviderAcceptance(t *testing.T) {
	type setupCandidate func(
		t *testing.T,
		store *Store,
		item Work,
		delivered WorkEvent,
		sessionID string,
		acceptedAt time.Time,
	)

	completeAdmission := func(turnID string, acceptedAt time.Time) watcher.TurnAdmission {
		return watcher.TurnAdmission{
			Stream: "provider", ID: "admission-" + turnID, Cursor: 1,
			SHA256: pendingSubmissionDigest("continue authority " + turnID),
			At:     acceptedAt.Add(time.Second).UTC(),
		}
	}
	runRejected := func(t *testing.T, reserved bool, successorSessionID string, setup setupCandidate) {
		t.Helper()
		root := t.TempDir()
		store, err := NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		item := createSignalTestWork(t, store, "Continue authority", successorSessionID)
		appendSignalTestEvent(t, store, item, "continue-authority")
		delivered, _ := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
		if reserved {
			if _, err := store.ReserveWorkSuccessor(item.ID, successorSessionID); err != nil {
				t.Fatal(err)
			}
		}
		acceptedAt := time.Date(2026, 8, 10, 7, 0, 0, 0, time.UTC)
		setup(t, store, item, delivered, successorSessionID, acceptedAt)

		beforeWork, beforeEvent, beforeSubmissions := continueAuthorityDurableBytes(t, store, item.ID, delivered.ID)
		writes := 0
		originalWrite := store.writeOrchestration
		store.writeOrchestration = func(path string, value any) error {
			writes++
			return originalWrite(path, value)
		}
		_, _, err = store.ResolveWorkEvent(WorkEventDispositionRequest{
			EventID: delivered.ID, HandlingID: delivered.HandlingID,
			ProviderTurnID:       delivered.ProviderTurnID,
			ExpectedWorkRevision: delivered.DeliveryWorkRevision,
			Disposition:          WorkDispositionContinue,
			SuccessorSessionID:   successorSessionID,
		})
		store.writeOrchestration = originalWrite
		if err == nil {
			t.Fatal("continue promoted a Turn without exact current provider-acceptance authority")
		}
		if writes != 0 {
			t.Fatalf("rejected continue attempted %d persistence writes", writes)
		}
		afterWork, afterEvent, afterSubmissions := continueAuthorityDurableBytes(t, store, item.ID, delivered.ID)
		if afterWork != beforeWork {
			t.Fatalf("rejected continue mutated Work\nbefore=%s\nafter=%s", beforeWork, afterWork)
		}
		if afterEvent != beforeEvent {
			t.Fatalf("rejected continue mutated Event\nbefore=%s\nafter=%s", beforeEvent, afterEvent)
		}
		if afterSubmissions != beforeSubmissions {
			t.Fatalf("rejected continue changed provider submission authority\nbefore=%s\nafter=%s", beforeSubmissions, afterSubmissions)
		}

		reopened, err := NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		reopenedWork, reopenedEvent, reopenedSubmissions := continueAuthorityDurableBytes(t, reopened, item.ID, delivered.ID)
		if reopenedWork != beforeWork || reopenedEvent != beforeEvent || reopenedSubmissions != beforeSubmissions {
			t.Fatalf(
				"reopen changed rejected authority state\nWork before=%s after=%s\nEvent before=%s after=%s\nsubmissions before=%s after=%s",
				beforeWork, reopenedWork, beforeEvent, reopenedEvent, beforeSubmissions, reopenedSubmissions,
			)
		}
	}

	for _, reserved := range []bool{false, true} {
		pathName := "current"
		if reserved {
			pathName = "reserved"
		}
		for _, candidate := range []struct {
			name   string
			status watcher.TurnStatus
		}{
			{name: "pre-receipt-admitted", status: watcher.TurnAdmitted},
			{name: "blocked-without-provider-admission", status: watcher.TurnBlocked},
			{name: "running-without-provider-admission", status: watcher.TurnRunning},
		} {
			reserved := reserved
			candidate := candidate
			t.Run(pathName+"/"+candidate.name, func(t *testing.T) {
				runRejected(t, reserved, "brain-agent-unaccepted:@1", func(
					t *testing.T, store *Store, item Work, _ WorkEvent, sessionID string, acceptedAt time.Time,
				) {
					turnID := sessionID + ":turn:1"
					seedContinueAuthorityTurn(t, store, item.ID, sessionID, turnID, candidate.status, watcher.TurnAdmission{}, acceptedAt)
					if reserved {
						bindContinueAuthorityReservation(t, store, item.ID, sessionID, turnID)
					}
				})
			})
		}
	}

	t.Run("reserved/incomplete-provider-admission", func(t *testing.T) {
		runRejected(t, true, "brain-agent-partial-admission:@1", func(
			t *testing.T, store *Store, item Work, _ WorkEvent, sessionID string, acceptedAt time.Time,
		) {
			turnID := sessionID + ":turn:1"
			partial := watcher.TurnAdmission{Stream: "provider", ID: "provider-row-without-cursor", At: acceptedAt.Add(time.Second)}
			seedContinueAuthorityTurn(t, store, item.ID, sessionID, turnID, watcher.TurnRunning, partial, acceptedAt)
			bindContinueAuthorityReservation(t, store, item.ID, sessionID, turnID)
		})
	})

	t.Run("reserved/older-provider-turn", func(t *testing.T) {
		runRejected(t, true, "brain-agent-older-turn:@1", func(
			t *testing.T, store *Store, item Work, _ WorkEvent, sessionID string, acceptedAt time.Time,
		) {
			olderTurnID := sessionID + ":turn:1"
			newerTurnID := sessionID + ":turn:2"
			seedContinueAuthorityTurn(
				t, store, item.ID, sessionID, olderTurnID, watcher.TurnRunning,
				completeAdmission(olderTurnID, acceptedAt), acceptedAt,
			)
			seedContinueAuthorityTurn(
				t, store, item.ID, sessionID, newerTurnID, watcher.TurnRunning,
				completeAdmission(newerTurnID, acceptedAt.Add(time.Minute)), acceptedAt.Add(time.Minute),
			)
			bindContinueAuthorityReservation(t, store, item.ID, sessionID, olderTurnID)
		})
	})

	t.Run("current/wrong-work", func(t *testing.T) {
		runRejected(t, false, "brain-agent-wrong-work:@1", func(
			t *testing.T, store *Store, item Work, _ WorkEvent, sessionID string, acceptedAt time.Time,
		) {
			store.mu.Lock()
			database, err := store.loadOrchestrationLocked()
			if err != nil {
				store.mu.Unlock()
				t.Fatal(err)
			}
			index := workIndex(database.BrainWork, item.ID)
			if index < 0 {
				store.mu.Unlock()
				t.Fatal("continue authority target Work disappeared")
			}
			database.BrainWork[index].OwnerSessionID = ""
			database.BrainWork[index].OwnerDelegated = false
			if err := store.persistOrchestrationLocked(database); err != nil {
				store.mu.Unlock()
				t.Fatal(err)
			}
			store.mu.Unlock()
			distractor := createSignalTestWork(t, store, "Wrong Work", sessionID)
			turnID := sessionID + ":turn:1"
			seedContinueAuthorityTurn(
				t, store, distractor.ID, sessionID, turnID, watcher.TurnRunning,
				completeAdmission(turnID, acceptedAt), acceptedAt,
			)
		})
	})

	t.Run("current/host-handling-turn", func(t *testing.T) {
		runRejected(t, false, "brain-agent-brain-hidden:@1", func(
			_ *testing.T, _ *Store, _ Work, _ WorkEvent, _ string, _ time.Time,
		) {
		})
	})

	for _, candidate := range []struct {
		name   string
		status watcher.TurnStatus
	}{
		{name: "admitted-even-with-tuple", status: watcher.TurnAdmitted},
		{name: "done", status: watcher.TurnDone},
		{name: "failed", status: watcher.TurnFailed},
		{name: "unknown", status: watcher.TurnUnknown},
	} {
		candidate := candidate
		t.Run("current/inactive-"+candidate.name, func(t *testing.T) {
			runRejected(t, false, "brain-agent-inactive:@1", func(
				t *testing.T, store *Store, item Work, _ WorkEvent, sessionID string, acceptedAt time.Time,
			) {
				turnID := sessionID + ":turn:1"
				seedContinueAuthorityTurn(
					t, store, item.ID, sessionID, turnID, candidate.status,
					completeAdmission(turnID, acceptedAt), acceptedAt,
				)
			})
		})
	}
}

func TestSignalAdversarialContinueAcceptsExactCurrentProviderAdmission(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		reserved := reserved
		name := "current"
		if reserved {
			name = "reserved"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			sessionID := "brain-agent-accepted:@1"
			item := createSignalTestWork(t, store, "Accepted continuation", sessionID)
			appendSignalTestEvent(t, store, item, "accepted-continuation")
			delivered, _ := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
			if reserved {
				if _, err := store.ReserveWorkSuccessor(item.ID, sessionID); err != nil {
					t.Fatal(err)
				}
			}
			acceptedAt := time.Date(2026, 8, 10, 7, 30, 0, 0, time.UTC)
			turnID := sessionID + ":turn:1"
			seedContinueAuthorityTurn(t, store, item.ID, sessionID, turnID, watcher.TurnAccepted, watcher.TurnAdmission{
				Stream: "provider", ID: "admission-" + turnID, Cursor: 1,
				SHA256: pendingSubmissionDigest("continue authority " + turnID), At: acceptedAt.Add(time.Second),
			}, acceptedAt)
			if reserved {
				bindContinueAuthorityReservation(t, store, item.ID, sessionID, turnID)
			}
			resolvedEvent, resolvedWork, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
				EventID: delivered.ID, HandlingID: delivered.HandlingID,
				ProviderTurnID:       delivered.ProviderTurnID,
				ExpectedWorkRevision: delivered.DeliveryWorkRevision,
				Disposition:          WorkDispositionContinue,
				SuccessorSessionID:   sessionID,
			})
			if err != nil {
				t.Fatalf("exact provider-accepted continue rejected: %v", err)
			}
			if resolvedEvent.HandledAt == nil || resolvedWork.OwnerSessionID != sessionID || resolvedWork.SuccessorReservation != nil {
				t.Fatalf("exact provider-accepted continue Event=%+v Work=%+v", resolvedEvent, resolvedWork)
			}
			reopened, err := NewStore(root)
			if err != nil {
				t.Fatal(err)
			}
			durableWork, err := reopened.Work(item.ID)
			if err != nil || durableWork.OwnerSessionID != sessionID {
				t.Fatalf("accepted continuation did not survive reopen: Work=%+v err=%v", durableWork, err)
			}
		})
	}
}

// canonicalHostDeliveryWatcher exercises the same prepare/provider/resolve
// transaction owned by Watcher.SubmitBrainHostInput. It deliberately does not
// bootstrap an Admitted Turn: product-path Host delivery evidence must come
// from the exact pending TurnSubmission and its provider admission.
type canonicalHostDeliveryWatcher struct {
	*fakeWatcher
	store            *Store
	wrongWorkID      string
	prepareCount     int
	resolveCount     int
	wrongClaimChecks int
}

func newCanonicalHostDeliveryWatcher(store *Store, hostID string) *canonicalHostDeliveryWatcher {
	return &canonicalHostDeliveryWatcher{
		fakeWatcher: &fakeWatcher{sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
		}},
		store: store,
	}
}

// This is the live cutover shape that previously wedged startup: an ambiguous
// Host transaction belongs to an older provider process behind the same stable
// Session ID, while an unrelated Work Event is ready for the replacement
// process. Reconciliation must preserve the old Event as held audit, retire
// only its obsolete provider authority, and deliver the unrelated Event once.
func TestSignalAdversarialAmbiguousOldHostGenerationCannotWedgeReplacementLane(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@stable"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	oldWork := createSignalTestWork(t, store, "Ambiguous old Host generation", "brain-agent-worker-old:@1")
	oldEvent := appendSignalTestEvent(t, store, oldWork, "old-generation")
	newWork := createSignalTestWork(t, store, "Replacement Host generation", "brain-agent-worker-new:@1")
	newEvent := appendSignalTestEvent(t, store, newWork, "replacement-generation")

	oldClaim, claimed, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !claimed || oldClaim.ID != oldEvent.ID {
		t.Fatalf("old claim=%+v claimed=%v err=%v", oldClaim, claimed, err)
	}
	oldAcceptedAt := time.Date(2026, 8, 11, 1, 0, 0, 0, time.UTC)
	oldPending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: oldClaim.WorkID, SessionID: hostID, ProposedTurnID: oldClaim.ProviderTurnID,
		Receipt: oldClaim.ID, ClaimToken: oldClaim.HandlingID,
		PayloadSHA256:   pendingSubmissionDigest("ambiguous old Host payload"),
		ProcessIdentity: "old-host-process", PaneGeneration: "old-host-pane",
		AcceptedAt: oldAcceptedAt, Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created {
		t.Fatalf("old Host prepare created=%v submission=%+v err=%v", created, oldPending, err)
	}

	delivery := newCanonicalHostDeliveryWatcher(store, hostID)
	delivery.outcomes = map[string]watcher.InputOutcome{oldClaim.ID: watcher.InputAmbiguous}
	woke, err := NewService(store, delivery, nil).ReconcileHostLane()
	if err != nil || !woke {
		t.Fatalf("replacement Host lane woke=%v err=%v", woke, err)
	}
	if delivery.prepareCount != 1 || delivery.resolveCount != 1 || len(delivery.sentCalls) != 1 {
		t.Fatalf("replacement delivery counts prepare=%d resolve=%d sends=%d", delivery.prepareCount, delivery.resolveCount, len(delivery.sentCalls))
	}
	oldDurable, found, err := store.TurnSubmission(hostID, oldClaim.ProviderTurnID)
	if err != nil || !found || oldDurable.State != watcher.TurnSubmissionRetired {
		t.Fatalf("old Host authority was not retired: submission=%+v found=%v err=%v", oldDurable, found, err)
	}
	oldRow, found, err := store.WorkEvent(oldClaim.ID)
	if err != nil || !found || oldRow.ClaimedAt == nil || oldRow.DeliveredAt != nil || oldRow.Resolution != "" {
		t.Fatalf("ambiguous old Event was replayed or actor-resolved: event=%+v found=%v err=%v", oldRow, found, err)
	}
	newRow, found, err := store.WorkEvent(newEvent.ID)
	if err != nil || !found || newRow.DeliveredAt == nil || newRow.ProviderTurnID == "" {
		t.Fatalf("replacement Event was not delivered: event=%+v found=%v err=%v", newRow, found, err)
	}
	newSubmission, found, err := store.TurnSubmission(hostID, newRow.ProviderTurnID)
	if err != nil || !found || newSubmission.State != watcher.TurnSubmissionResolved {
		t.Fatalf("replacement provider authority did not resolve: submission=%+v found=%v err=%v", newSubmission, found, err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	oldDurable, found, err = reopened.TurnSubmission(hostID, oldClaim.ProviderTurnID)
	if err != nil || !found || oldDurable.State != watcher.TurnSubmissionRetired {
		t.Fatalf("old Host retirement did not survive reopen: submission=%+v found=%v err=%v", oldDurable, found, err)
	}
	newRow, found, err = reopened.WorkEvent(newEvent.ID)
	if err != nil || !found || newRow.DeliveredAt == nil {
		t.Fatalf("replacement delivery did not survive reopen: event=%+v found=%v err=%v", newRow, found, err)
	}
}

func (w *canonicalHostDeliveryWatcher) SubmitBrainHostInput(
	sessionID, payload, eventID, claimToken, workID, providerTurnID string,
	acceptedAt time.Time,
) (watcher.InputResult, error) {
	candidate := watcher.TurnSubmission{
		WorkID: workID, SessionID: sessionID, ProposedTurnID: providerTurnID, Receipt: eventID,
		ClaimToken:      claimToken,
		PayloadSHA256:   pendingSubmissionDigest(payload),
		ProcessIdentity: "host-process-identity", PaneGeneration: "host-pane-generation",
		AcceptedAt: acceptedAt.UTC(), Mode: watcher.TurnSubmissionFresh,
	}
	wrongClaims := []struct {
		name      string
		candidate watcher.TurnSubmission
	}{
		{name: "event", candidate: candidate},
		{name: "claim token", candidate: candidate},
		{name: "Work", candidate: candidate},
		{name: "Host Session", candidate: candidate},
		{name: "provider Turn", candidate: candidate},
	}
	wrongClaims[0].candidate.Receipt = "wrong-event-id"
	wrongClaims[1].candidate.ClaimToken = "wrong-claim-token"
	wrongClaims[2].candidate.WorkID = w.wrongWorkID
	wrongClaims[3].candidate.SessionID = "brain-agent-wrong-host:@1"
	wrongClaims[4].candidate.ProposedTurnID = "wrong-provider-turn"
	for _, probe := range wrongClaims {
		w.wrongClaimChecks++
		if _, created, err := w.store.PrepareTurnSubmission(probe.candidate); err == nil || created {
			return watcher.InputResult{
				Outcome: watcher.InputNotSubmitted, Receipt: eventID, TurnID: providerTurnID,
			}, fmt.Errorf("wrong %s acquired Host admission: created=%v err=%v", probe.name, created, err)
		}
	}

	w.prepareCount++
	pending, created, err := w.store.PrepareTurnSubmission(candidate)
	if err != nil {
		return watcher.InputResult{
			Outcome: watcher.InputNotSubmitted, Receipt: eventID, TurnID: providerTurnID,
		}, err
	}
	if !created || pending.State != watcher.TurnSubmissionPending {
		return watcher.InputResult{
			Outcome: watcher.InputAmbiguous, Receipt: eventID, TurnID: providerTurnID,
		}, fmt.Errorf("Host Turn submission was not freshly prepared")
	}
	if pending.WorkID != workID || pending.Receipt != eventID || pending.ClaimToken != claimToken ||
		pending.SessionID != sessionID || pending.ProposedTurnID != providerTurnID {
		return watcher.InputResult{
			Outcome: watcher.InputAmbiguous, Receipt: eventID, TurnID: providerTurnID,
		}, fmt.Errorf("prepared Host capability changed identity: %+v", pending)
	}
	result, err := w.SendInputWithReceiptResult(sessionID, payload, eventID)
	result.TurnID = providerTurnID
	if err != nil {
		return result, err
	}
	w.resolveCount++
	resolvedAt := acceptedAt.Add(time.Second).UTC()
	resolved, err := w.store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: sessionID, ProposedTurnID: providerTurnID, Receipt: eventID,
		PayloadSHA256: pending.PayloadSHA256, ActivityID: "host-activity-" + providerTurnID,
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "host-admission-" + providerTurnID, Cursor: 1,
			SHA256: pending.PayloadSHA256, At: resolvedAt,
		},
		ResolvedAt: resolvedAt,
	})
	if err != nil {
		result.Outcome = watcher.InputAmbiguous
		return result, err
	}
	result.Outcome = watcher.InputAccepted
	result.TurnID = resolved.ResolvedTurnID
	return result, nil
}

func assertCanonicalHostEventHandledOnce(
	t *testing.T,
	root string,
	store *Store,
	service *Service,
	delivery *canonicalHostDeliveryWatcher,
	hostID, workID string,
) {
	t.Helper()
	before, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	woke, err := service.ReconcileHostLane()
	if err != nil || !woke {
		t.Fatalf("canonical Host delivery woke=%v err=%v", woke, err)
	}
	events, err := store.ListWorkEvents(workID)
	if err != nil {
		t.Fatal(err)
	}
	var delivered WorkEvent
	for _, event := range events {
		if event.DeliveredAt != nil && event.HandledAt == nil {
			if delivered.ID != "" {
				t.Fatalf("more than one live delivered handling: %+v", events)
			}
			delivered = event
		}
	}
	if delivered.ID == "" || delivered.DeliveryHostSessionID != hostID ||
		delivered.HandlingID == "" || delivered.ProviderTurnID == "" ||
		delivered.HandlingID == delivered.ProviderTurnID {
		t.Fatalf("exact Host delivery identity missing: %+v", delivered)
	}
	turn, found, err := store.TurnByID(hostID, delivered.ProviderTurnID)
	if err != nil || !found || turn.Status != watcher.TurnAccepted {
		t.Fatalf("canonical Host provider Turn=%+v found=%v err=%v", turn, found, err)
	}
	if delivery.wrongClaimChecks != 5 || delivery.prepareCount != 1 || delivery.resolveCount != 1 || len(delivery.sentCalls) != 1 {
		t.Fatalf("wrong-claim checks=%d prepare=%d resolve=%d sends=%d",
			delivery.wrongClaimChecks, delivery.prepareCount, delivery.resolveCount, len(delivery.sentCalls))
	}
	afterDelivery, err := store.Work(workID)
	if err != nil {
		t.Fatal(err)
	}
	if afterDelivery.Revision != before.Revision || afterDelivery.Status != before.Status ||
		afterDelivery.OwnerSessionID != before.OwnerSessionID || afterDelivery.OwnerDelegated != before.OwnerDelegated ||
		afterDelivery.SuccessorReservation != nil {
		t.Fatalf("Host admission mutated Work before disposition: before=%+v after=%+v", before, afterDelivery)
	}
	resolved, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
		Summary:              "Handled the exact claimed signal.",
	})
	if err != nil || resolved.HandledAt == nil {
		t.Fatalf("resolve exact Host handling: event=%+v err=%v", resolved, err)
	}
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionComplete,
	}); !errors.Is(err, ErrEventHandled) {
		t.Fatalf("duplicate resolution err=%v, want ErrEventHandled", err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	restartedDelivery := newCanonicalHostDeliveryWatcher(reopened, hostID)
	if woke, err := NewService(reopened, restartedDelivery, nil).ReconcileHostLane(); err != nil || woke {
		t.Fatalf("restart replayed resolved signal: woke=%v err=%v", woke, err)
	}
	if restartedDelivery.prepareCount != 0 || restartedDelivery.resolveCount != 0 || len(restartedDelivery.sentCalls) != 0 {
		t.Fatalf("restart replayed provider input: prepare=%d resolve=%d sends=%d",
			restartedDelivery.prepareCount, restartedDelivery.resolveCount, len(restartedDelivery.sentCalls))
	}
	events, err = reopened.ListWorkEvents(workID)
	if err != nil {
		t.Fatal(err)
	}
	handled := 0
	for _, event := range events {
		if event.ID == delivered.ID && event.DeliveredAt != nil && event.HandledAt != nil {
			handled++
		}
	}
	if handled != 1 {
		t.Fatalf("resolved signal count=%d events=%+v", handled, events)
	}
	submission, found, err := reopened.TurnSubmission(hostID, delivered.ProviderTurnID)
	if err != nil || !found || submission.WorkID != workID || submission.Receipt != delivered.ID ||
		submission.ClaimToken != delivered.HandlingID || submission.State != watcher.TurnSubmissionResolved {
		t.Fatalf("reopened Host admission capability=%+v found=%v err=%v", submission, found, err)
	}
}

func createHostAuthorityDistractorWork(t *testing.T, store *Store) Work {
	t.Helper()
	item, err := store.CreateWork(Work{
		Title: "Host authority distractor", Objective: "Never receive another Event's Host Turn.",
		Status: WorkDone, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestSignalAdversarialHostProductPathAdmitsClaimedTerminalWorkWithHistoricalOwner(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@host-authority-terminal"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "Terminal delegated result", Objective: "Handle the exact completion Event.",
		Status: WorkDone, OwnerSessionID: "brain-agent-historical-worker:@1", OwnerDelegated: true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: item.ID, Kind: "session.done",
		DedupeKey:  "session:historical-worker:turn:terminal:session.done",
		PayloadRef: "session:brain-agent-historical-worker:@1",
		SourceName: "brain-agent-historical-worker:@1",
		Summary:    "The delegated result is ready for Host review.", Actionable: true,
	}); err != nil || !created {
		t.Fatalf("append terminal signal created=%v err=%v", created, err)
	}
	distractor := createHostAuthorityDistractorWork(t, store)
	delivery := newCanonicalHostDeliveryWatcher(store, hostID)
	delivery.wrongWorkID = distractor.ID
	assertCanonicalHostEventHandledOnce(t, root, store, NewService(store, delivery, nil), delivery, hostID, item.ID)
}

func TestSignalAdversarialHostProductPathAdmitsClaimedOwnerlessMigrationAttention(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	document := map[string]any{
		"schema_version": 6,
		"migrations":     map[string]any{},
		"brain_work": []any{map[string]any{
			"work_id": "ownerless-migration-work", "title": "Ownerless migration Work",
			"objective": "Reconcile through the real Host delivery path.", "status": "waiting",
			"completion_policy": "bounded", "created_at": at, "updated_at": at,
		}},
		"brain_work_events":      []any{},
		"brain_turns":            []any{},
		"brain_turn_submissions": []any{},
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "orchestration.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if complete, processed, err := store.MigrateSignalSystemV1(1); err != nil || complete || processed != 1 {
		t.Fatalf("migration batch complete=%v processed=%d err=%v", complete, processed, err)
	}
	if complete, processed, err := store.MigrateSignalSystemV1(1); err != nil || !complete || processed != 0 {
		t.Fatalf("migration completion complete=%v processed=%d err=%v", complete, processed, err)
	}
	hostID := "brain-agent-brain-hidden:@host-authority-migration"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	distractor := createHostAuthorityDistractorWork(t, store)
	delivery := newCanonicalHostDeliveryWatcher(store, hostID)
	delivery.wrongWorkID = distractor.ID
	assertCanonicalHostEventHandledOnce(
		t, root, store, NewService(store, delivery, nil), delivery, hostID, "ownerless-migration-work",
	)
}

// Host transport admission owns only Event delivery. Even when the Host
// Session string happens to equal an exclusive delegated successor Session,
// prepare, provider resolution, and consume must leave every Work byte
// unchanged until the exact typed disposition promotes that successor.
func TestSignalAdversarialHostConsumeNeverBindsCoincidentSuccessorReservation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Coincident Host and successor identity", "brain-agent-incumbent:@1")
	appendSignalTestEvent(t, store, item, "coincident-reservation")
	firstHandling, _ := deliverSignalTestEvent(t, store, "brain-agent-setup-host:@1")
	coincidentID := "brain-agent-coincident:@1"
	if _, err := store.ReserveWorkSuccessor(item.ID, coincidentID); err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.RequeueUnhandledHostAttention(
		firstHandling.ID, firstHandling.HandlingID, firstHandling.ProviderTurnID,
	); err != nil || !created {
		t.Fatalf("requeue reserved handling created=%v err=%v", created, err)
	}
	claimed, ok, err := store.ClaimNextActionableEvent(coincidentID)
	if err != nil || !ok {
		t.Fatalf("claim coincident Host event ok=%v err=%v", ok, err)
	}
	before, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.SuccessorReservation == nil || before.SuccessorReservation.SessionID != coincidentID ||
		before.SuccessorReservation.EventID != "" || before.SuccessorReservation.HandlingID != "" {
		t.Fatalf("fixture did not retain one unbound successor reservation: %+v", before)
	}
	beforeBytes, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	assertWorkBytes := func(boundary string) {
		t.Helper()
		current, readErr := store.Work(item.ID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		currentBytes, marshalErr := json.Marshal(current)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if string(currentBytes) != string(beforeBytes) {
			t.Fatalf("%s mutated Work before disposition:\nbefore=%s\nafter=%s", boundary, beforeBytes, currentBytes)
		}
	}

	acceptedAt := time.Date(2026, 8, 10, 7, 0, 0, 0, time.UTC)
	payloadDigest := pendingSubmissionDigest("coincident Host claim")
	pending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: claimed.WorkID, SessionID: coincidentID, ProposedTurnID: claimed.ProviderTurnID,
		Receipt: claimed.ID, ClaimToken: claimed.HandlingID, PayloadSHA256: payloadDigest,
		ProcessIdentity: "coincident-host-process", PaneGeneration: "coincident-host-pane",
		AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created {
		t.Fatalf("prepare coincident Host submission pending=%+v created=%v err=%v", pending, created, err)
	}
	assertWorkBytes("Host prepare")
	resolvedAt := acceptedAt.Add(time.Second)
	if _, err := store.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: coincidentID, ProposedTurnID: claimed.ProviderTurnID,
		Receipt: claimed.ID, PayloadSHA256: pending.PayloadSHA256,
		ActivityID: "coincident-host-activity",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "coincident-host-admission", Cursor: 1,
			SHA256: pending.PayloadSHA256, At: resolvedAt,
		},
		ResolvedAt: resolvedAt,
	}); err != nil {
		t.Fatalf("resolve coincident Host submission: %v", err)
	}
	assertWorkBytes("provider resolve")
	if _, _, err := store.ConsumeClaimedWorkEvent(
		claimed.ID, claimed.HandlingID, claimed.WorkID, coincidentID, claimed.ProviderTurnID,
	); err != nil {
		t.Fatalf("consume coincident Host claim: %v", err)
	}
	assertWorkBytes("Host consume")
}

func preparePendingClaimedHostSubmission(t *testing.T) (string, *Store, WorkEvent, watcher.TurnSubmission) {
	t.Helper()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item := createSignalTestWork(t, store, "Pending Host claim", "brain-agent-worker:@1")
	appendSignalTestEvent(t, store, item, "pending-host-claim")
	hostID := "brain-agent-brain-hidden:@pending-host"
	claimed, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok {
		t.Fatalf("claim pending Host event ok=%v err=%v", ok, err)
	}
	acceptedAt := time.Date(2026, 8, 10, 7, 5, 0, 0, time.UTC)
	pending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: claimed.WorkID, SessionID: hostID, ProposedTurnID: claimed.ProviderTurnID,
		Receipt: claimed.ID, ClaimToken: claimed.HandlingID,
		PayloadSHA256:   pendingSubmissionDigest("pending claimed Host payload"),
		ProcessIdentity: "pending-host-process", PaneGeneration: "pending-host-pane",
		AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created || pending.State != watcher.TurnSubmissionPending {
		t.Fatalf("prepare pending Host claim pending=%+v created=%v err=%v", pending, created, err)
	}
	return root, store, claimed, pending
}

// A Host Session may retain an unrelated pending submission while startup
// reconciles an older claim whose exact receipt is absent. The complete Event
// capability, not ambient Session-wide pending state, is the release
// authority. Otherwise one stale submission can wedge the entire Host lane
// before foreground retirement and every later Work delivery.
func TestSignalAdversarialUnrelatedPendingHostSubmissionCannotBlockExactClaimRelease(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@unrelated-pending"
	firstWork := createSignalTestWork(t, store, "Release exact older claim", "brain-agent-worker:@older")
	appendSignalTestEvent(t, store, firstWork, "older-claim")
	secondWork := createSignalTestWork(t, store, "Keep unrelated pending submission isolated", "brain-agent-worker:@newer")
	appendSignalTestEvent(t, store, secondWork, "newer-claim")

	firstClaim, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || firstClaim.WorkID != firstWork.ID {
		t.Fatalf("first claim=%+v ok=%v err=%v", firstClaim, ok, err)
	}
	secondClaim, ok, err := store.ClaimNextActionableEvent(hostID)
	if err != nil || !ok || secondClaim.WorkID != secondWork.ID {
		t.Fatalf("second claim=%+v ok=%v err=%v", secondClaim, ok, err)
	}
	acceptedAt := time.Date(2026, 8, 11, 1, 25, 0, 0, time.UTC)
	pending, created, err := store.PrepareTurnSubmission(watcher.TurnSubmission{
		WorkID: secondClaim.WorkID, SessionID: hostID, ProposedTurnID: secondClaim.ProviderTurnID,
		Receipt: secondClaim.ID, ClaimToken: secondClaim.HandlingID,
		PayloadSHA256:   pendingSubmissionDigest("unrelated pending Host submission"),
		ProcessIdentity: "unrelated-pending-process", PaneGeneration: "unrelated-pending-pane",
		AcceptedAt: acceptedAt, Mode: watcher.TurnSubmissionFresh,
	})
	if err != nil || !created || pending.State != watcher.TurnSubmissionPending {
		t.Fatalf("pending submission=%+v created=%v err=%v", pending, created, err)
	}

	fixture := &fakeWatcher{
		turnStore: store,
		sessions: map[string]*classifier.Agent{
			hostID: {ID: hostID, Hidden: true, State: classifier.StateDone},
		},
	}
	service := NewService(store, fixture, nil)
	if err := service.reconcileDeliveryReceiptsLocked(); err != nil {
		t.Fatalf("unrelated pending submission wedged exact releases: %v", err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, claimed := range []WorkEvent{firstClaim, secondClaim} {
		event, found, eventErr := reopened.WorkEvent(claimed.ID)
		if eventErr != nil || !found || event.ClaimedAt != nil || event.HandlingID != "" ||
			event.DeliveryHostSessionID != "" || event.ProviderTurnID != "" {
			t.Fatalf("claim %s did not release exactly: event=%+v found=%v err=%v", claimed.ID, event, found, eventErr)
		}
	}
	resolvedPending, found, err := reopened.TurnSubmission(hostID, secondClaim.ProviderTurnID)
	if err != nil || !found || resolvedPending.State != watcher.TurnSubmissionAborted {
		t.Fatalf("exact pending submission was not aborted: submission=%+v found=%v err=%v", resolvedPending, found, err)
	}
}

// Missing transport receipt plus the unchanged frozen target proves that the
// provider mutation never began. The exact pending Host submission abort and
// exact Event-claim release must therefore be one orchestration replacement:
// reopen may expose only the complete held-before or complete released-after
// state. A resolved submission is ambiguous and remains held without replay.
func TestSignalAdversarialProvedUnsentHostSubmissionAndClaimAbortAtomicallyAcrossReopen(t *testing.T) {
	t.Run("write failure keeps complete held before-state", func(t *testing.T) {
		root, store, claimed, _ := preparePendingClaimedHostSubmission(t)
		store.writeOrchestration = func(string, any) error { return errors.New("injected Host abort-release failure") }
		if err := store.ReleaseEventClaim(
			claimed.ID, claimed.HandlingID, claimed.WorkID,
			claimed.DeliveryHostSessionID, claimed.ProviderTurnID,
		); err == nil {
			t.Fatal("injected Host abort-release failure was ignored")
		}
		reopened, err := NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		event, found, err := reopened.WorkEvent(claimed.ID)
		if err != nil || !found || event.ClaimedAt == nil || event.HandlingID != claimed.HandlingID ||
			event.DeliveryHostSessionID != claimed.DeliveryHostSessionID || event.ProviderTurnID != claimed.ProviderTurnID {
			t.Fatalf("write failure did not retain exact held Event: event=%+v found=%v err=%v", event, found, err)
		}
		submission, found, err := reopened.TurnSubmission(claimed.DeliveryHostSessionID, claimed.ProviderTurnID)
		if err != nil || !found || submission.State != watcher.TurnSubmissionPending ||
			submission.Receipt != claimed.ID || submission.ClaimToken != claimed.HandlingID || submission.WorkID != claimed.WorkID {
			t.Fatalf("write failure did not retain exact pending Host submission: submission=%+v found=%v err=%v", submission, found, err)
		}
	})

	t.Run("one write commits complete aborted released after-state", func(t *testing.T) {
		root, _, claimed, _ := preparePendingClaimedHostSubmission(t)
		reopened, err := NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		writes := 0
		write := reopened.writeOrchestration
		reopened.writeOrchestration = func(path string, value any) error {
			writes++
			if writes > 1 {
				return errors.New("Host abort-release attempted split persistence")
			}
			return write(path, value)
		}
		delivery := &fakeWatcher{sessions: map[string]*classifier.Agent{
			claimed.DeliveryHostSessionID: {ID: claimed.DeliveryHostSessionID, Hidden: true, State: classifier.StateRunning},
		}}
		if woke, err := NewService(reopened, delivery, nil).ReconcileHostLane(); err != nil || woke {
			t.Fatalf("reopen proved-unsent recovery woke=%v err=%v", woke, err)
		}
		if writes != 1 {
			t.Fatalf("Host abort-release writes=%d want 1", writes)
		}
		if len(delivery.sentCalls) != 0 {
			t.Fatalf("reopen replayed pending Host input: %+v", delivery.sentCalls)
		}
		reopenedAgain, err := NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		event, found, err := reopenedAgain.WorkEvent(claimed.ID)
		if err != nil || !found || event.ClaimedAt != nil || event.HandlingID != "" ||
			event.DeliveryHostSessionID != "" || event.ProviderTurnID != "" {
			t.Fatalf("atomic after-state did not release exact Event: event=%+v found=%v err=%v", event, found, err)
		}
		submission, found, err := reopenedAgain.TurnSubmission(claimed.DeliveryHostSessionID, claimed.ProviderTurnID)
		if err != nil || !found || submission.State != watcher.TurnSubmissionAborted {
			t.Fatalf("atomic after-state did not abort exact Host submission: submission=%+v found=%v err=%v", submission, found, err)
		}
		retry, ok, err := reopenedAgain.ClaimNextActionableEvent(claimed.DeliveryHostSessionID)
		if err != nil || !ok || retry.ID != claimed.ID || retry.HandlingID == claimed.HandlingID ||
			retry.ProviderTurnID == claimed.ProviderTurnID {
			t.Fatalf("released Event did not mint one fresh exact retry: retry=%+v ok=%v err=%v", retry, ok, err)
		}
	})

	t.Run("resolved provider admission remains held", func(t *testing.T) {
		root, store, claimed, pending := preparePendingClaimedHostSubmission(t)
		resolveDelegatedSubmission(
			t, store, pending, "ambiguous-host-activity",
			time.Date(2026, 8, 10, 7, 5, 1, 0, time.UTC),
		)
		writes := 0
		write := store.writeOrchestration
		store.writeOrchestration = func(path string, value any) error {
			writes++
			return write(path, value)
		}
		if err := store.ReleaseEventClaim(
			claimed.ID, claimed.HandlingID, claimed.WorkID,
			claimed.DeliveryHostSessionID, claimed.ProviderTurnID,
		); !errors.Is(err, ErrEventClaim) {
			t.Fatalf("resolved provider admission release err=%v want ErrEventClaim", err)
		}
		if writes != 0 {
			t.Fatalf("ambiguous resolved admission attempted %d persistence writes", writes)
		}
		delivery := &fakeWatcher{sessions: map[string]*classifier.Agent{
			claimed.DeliveryHostSessionID: {ID: claimed.DeliveryHostSessionID, Hidden: true, State: classifier.StateRunning},
		}}
		if woke, err := NewService(store, delivery, nil).ReconcileHostLane(); err != nil || woke {
			t.Fatalf("ambiguous resolved recovery woke=%v err=%v", woke, err)
		}
		if len(delivery.sentCalls) != 0 || writes != 0 {
			t.Fatalf("ambiguous resolved recovery replayed or wrote: sends=%+v writes=%d", delivery.sentCalls, writes)
		}
		reopened, err := NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		event, _, _ := reopened.WorkEvent(claimed.ID)
		submission, found, err := reopened.TurnSubmission(claimed.DeliveryHostSessionID, claimed.ProviderTurnID)
		if err != nil || !found || event.ClaimedAt == nil || submission.State != watcher.TurnSubmissionResolved {
			t.Fatalf("ambiguous provider admission was not held: event=%+v submission=%+v found=%v err=%v", event, submission, found, err)
		}
	})
}

// A retained owner link is never admission authority by itself. Admission
// requires exact canonical submission/Turn evidence, and reopening must not
// promote the same bare owner string into authority.
func TestSignalAdversarialBareOwnerCannotAdmitTurnAcrossReopen(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-bare-owner:@1"
	item, err := store.CreateWork(Work{
		Title: "Bare owner authority", Objective: "Reject owner text as Turn admission evidence.",
		Status: WorkRunning, OwnerSessionID: sessionID, OwnerDelegated: true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	bareAcceptedAt := time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC)
	candidate := delegatedSubmissionCandidate(
		"", sessionID, sessionID+":turn:1", "bare owner input",
		bareAcceptedAt,
	)
	if _, _, err := store.PrepareTurnSubmission(candidate); err == nil {
		t.Fatal("bare owner_session_id admitted a canonical Turn")
	}
	if _, found, err := store.Turn(sessionID); err != nil || found {
		t.Fatalf("rejected bare owner admission created Turn: found=%v err=%v", found, err)
	}

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.PrepareTurnSubmission(candidate); err == nil {
		t.Fatal("reopen promoted bare owner_session_id into admission authority")
	}
	if _, found, err := reopened.Turn(sessionID); err != nil || found {
		t.Fatalf("reopen rejection created Turn: found=%v err=%v", found, err)
	}
	after, err := reopened.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.OwnerSessionID != sessionID || after.Revision != item.Revision {
		t.Fatalf("rejected admission mutated retained owner Work: before=%+v after=%+v", item, after)
	}

	canonicalStore, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	canonicalWork, err := canonicalStore.CreateWork(Work{
		Title: "Canonical owner admission", Objective: "Admit through the exact submission ledger.",
		Status: WorkOpen, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	canonicalSessionID := "brain-agent-canonical-owner:@1"
	pending, created, err := canonicalStore.PrepareTurnSubmission(delegatedSubmissionCandidate(
		canonicalWork.ID,
		canonicalSessionID,
		canonicalSessionID+":turn:1",
		"canonical admission payload",
		bareAcceptedAt,
	))
	if err != nil || !created {
		t.Fatalf("prepare canonical submission: pending=%+v created=%v err=%v", pending, created, err)
	}
	resolved := resolveDelegatedSubmission(t, canonicalStore, pending, "canonical-activity", bareAcceptedAt.Add(time.Second))
	turn, found, err := canonicalStore.Turn(canonicalSessionID)
	if err != nil || !found || turn.TurnID != resolved.ResolvedTurnID || !turn.HasAdmission {
		t.Fatalf("canonical pending/resolved path did not admit exact Turn: turn=%+v found=%v resolved=%+v err=%v", turn, found, resolved, err)
	}
}

// Initial delegated ownership and its pending Turn submission are one durable
// authority transition. A failed replacement leaves the original ready Work;
// a committed replacement reopens with both the owner link and the canonical
// pending submission, never a naked owner string.
func TestSignalAdversarialInitialOwnerAdmissionAndPendingSubmissionAreAtomicAcrossReopen(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "Atomic initial owner admission", Objective: "Persist one authoritative launch boundary.",
		Status: WorkOpen, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if complete, processed, err := store.MigrateSignalSystemV1(8); err != nil || complete || processed != 1 {
		t.Fatalf("migration batch complete=%v processed=%d err=%v", complete, processed, err)
	}
	if complete, processed, err := store.MigrateSignalSystemV1(8); err != nil || !complete || processed != 0 {
		t.Fatalf("migration completion complete=%v processed=%d err=%v", complete, processed, err)
	}

	sessionID := "brain-agent-atomic-owner:@1"
	turnID := sessionID + ":turn:1"
	acceptedAt := time.Date(2026, 8, 10, 5, 30, 0, 0, time.UTC)
	candidate := delegatedSubmissionCandidate(item.ID, sessionID, turnID, "initial delegated payload", acceptedAt)
	originalWrite := store.writeOrchestration
	store.writeOrchestration = func(string, any) error {
		return errors.New("injected owner-admission persistence failure")
	}
	if _, _, err := store.PrepareTurnSubmission(candidate); err == nil {
		t.Fatal("failed owner-admission replacement was reported successful")
	}
	store.writeOrchestration = originalWrite

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	before, err := reopened.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.OwnerSessionID != "" || before.OwnerDelegated || before.Status != WorkOpen {
		t.Fatalf("failed atomic admission exposed owner state: %+v", before)
	}
	if _, found, err := reopened.TurnSubmission(sessionID, turnID); err != nil || found {
		t.Fatalf("failed atomic admission exposed pending submission: found=%v err=%v", found, err)
	}
	if projected := activeWorkByID(t, reopened, item.ID); projected.ProgressMode != WorkProgressReady || !projected.AttentionPending {
		t.Fatalf("failed atomic admission lost original ready progress: %+v", projected)
	}

	writes := 0
	reopenWrite := reopened.writeOrchestration
	reopened.writeOrchestration = func(path string, value any) error {
		writes++
		if writes > 1 {
			return errors.New("owner admission attempted split persistence")
		}
		return reopenWrite(path, value)
	}
	pending, created, err := reopened.PrepareTurnSubmission(candidate)
	if err != nil || !created || pending.State != watcher.TurnSubmissionPending {
		t.Fatalf("atomic owner admission=(%+v, %v, %v)", pending, created, err)
	}
	if writes != 1 {
		t.Fatalf("owner admission writes=%d want 1", writes)
	}

	reopenedAgain, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopenedAgain.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.OwnerSessionID != sessionID || !after.OwnerDelegated || after.Status != WorkRunning {
		t.Fatalf("committed atomic admission lost owner state: %+v", after)
	}
	if durable, found, err := reopenedAgain.TurnSubmission(sessionID, turnID); err != nil || !found || durable.State != watcher.TurnSubmissionPending || durable.WorkID != item.ID {
		t.Fatalf("committed atomic admission lost pending submission: submission=%+v found=%v err=%v", durable, found, err)
	}
	if _, found, err := reopenedAgain.Turn(sessionID); err != nil || found {
		t.Fatalf("pending admission fabricated canonical Turn: found=%v err=%v", found, err)
	}
	if projected := activeWorkByID(t, reopenedAgain, item.ID); projected.ProgressMode != WorkProgressOwned || projected.AttentionPending {
		t.Fatalf("pending admission is not the singular owner: %+v", projected)
	}
}

// A stable Session name is a UI container, not provider mutation authority.
// Once the watcher has admitted a newer exact pane/process generation, an
// ambiguous pending transaction from the replaced generation must remain
// non-adoptable audit without wedging the new generation forever. The
// replacement and retirement are one durable write; a concurrent input aimed
// at the same generation is still rejected.
func TestSignalAdversarialReplacedProviderGenerationRetiresOldPendingAtomically(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "Replaced provider generation", Objective: "Keep one mutation authority per exact provider generation.",
		Status: WorkOpen, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionID := "brain-agent-stable-session:@1"
	acceptedAt := time.Date(2026, 8, 11, 2, 0, 0, 0, time.UTC)
	oldCandidate := delegatedSubmissionCandidate(
		item.ID, sessionID, sessionID+":turn:old", "ambiguous old input", acceptedAt,
	)
	oldCandidate.ProcessIdentity = "old-process"
	oldCandidate.PaneGeneration = "old-pane"
	oldPending, created, err := store.PrepareTurnSubmission(oldCandidate)
	if err != nil || !created {
		t.Fatalf("prepare old generation created=%v submission=%+v err=%v", created, oldPending, err)
	}

	sameGeneration := delegatedSubmissionCandidate(
		item.ID, sessionID, sessionID+":turn:concurrent", "concurrent same-generation input", acceptedAt.Add(time.Second),
	)
	sameGeneration.ProcessIdentity = oldCandidate.ProcessIdentity
	sameGeneration.PaneGeneration = oldCandidate.PaneGeneration
	if _, created, err := store.PrepareTurnSubmission(sameGeneration); err == nil || created {
		t.Fatalf("same provider generation acquired concurrent authority: created=%v err=%v", created, err)
	}
	if durable, found, err := store.TurnSubmission(sessionID, oldCandidate.ProposedTurnID); err != nil || !found || durable.State != watcher.TurnSubmissionPending {
		t.Fatalf("same-generation rejection changed old authority: submission=%+v found=%v err=%v", durable, found, err)
	}

	replacement := delegatedSubmissionCandidate(
		item.ID, sessionID, sessionID+":turn:replacement", "replacement generation input", acceptedAt.Add(2*time.Second),
	)
	replacement.ProcessIdentity = "replacement-process"
	replacement.PaneGeneration = "replacement-pane"
	originalWrite := store.writeOrchestration
	store.writeOrchestration = func(string, any) error {
		return errors.New("injected generation-replacement persistence failure")
	}
	if _, created, err := store.PrepareTurnSubmission(replacement); err == nil || created {
		t.Fatalf("failed atomic replacement created=%v err=%v", created, err)
	}
	store.writeOrchestration = originalWrite

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if durable, found, err := reopened.TurnSubmission(sessionID, oldCandidate.ProposedTurnID); err != nil || !found || durable.State != watcher.TurnSubmissionPending {
		t.Fatalf("failed replacement retired old authority: submission=%+v found=%v err=%v", durable, found, err)
	}
	if _, found, err := reopened.TurnSubmission(sessionID, replacement.ProposedTurnID); err != nil || found {
		t.Fatalf("failed replacement persisted new authority: found=%v err=%v", found, err)
	}

	newPending, created, err := reopened.PrepareTurnSubmission(replacement)
	if err != nil || !created || newPending.State != watcher.TurnSubmissionPending {
		t.Fatalf("replacement generation created=%v submission=%+v err=%v", created, newPending, err)
	}
	reopenedAgain, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	oldDurable, oldFound, err := reopenedAgain.TurnSubmission(sessionID, oldCandidate.ProposedTurnID)
	if err != nil || !oldFound || oldDurable.State != watcher.TurnSubmissionRetired {
		t.Fatalf("old generation was not durably retired: submission=%+v found=%v err=%v", oldDurable, oldFound, err)
	}
	current, currentFound, err := reopenedAgain.PendingTurnSubmission(sessionID)
	if err != nil || !currentFound || current.ProposedTurnID != replacement.ProposedTurnID || current.State != watcher.TurnSubmissionPending {
		t.Fatalf("replacement is not the sole current authority: submission=%+v found=%v err=%v", current, currentFound, err)
	}
	if _, err := reopenedAgain.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: oldCandidate.SessionID, ProposedTurnID: oldCandidate.ProposedTurnID,
		Receipt: oldCandidate.Receipt, PayloadSHA256: oldCandidate.PayloadSHA256,
		ActivityID: "late-old-activity",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "late-old-admission", Cursor: 1,
			SHA256: oldCandidate.PayloadSHA256, At: acceptedAt.Add(3 * time.Second),
		},
		ResolvedAt: acceptedAt.Add(3 * time.Second),
	}); err == nil {
		t.Fatal("late evidence adopted the replaced provider generation")
	}
	if _, found, err := reopenedAgain.Turn(sessionID); err != nil || found {
		t.Fatalf("replaced provider generation created a canonical Turn: found=%v err=%v", found, err)
	}
}

// A correction that reuses the retained Session stages its accepted successor
// Turn under the delivered review Attention. Preparation and resolution may
// each survive a crash, but neither transfers progress ownership; only the
// exact continue disposition does so.
func TestSignalAdversarialSameSessionCorrectionStagesAcceptedTurnUntilExactContinue(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "Same-Session correction", Objective: "Transfer correction ownership only through continue.",
		Status: WorkOpen, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MigrateSignalSystemV1(8); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.MigrateSignalSystemV1(8); err != nil {
		t.Fatal(err)
	}

	sessionID := "brain-agent-same-session:@1"
	firstTurnID := sessionID + ":turn:1"
	firstAcceptedAt := time.Date(2026, 8, 10, 6, 0, 0, 0, time.UTC)
	first, created, err := store.PrepareTurnSubmission(
		delegatedSubmissionCandidate(item.ID, sessionID, firstTurnID, "initial result", firstAcceptedAt),
	)
	if err != nil || !created {
		t.Fatalf("prepare initial submission created=%v err=%v", created, err)
	}
	resolveDelegatedSubmission(t, store, first, "initial", firstAcceptedAt.Add(time.Second))
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: sessionID, TurnID: firstTurnID,
		Class: watcher.EvidenceProvider, Kind: "done", SourceID: "provider-initial-done",
		ActivityID: "initial", StartedAt: firstAcceptedAt,
		SettledAt: firstAcceptedAt.Add(2 * time.Second), At: firstAcceptedAt.Add(2 * time.Second),
	}); err != nil || !changed {
		t.Fatalf("terminal initial Turn changed=%v err=%v", changed, err)
	}
	delivered, ready := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	if ready.OwnerSessionID != sessionID || activeWorkByID(t, store, item.ID).ProgressMode != WorkProgressReady {
		t.Fatalf("review Attention did not retain historical Session linkage without ownership: %+v", ready)
	}

	abortedTurnID := sessionID + ":turn:2"
	abortedAcceptedAt := firstAcceptedAt.Add(time.Minute)
	abortedCandidate := delegatedSubmissionCandidate(item.ID, sessionID, abortedTurnID, "aborted correction", abortedAcceptedAt)
	abortedCandidate.ExistingTurnID = firstTurnID
	originalWrite := store.writeOrchestration
	store.writeOrchestration = func(string, any) error {
		return errors.New("injected correction prepare persistence failure")
	}
	if _, _, err := store.PrepareTurnSubmission(abortedCandidate); err == nil {
		t.Fatal("failed correction prepare was reported successful")
	}
	store.writeOrchestration = originalWrite

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := reopened.TurnSubmission(sessionID, abortedTurnID); err != nil || found {
		t.Fatalf("failed correction prepare survived: found=%v err=%v", found, err)
	}
	current, err := reopened.Work(item.ID)
	if err != nil || current.SuccessorReservation != nil || activeWorkByID(t, reopened, item.ID).ProgressMode != WorkProgressReady {
		t.Fatalf("failed correction prepare changed ready Work: Work=%+v err=%v", current, err)
	}

	aborted, created, err := reopened.PrepareTurnSubmission(abortedCandidate)
	if err != nil || !created {
		t.Fatalf("prepare same-Session correction created=%v err=%v", created, err)
	}
	preparedWork, err := reopened.Work(item.ID)
	if err != nil || preparedWork.SuccessorReservation == nil ||
		preparedWork.SuccessorReservation.SessionID != sessionID ||
		preparedWork.SuccessorReservation.EventID != delivered.ID ||
		preparedWork.Revision != delivered.DeliveryWorkRevision {
		t.Fatalf("same-Session correction was not staged under exact handling: Work=%+v err=%v", preparedWork, err)
	}
	if _, err := reopened.AbortTurnSubmission(
		sessionID, abortedTurnID, aborted.Receipt, aborted.PayloadSHA256,
	); err != nil {
		t.Fatalf("abort proved non-submission: %v", err)
	}
	afterAbort, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	abortedWork, err := afterAbort.Work(item.ID)
	if err != nil || abortedWork.SuccessorReservation != nil || activeWorkByID(t, afterAbort, item.ID).ProgressMode != WorkProgressReady {
		t.Fatalf("proved non-submission did not release same-Session staging: Work=%+v err=%v", abortedWork, err)
	}
	if durable, found, err := afterAbort.TurnSubmission(sessionID, abortedTurnID); err != nil || !found || durable.State != watcher.TurnSubmissionAborted {
		t.Fatalf("aborted correction did not survive reopen: submission=%+v found=%v err=%v", durable, found, err)
	}

	secondTurnID := sessionID + ":turn:3"
	secondAcceptedAt := abortedAcceptedAt.Add(time.Minute)
	secondCandidate := delegatedSubmissionCandidate(item.ID, sessionID, secondTurnID, "corrected result", secondAcceptedAt)
	secondCandidate.ExistingTurnID = firstTurnID
	second, created, err := afterAbort.PrepareTurnSubmission(secondCandidate)
	if err != nil || !created {
		t.Fatalf("prepare accepted same-Session correction created=%v err=%v", created, err)
	}

	reopenWrite := afterAbort.writeOrchestration
	afterAbort.writeOrchestration = func(string, any) error {
		return errors.New("injected accepted correction persistence failure")
	}
	if _, err := afterAbort.ResolveTurnSubmission(watcher.TurnSubmissionResolution{
		SessionID: sessionID, ProposedTurnID: secondTurnID, Receipt: second.Receipt,
		PayloadSHA256: second.PayloadSHA256, ActivityID: "correction",
		Admission: watcher.TurnAdmission{
			Stream: "provider", ID: "admission-correction", Cursor: 2,
			SHA256: second.PayloadSHA256, At: secondAcceptedAt.Add(time.Second),
		},
		ResolvedAt: secondAcceptedAt.Add(time.Second),
	}); err == nil {
		t.Fatal("failed accepted-correction replacement was reported successful")
	}
	afterAbort.writeOrchestration = reopenWrite

	reopenedAgain, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if pending, found, err := reopenedAgain.TurnSubmission(sessionID, secondTurnID); err != nil || !found || pending.State != watcher.TurnSubmissionPending {
		t.Fatalf("failed correction resolution lost pending row: submission=%+v found=%v err=%v", pending, found, err)
	}
	if turn, found, err := reopenedAgain.Turn(sessionID); err != nil || !found || turn.TurnID != firstTurnID {
		t.Fatalf("failed correction resolution replaced canonical Turn: turn=%+v found=%v err=%v", turn, found, err)
	}
	resolved := resolveDelegatedSubmission(t, reopenedAgain, second, "correction", secondAcceptedAt.Add(time.Second))
	if resolved.ResolvedTurnID != secondTurnID {
		t.Fatalf("resolved correction Turn=%+v", resolved)
	}
	acceptedWork, err := reopenedAgain.Work(item.ID)
	if err != nil || acceptedWork.SuccessorReservation == nil || acceptedWork.SuccessorReservation.ProviderTurnID != secondTurnID ||
		acceptedWork.OwnerSessionID != sessionID || acceptedWork.Revision != delivered.DeliveryWorkRevision {
		t.Fatalf("accepted correction transferred ownership before continue: Work=%+v err=%v", acceptedWork, err)
	}
	if projected := activeWorkByID(t, reopenedAgain, item.ID); projected.ProgressMode != WorkProgressReady || !projected.AttentionPending {
		t.Fatalf("accepted correction counted as a second progress owner: %+v", projected)
	}

	_, continued, err := reopenedAgain.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionContinue, SuccessorSessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("exact continue rejected accepted same-Session correction: %v", err)
	}
	if continued.OwnerSessionID != sessionID || !continued.OwnerDelegated || continued.SuccessorReservation != nil {
		t.Fatalf("exact continue did not transfer same-Session ownership: %+v", continued)
	}
	if projected := activeWorkByID(t, reopenedAgain, item.ID); projected.ProgressMode != WorkProgressOwned || projected.AttentionPending {
		t.Fatalf("continued same-Session correction is not singularly owned: %+v", projected)
	}
}

// The Watcher emits changed output before the authoritative state transition.
// Only the latter, bound to the exact accepted provider Turn, may end A. A
// duplicate snapshot must not end the newly delivered B handling.
func TestSignalAdversarialWatcherOutputThenStateChangeClosesOnlyExactHandling(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	a := createSignalTestWork(t, store, "Watcher A", "brain-agent-a:@1")
	b := createSignalTestWork(t, store, "Watcher B", "brain-agent-b:@1")
	appendSignalTestEvent(t, store, a, "watcher-a")
	appendSignalTestEvent(t, store, b, "watcher-b")
	deliveredA, _ := deliverSignalTestEvent(t, store, hostID)
	host := &classifier.Agent{ID: hostID, Hidden: true, State: classifier.StateDone}
	fw := &fakeWatcher{turnStore: store, sessions: map[string]*classifier.Agent{hostID: host}}
	service := NewService(store, fw, nil)

	if woke, err := service.ObserveHostSessionEvent(watcher.SessionEvent{
		Type: "agent_output", AgentID: hostID, Agent: host,
	}); err != nil || woke {
		t.Fatalf("output snapshot closed a handling: woke=%v err=%v", woke, err)
	}
	row, _, _ := store.WorkEvent(deliveredA.ID)
	if row.HandlingEndedAt != nil {
		t.Fatalf("output snapshot ended A: %+v", row)
	}

	terminal := watcher.SessionEvent{
		Type: "agent_state_change", AgentID: hostID, Agent: host,
		OldState: string(classifier.StateRunning), NewState: string(classifier.StateDone),
		TurnID: deliveredA.ProviderTurnID,
	}
	if _, err := service.ObserveHostSessionEvent(terminal); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListWorkEvents("")
	if err != nil {
		t.Fatal(err)
	}
	var deliveredB WorkEvent
	for _, event := range events {
		if event.WorkID == b.ID && event.DeliveredAt != nil && event.HandlingEndedAt == nil {
			deliveredB = event
		}
	}
	if deliveredB.ID == "" {
		t.Fatalf("B was not delivered after exact A terminal: %+v", events)
	}
	if _, err := service.ObserveHostSessionEvent(terminal); err != nil {
		t.Fatal(err)
	}
	row, _, _ = store.WorkEvent(deliveredB.ID)
	if row.HandlingEndedAt != nil {
		t.Fatalf("duplicate A terminal ended B: %+v", row)
	}
}

func TestSignalAdversarialOnlyOneDeliveredHostHandlingGlobally(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a := createSignalTestWork(t, store, "Global A", "brain-agent-a:@1")
	b := createSignalTestWork(t, store, "Global B", "brain-agent-b:@1")
	appendSignalTestEvent(t, store, a, "global-a")
	appendSignalTestEvent(t, store, b, "global-b")
	deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	if event, claimed, err := store.ClaimNextActionableEvent("brain-agent-brain-hidden:@1"); err != nil || claimed {
		t.Fatalf("second Work entered Host admission window: event=%+v claimed=%v err=%v", event, claimed, err)
	}
}

func TestSignalAdversarialStartupRecoversOldHostDeliveryAfterBindingReplacement(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	oldHost := "brain-agent-brain-hidden:@1"
	newHost := "brain-agent-brain-hidden:@2"
	item := createSignalTestWork(t, store, "Old Host delivery", "brain-agent-worker:@1")
	appendSignalTestEvent(t, store, item, "old-host")
	delivered, _ := deliverSignalTestEvent(t, store, oldHost)
	if err := store.SetHostSession(newHost, "codex"); err != nil {
		t.Fatal(err)
	}

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	fw := &fakeWatcher{turnStore: restarted, sessions: map[string]*classifier.Agent{
		newHost: {ID: newHost, Hidden: true, State: classifier.StateDone},
	}}
	service := NewService(restarted, fw, nil)
	complete, err := service.ReconcileSignalSystemStartup(fw.Agents(), 8)
	if err != nil || !complete {
		t.Fatalf("startup complete=%v err=%v", complete, err)
	}
	row, _, err := restarted.WorkEvent(delivered.ID)
	if err != nil {
		t.Fatal(err)
	}
	if row.DeliveredAt == nil || row.HandlingEndedAt == nil || row.HandledAt != nil {
		t.Fatalf("old Host delivery was stranded or replayed: %+v", row)
	}
	events, _ := restarted.ListWorkEvents(item.ID)
	if !containsUnhandledEventKind(events, "brain.reconcile_required") {
		t.Fatalf("old Host Work has no durable reconcile attention: %+v", events)
	}
}

func TestSignalAdversarialWrongReorderedAndDuplicateProviderTurnsAreIgnored(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	item := createSignalTestWork(t, store, "Provider turn CAS", "brain-agent-worker:@1")
	appendSignalTestEvent(t, store, item, "provider-cas")
	delivered, _ := deliverSignalTestEvent(t, store, hostID)
	if delivered.HandlingID == delivered.ProviderTurnID {
		t.Fatalf("random handling token aliased provider Turn: %+v", delivered)
	}
	if _, created, err := store.RequeueUnhandledHostAttention(
		delivered.ID, delivered.HandlingID, "wrong-provider-turn",
	); err != nil || created {
		t.Fatalf("wrong provider Turn changed handling: created=%v err=%v", created, err)
	}
	if _, created, err := store.RequeueUnhandledHostAttention(
		delivered.ID, delivered.HandlingID, delivered.ProviderTurnID,
	); err != nil || !created {
		t.Fatalf("exact provider Turn did not requeue: created=%v err=%v", created, err)
	}
	if _, created, err := store.RequeueUnhandledHostAttention(
		delivered.ID, delivered.HandlingID, delivered.ProviderTurnID,
	); err != nil || created {
		t.Fatalf("duplicate provider Turn was not idempotent: created=%v err=%v", created, err)
	}
}

func TestSignalAdversarialProgressModeIsExactlyOneAcrossReadyWaitWakeAndContinue(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	owner := "brain-agent-progress:@1"
	item, err := store.CreateWork(Work{
		Title: "Progress modes", Objective: "Exercise each exclusive progress owner.",
		Status: WorkNeedsInput, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	appendSignalTestEvent(t, store, item, "progress-ready")
	delivered, current := deliverSignalTestEvent(t, store, hostID)
	if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressReady {
		t.Fatalf("delivered attention mode=%q projection=%+v", projected.ProgressMode, projected)
	}
	wake := &WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + current.SourceThreadID}
	_, waiting := resolveAdversarialEvent(t, store, delivered, WorkDispositionWait, wake, "")
	if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressWaiting || projected.AttentionPending {
		t.Fatalf("wait mode projection=%+v Work=%+v", projected, waiting)
	}
	if err := store.SetHostSession(hostID, "codex"); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)
	prepared, created, err := service.PrepareHostUserInput(hostID, "progress-input-1", "continue", wake.Ref)
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	if err := service.AdmitHostUserInput(prepared); err != nil {
		t.Fatal(err)
	}
	if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressReady || !projected.AttentionPending {
		t.Fatalf("wake mode projection=%+v", projected)
	}
	next, _ := deliverSignalTestEvent(t, store, hostID)
	if _, err := store.ReserveWorkSuccessor(item.ID, owner); err != nil {
		t.Fatal(err)
	}
	turnID := owner + ":turn:1"
	acceptedAt := time.Now().UTC()
	seedContinueAuthorityTurn(t, store, item.ID, owner, turnID, watcher.TurnAccepted, watcher.TurnAdmission{
		Stream: "provider", ID: "admission-" + turnID, Cursor: 1,
		SHA256: pendingSubmissionDigest("continue authority " + turnID), At: acceptedAt.Add(time.Second),
	}, acceptedAt)
	bindContinueAuthorityReservation(t, store, item.ID, owner, turnID)
	_, owned := resolveAdversarialEvent(t, store, next, WorkDispositionContinue, nil, owner)
	if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressOwned || projected.AttentionPending || projected.Wake != nil {
		t.Fatalf("continue mode projection=%+v Work=%+v", projected, owned)
	}
}

// A wait cannot hide a still-live canonical execution owner behind the
// projected waiting enum. Rejection must leave the exact Turn, Work owner,
// wake, and handling unchanged.
func TestSignalAdversarialWaitRejectsLiveCanonicalOwner(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	owner := "brain-agent-live-owner:@1"
	item, err := store.CreateWork(Work{
		Title: "Live owner cannot wait", Objective: "Reject a second progress owner.",
		Status: WorkWaiting, OwnerSessionID: owner, OwnerDelegated: true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	appendSignalTestEvent(t, store, item, "live-owner-review")
	delivered, current := deliverSignalTestEvent(t, store, hostID)
	ownerTurnID := owner + ":turn:1"
	bootstrapAdmittedTurnFixture(t, store, item.ID, watcher.AdmittedTurn{
		SessionID: owner, TurnID: ownerTurnID, AcceptedAt: time.Now().UTC(),
	})
	wake := &WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + current.SourceThreadID}
	if _, _, err := store.ResolveWorkEvent(WorkEventDispositionRequest{
		EventID: delivered.ID, HandlingID: delivered.HandlingID,
		ProviderTurnID:       delivered.ProviderTurnID,
		ExpectedWorkRevision: delivered.DeliveryWorkRevision,
		Disposition:          WorkDispositionWait,
		Wake:                 wake,
	}); err == nil {
		t.Fatal("wait disposition hid a live canonical owner Turn")
	}
	after, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.OwnerSessionID != owner || after.Wake != nil || after.Revision != current.Revision {
		t.Fatalf("rejected wait mutated Work: before=%+v after=%+v", current, after)
	}
	turn, found, err := store.TurnByID(owner, ownerTurnID)
	if err != nil || !found || watcher.TurnImmutable(turn.Status) {
		t.Fatalf("rejected wait changed live owner Turn: turn=%+v found=%v err=%v", turn, found, err)
	}
	row, found, err := store.WorkEvent(delivered.ID)
	if err != nil || !found || row.HandledAt != nil || row.HandlingEndedAt != nil {
		t.Fatalf("rejected wait consumed handling: event=%+v found=%v err=%v", row, found, err)
	}
}

func TestSignalAdversarialCanonicalAttentionIsOrthogonalToLiveOwner(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	owner := "brain-agent-canonical-attention:@1"
	item, err := store.CreateWork(Work{
		Title:     "Canonical attention owner transition",
		Objective: "Keep exactly one progress owner through delegated attention.",
		Status:    WorkRunning, OwnerSessionID: owner, OwnerDelegated: true,
		CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	acceptedAt := time.Now().UTC().Add(-time.Second)
	turnID := owner + ":turn:1"
	seedContinueAuthorityTurn(t, store, item.ID, owner, turnID, watcher.TurnAccepted, watcher.TurnAdmission{
		Stream: "provider", ID: "admission-" + turnID, Cursor: 1,
		SHA256: pendingSubmissionDigest("continue authority " + turnID), At: acceptedAt.Add(time.Second),
	}, acceptedAt)
	if complete, processed, err := store.MigrateSignalSystemV1(8); err != nil || complete || processed != 1 {
		t.Fatalf("migration batch complete=%v processed=%d err=%v", complete, processed, err)
	}
	if complete, processed, err := store.MigrateSignalSystemV1(8); err != nil || !complete || processed != 0 {
		t.Fatalf("migration completion complete=%v processed=%d err=%v", complete, processed, err)
	}
	if _, changed, err := store.ApplyTurnFact(watcher.TurnFact{
		SessionID: owner, TurnID: turnID,
		Class: watcher.EvidenceControl, Kind: "attention", SourceID: "attention-1",
		At: acceptedAt.Add(time.Second), Summary: "Delegated Session needs input",
	}); err != nil || !changed {
		t.Fatalf("canonical attention changed=%v err=%v", changed, err)
	}
	ready, err := store.Work(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ready.OwnerSessionID != owner || !ready.OwnerDelegated {
		t.Fatalf("canonical attention deprojected the exact live execution owner: %+v", ready)
	}
	if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressOwned || !projected.AttentionPending {
		t.Fatalf("canonical attention was not orthogonal to owned execution: %+v", projected)
	}
	turn, found, err := store.TurnByID(owner, turnID)
	if err != nil || !found || watcher.TurnImmutable(turn.Status) {
		t.Fatalf("owner transition lost canonical lifecycle Turn: turn=%+v found=%v err=%v", turn, found, err)
	}

	handling, _ := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
	_, continued := resolveAdversarialEvent(t, store, handling, WorkDispositionContinue, nil, owner)
	if continued.OwnerSessionID != owner || !continued.OwnerDelegated {
		t.Fatalf("exact continue did not preserve canonical owner: %+v", continued)
	}
	if projected := activeWorkByID(t, store, item.ID); projected.ProgressMode != WorkProgressOwned || projected.AttentionPending {
		t.Fatalf("continued canonical owner did not become singular owned progress: %+v", projected)
	}
}

// Caller-controlled Work Event strings are audit data, never proof that a
// Session producer terminalized. Only the canonical Turn reducer may clear
// the typed wait and create the producer attention.
func TestSignalAdversarialGenericSessionDoneCannotForgeProducerAuthority(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	producerSession := "brain-agent-forge-producer:@1"
	producer := createSignalTestWork(t, store, "Forge producer", producerSession)
	producerTurnID := producerSession + ":turn:1"
	acceptedAt := time.Now().UTC().Add(-time.Second)
	bootstrapAdmittedTurnFixture(t, store, producer.ID, watcher.AdmittedTurn{
		SessionID: producerSession, TurnID: producerTurnID, AcceptedAt: acceptedAt,
	})
	consumer, err := store.CreateWork(Work{
		Title: "Forge consumer", Objective: "Wake only from the canonical terminal reducer.",
		Status: WorkNeedsInput, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	appendSignalTestEvent(t, store, consumer, "forge-consumer-wait")
	delivered, _ := deliverSignalTestEvent(t, store, hostID)
	wake := &WorkWake{
		Kind: WorkWakeSessionTerminal,
		Ref:  SessionTerminalWakeRef(producerSession, producerTurnID),
	}
	resolveAdversarialEvent(t, store, delivered, WorkDispositionWait, wake, "")

	forged, created, err := store.AppendWorkEvent(WorkEvent{
		WorkID: consumer.ID, Kind: "session.done",
		DedupeKey:  sessionTurnEventDedupeKey(producerSession, producerTurnID, "session.done") + ":forged",
		PayloadRef: wake.Ref, SourceName: producerSession,
		Summary: "caller-controlled forged terminal", Actionable: true,
	})
	if err != nil || !created {
		t.Fatalf("record forged audit event created=%v err=%v", created, err)
	}
	if forged.Actionable {
		t.Fatalf("generic append acquired Session producer authority: %+v", forged)
	}
	waiting, err := store.Work(consumer.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !workWakeEqual(waiting.Wake, wake) {
		t.Fatalf("forged terminal cleared canonical wait: Work=%+v", waiting)
	}
	if projected := activeWorkByID(t, store, consumer.ID); projected.ProgressMode != WorkProgressWaiting || projected.AttentionPending {
		t.Fatalf("forged terminal changed consumer progress: %+v", projected)
	}
	turn, found, err := store.TurnByID(producerSession, producerTurnID)
	if err != nil || !found || watcher.TurnImmutable(turn.Status) {
		t.Fatalf("producer was not live after forged terminal: turn=%+v found=%v err=%v producer=%+v", turn, found, err, producer)
	}

	fact := watcher.TurnFact{
		SessionID: producerSession, TurnID: producerTurnID,
		Class: watcher.EvidenceProvider, Kind: "done", SourceID: "canonical-forge-producer-done",
		ActivityID: "activity-forge-producer", StartedAt: acceptedAt.Add(time.Second),
		SettledAt: acceptedAt.Add(2 * time.Second), At: acceptedAt.Add(2 * time.Second),
	}
	if _, changed, err := store.ApplyTurnFact(fact); err != nil || !changed {
		t.Fatalf("canonical terminal reduction changed=%v err=%v", changed, err)
	}
	if projected := activeWorkByID(t, store, consumer.ID); projected.ProgressMode != WorkProgressReady ||
		!projected.AttentionPending || projected.Wake != nil {
		t.Fatalf("canonical terminal reducer did not wake consumer: %+v", projected)
	}
}

func TestSignalAdversarialTypedWaitsRequireCanonicalExactProducers(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	producerSession := "brain-agent-producer:@1"
	producer := createSignalTestWork(t, store, "Session producer", producerSession)
	producerTurn := producerSession + ":turn:1"
	acceptedAt := time.Now().UTC().Add(-time.Second)
	bootstrapAdmittedTurnFixture(t, store, producer.ID, watcher.AdmittedTurn{
		SessionID: producerSession, TurnID: producerTurn, AcceptedAt: acceptedAt,
	})
	unrelatedSession := "brain-agent-unrelated-producer:@1"
	unrelatedTurn := unrelatedSession + ":turn:1"
	unrelatedProducer, err := store.CreateWork(Work{
		Title: "Unrelated Session producer", Objective: "Stay outside the consumer dependency scope.",
		Status: WorkRunning, OwnerSessionID: unrelatedSession, OwnerDelegated: true,
		SourceThreadID: "brain-thread-unrelated", CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	bootstrapAdmittedTurnFixture(t, store, unrelatedProducer.ID, watcher.AdmittedTurn{
		SessionID: unrelatedSession, TurnID: unrelatedTurn, AcceptedAt: acceptedAt,
	})
	calendarProducer, err := store.CreateWork(Work{
		ID:    calendarWorkID("item-1", "run-1"),
		Title: "Calendar producer", Objective: "Produce one exact occurrence.",
		Status: WorkRunning, CompletionPolicy: CompletionBounded,
		ContextRef: "calendar:item-1:run-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = calendarProducer
	if _, err := store.CreateWork(Work{
		ID:    calendarWorkID("item-unrelated", "run-1"),
		Title: "Unrelated Calendar producer", Objective: "Stay outside the consumer dependency scope.",
		Status: WorkRunning, OwnerSessionID: "brain-agent-calendar-unrelated:@1", OwnerDelegated: true,
		SourceThreadID: "brain-thread-unrelated", CompletionPolicy: CompletionBounded,
		ContextRef: "calendar:item-unrelated:run-1",
	}); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name string
		wake WorkWake
		ok   bool
	}{
		{name: "session exact", wake: WorkWake{Kind: WorkWakeSessionTerminal, Ref: SessionTerminalWakeRef(producerSession, producerTurn)}, ok: true},
		{name: "session missing", wake: WorkWake{Kind: WorkWakeSessionTerminal, Ref: SessionTerminalWakeRef("missing", "turn")}},
		{name: "session unrelated", wake: WorkWake{Kind: WorkWakeSessionTerminal, Ref: SessionTerminalWakeRef(unrelatedSession, unrelatedTurn)}},
		{name: "calendar exact", wake: WorkWake{Kind: WorkWakeCalendarResult, Ref: "calendar:item-1:run-1"}, ok: true},
		{name: "calendar missing", wake: WorkWake{Kind: WorkWakeCalendarResult, Ref: "calendar:item-2:run-9"}},
		{name: "calendar unrelated", wake: WorkWake{Kind: WorkWakeCalendarResult, Ref: "calendar:item-unrelated:run-1"}},
		{name: "user exact", wake: WorkWake{Kind: WorkWakeUserInput}, ok: true},
		{name: "user cross thread", wake: WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:not-the-work-thread"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			consumer, err := store.CreateWork(Work{
				Title: "Consumer " + test.name, Objective: "Wait on one canonical producer.",
				Status: WorkNeedsInput, CompletionPolicy: CompletionBounded,
			})
			if err != nil {
				t.Fatal(err)
			}
			appendSignalTestEvent(t, store, consumer, "wait-"+strings.ReplaceAll(test.name, " ", "-"))
			delivered, current := deliverSignalTestEvent(t, store, hostID)
			wake := test.wake
			if wake.Kind == WorkWakeUserInput && wake.Ref == "" {
				wake.Ref = "brain-thread:" + current.SourceThreadID
			}
			_, _, err = store.ResolveWorkEvent(WorkEventDispositionRequest{
				EventID: delivered.ID, HandlingID: delivered.HandlingID,
				ProviderTurnID:       delivered.ProviderTurnID,
				ExpectedWorkRevision: delivered.DeliveryWorkRevision,
				Disposition:          WorkDispositionWait, Wake: &wake,
			})
			if test.ok && err != nil {
				t.Fatalf("exact producer rejected: %v", err)
			}
			if !test.ok && err == nil {
				t.Fatalf("invalid producer accepted: %+v", wake)
			}
			if !test.ok {
				if _, _, endErr := store.RequeueUnhandledHostAttention(delivered.ID, delivered.HandlingID, delivered.ProviderTurnID); endErr != nil {
					t.Fatal(endErr)
				}
				reconcile, _ := deliverSignalTestEvent(t, store, hostID)
				resolveAdversarialEvent(t, store, reconcile, WorkDispositionComplete, nil, "")
			}
		})
	}

	consumer, err := store.CreateWork(Work{
		Title: "Cross-Work Session consumer", Objective: "Wake when the producer Turn settles.",
		Status: WorkNeedsInput, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	appendSignalTestEvent(t, store, consumer, "cross-work-session")
	delivered, _ := deliverSignalTestEvent(t, store, hostID)
	sessionWake := &WorkWake{Kind: WorkWakeSessionTerminal, Ref: SessionTerminalWakeRef(producerSession, producerTurn)}
	resolveAdversarialEvent(t, store, delivered, WorkDispositionWait, sessionWake, "")
	fact := watcher.TurnFact{
		SessionID: producerSession, TurnID: producerTurn,
		Class: watcher.EvidenceProvider, Kind: "done", SourceID: "provider-session-producer-done",
		ActivityID: "activity-producer", StartedAt: acceptedAt.Add(time.Second),
		SettledAt: acceptedAt.Add(2 * time.Second), At: acceptedAt.Add(2 * time.Second),
	}
	if _, _, err := store.ApplyTurnFact(fact); err != nil {
		t.Fatal(err)
	}
	if projected := activeWorkByID(t, store, consumer.ID); projected.ProgressMode != WorkProgressReady || projected.Wake != nil {
		t.Fatalf("cross-Work terminal did not wake consumer: %+v producer=%+v", projected, producer)
	}
	if _, _, err := store.ApplyTurnFact(fact); err != nil {
		t.Fatal(err)
	}
	events, _ := store.ListWorkEvents(consumer.ID)
	if countUnhandledEventKind(events, "session.done") != 1 {
		t.Fatalf("duplicate producer occurrence woke more than once: %+v", events)
	}
	for {
		claimed, ok, err := store.ClaimNextActionableEvent(hostID)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		resolveClaimedHostTurnForTest(t, store, claimed)
		ready, _, err := store.ConsumeClaimedWorkEvent(claimed.ID, claimed.HandlingID, claimed.WorkID, hostID, claimed.ProviderTurnID)
		if err != nil {
			t.Fatal(err)
		}
		resolveAdversarialEvent(t, store, ready, WorkDispositionComplete, nil, "")
	}

	calendarConsumer, err := store.CreateWork(Work{
		Title: "Cross-Work Calendar consumer", Objective: "Wake on the exact Calendar occurrence.",
		Status: WorkNeedsInput, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	appendSignalTestEvent(t, store, calendarConsumer, "cross-work-calendar")
	calendarHandling, _ := deliverSignalTestEvent(t, store, hostID)
	calendarWake := &WorkWake{Kind: WorkWakeCalendarResult, Ref: calendarProducer.ContextRef}
	resolveAdversarialEvent(t, store, calendarHandling, WorkDispositionWait, calendarWake, "")
	finished := time.Now().UTC()
	calendarEvent := calendar.Event{Item: calendar.Item{
		ID: "item-1", Title: "Calendar producer", Kind: calendar.KindScheduledAction,
		ActionInstruction: "Produce one exact occurrence.",
		Runs: []calendar.Run{{
			ID: "run-1", Title: "Calendar producer", Status: calendar.StatusCompleted,
			FinishedAt: &finished,
		}},
	}}
	calendarService := NewService(store, &fakeWatcher{}, nil)
	writes := 0
	write := store.writeOrchestration
	store.writeOrchestration = func(path string, value any) error {
		writes++
		return write(path, value)
	}
	if woke, err := calendarService.RouteCalendarEvent(calendarEvent); err != nil || !woke {
		t.Fatalf("Calendar producer wake=%v err=%v", woke, err)
	}
	if writes != 1 {
		t.Fatalf("Calendar producer transition and consumer attention used %d persistence writes, want 1", writes)
	}
	store.writeOrchestration = write
	if projected := activeWorkByID(t, store, calendarConsumer.ID); projected.ProgressMode != WorkProgressReady || projected.Wake != nil {
		t.Fatalf("cross-Work Calendar completion did not wake consumer: %+v", projected)
	}
	if woke, err := calendarService.RouteCalendarEvent(calendarEvent); err != nil || woke {
		t.Fatalf("duplicate Calendar occurrence wake=%v err=%v", woke, err)
	}
	calendarEvents, _ := store.ListWorkEvents(calendarConsumer.ID)
	if countUnhandledEventKind(calendarEvents, "calendar.result") != 1 {
		t.Fatalf("duplicate Calendar occurrence woke more than once: %+v", calendarEvents)
	}
}

func TestSignalAdversarialHostUserInputAdmissionFaultReopenMatrix(t *testing.T) {
	newFixture := func(t *testing.T) (*Store, *Service, Work, string, string, string) {
		t.Helper()
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		threadID := "thread-first-admission"
		hostID := "brain-agent-brain-hidden:@admission"
		requestID := "request-first-admission"
		if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
			t.Fatal(err)
		}
		if err := store.SetHostSession(hostID, "codex"); err != nil {
			t.Fatal(err)
		}
		item, err := store.CreateWork(Work{
			Title: "Wait for admitted user input", Objective: "Wake on one exact Brain thread input.",
			Status: WorkWaiting, CompletionPolicy: CompletionBounded,
			Wake: &WorkWake{Kind: WorkWakeUserInput, Ref: "brain-thread:" + threadID},
		})
		if err != nil {
			t.Fatal(err)
		}
		return store, NewService(store, nil, nil), item, hostID, threadID, requestID
	}
	assertBefore := func(t *testing.T, store *Store, item Work, threadID, requestID string, wantIntent bool) {
		t.Helper()
		admission, found, err := store.BrainInputAdmission(requestID, threadID)
		if err != nil {
			t.Fatal(err)
		}
		if found != wantIntent {
			t.Fatalf("admission found=%v want=%v row=%+v", found, wantIntent, admission)
		}
		if found && admission.State != BrainInputAdmissionPending {
			t.Fatalf("ambiguous admission state=%q want pending", admission.State)
		}
		current, err := store.Work(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status != WorkWaiting || current.Wake == nil ||
			current.Wake.Kind != WorkWakeUserInput || current.Wake.Ref != "brain-thread:"+threadID {
			t.Fatalf("before-state wait changed: %+v", current)
		}
		events, err := store.ListWorkEvents(item.ID)
		if err != nil || len(events) != 0 {
			t.Fatalf("before-state Attention=%+v err=%v", events, err)
		}
		items, err := store.ThreadTimeline(threadID, 0)
		if err != nil || len(items) != 0 {
			t.Fatalf("before-state timeline=%+v err=%v", items, err)
		}
	}

	t.Run("intent write failure", func(t *testing.T) {
		store, service, item, hostID, threadID, requestID := newFixture(t)
		store.writeOrchestration = func(string, any) error { return errors.New("injected intent write failure") }
		if _, _, err := service.PrepareHostUserInput(hostID, requestID, "first accepted body", "brain-thread:"+threadID); err == nil {
			t.Fatal("intent persistence failure was ignored")
		}
		reopened, err := NewStore(store.Root)
		if err != nil {
			t.Fatal(err)
		}
		complete, err := NewService(reopened, nil, nil).ReconcileSignalSystemStartup(nil, 8)
		if err != nil || !complete {
			t.Fatalf("startup complete=%v err=%v", complete, err)
		}
		assertBefore(t, reopened, item, threadID, requestID, false)
	})

	t.Run("crash after provider acceptance", func(t *testing.T) {
		store, service, item, hostID, threadID, requestID := newFixture(t)
		admission, created, err := service.PrepareHostUserInput(hostID, requestID, "first accepted body", "brain-thread:"+threadID)
		if err != nil || !created || admission.State != BrainInputAdmissionPending {
			t.Fatalf("prepare created=%v admission=%+v err=%v", created, admission, err)
		}
		// The provider accepted here, but the process stopped before committing
		// the exact result. Pending is a durable no-replay hold, not acceptance.
		reopened, err := NewStore(store.Root)
		if err != nil {
			t.Fatal(err)
		}
		restarted := NewService(reopened, nil, nil)
		complete, err := restarted.ReconcileSignalSystemStartup(nil, 8)
		if err != nil || !complete {
			t.Fatalf("startup complete=%v err=%v", complete, err)
		}
		assertBefore(t, reopened, item, threadID, requestID, true)
		duplicate, duplicateCreated, err := restarted.PrepareHostUserInput(hostID, requestID, "first accepted body", "brain-thread:"+threadID)
		if err != nil || duplicateCreated || duplicate.State != BrainInputAdmissionPending {
			t.Fatalf("duplicate pending prepare created=%v admission=%+v err=%v", duplicateCreated, duplicate, err)
		}
	})

	t.Run("accepted plus Attention write failure", func(t *testing.T) {
		store, service, item, hostID, threadID, requestID := newFixture(t)
		prepared, created, err := service.PrepareHostUserInput(hostID, requestID, "first accepted body", "brain-thread:"+threadID)
		if err != nil || !created {
			t.Fatalf("prepare created=%v err=%v", created, err)
		}
		store.writeOrchestration = func(string, any) error { return errors.New("injected accepted admission write failure") }
		if err := service.AdmitHostUserInput(prepared); err == nil {
			t.Fatal("accepted admission persistence failure was ignored")
		}
		reopened, err := NewStore(store.Root)
		if err != nil {
			t.Fatal(err)
		}
		complete, err := NewService(reopened, nil, nil).ReconcileSignalSystemStartup(nil, 8)
		if err != nil || !complete {
			t.Fatalf("startup complete=%v err=%v", complete, err)
		}
		assertBefore(t, reopened, item, threadID, requestID, true)
	})

	t.Run("timeline projection failure", func(t *testing.T) {
		store, service, item, hostID, threadID, requestID := newFixture(t)
		writes := 0
		write := store.writeOrchestration
		store.writeOrchestration = func(path string, value any) error {
			writes++
			return write(path, value)
		}
		prepared, created, err := service.PrepareHostUserInput(hostID, requestID, "first accepted body", "brain-thread:"+threadID)
		if err != nil || !created {
			t.Fatalf("prepare created=%v err=%v", created, err)
		}
		store.projectBrainInputAdmission = func(BrainInputAdmission) error {
			return errors.New("injected timeline projection failure")
		}
		if err := service.AdmitHostUserInput(prepared); err == nil {
			t.Fatal("timeline projection failure was ignored")
		}
		if writes != 2 {
			t.Fatalf("intent plus accepted/Attention writes=%d want 2", writes)
		}

		reopened, err := NewStore(store.Root)
		if err != nil {
			t.Fatal(err)
		}
		admission, found, err := reopened.BrainInputAdmission(requestID, threadID)
		if err != nil || !found || admission.State != BrainInputAdmissionAccepted {
			t.Fatalf("accepted authority found=%v admission=%+v err=%v", found, admission, err)
		}
		current, err := reopened.Work(item.ID)
		if err != nil || current.Wake != nil {
			t.Fatalf("accepted authority retained wait: Work=%+v err=%v", current, err)
		}
		events, err := reopened.ListWorkEvents(item.ID)
		if err != nil || countUnhandledEventKind(events, "user.input") != 1 {
			t.Fatalf("accepted Attention events=%+v err=%v", events, err)
		}
		items, err := reopened.ThreadTimeline(threadID, 0)
		if err != nil || len(items) != 0 {
			t.Fatalf("failed projection unexpectedly materialized: items=%+v err=%v", items, err)
		}

		restarted := NewService(reopened, nil, nil)
		complete, err := restarted.ReconcileSignalSystemStartup(nil, 8)
		if err != nil || !complete {
			t.Fatalf("startup complete=%v err=%v", complete, err)
		}
		items, err = reopened.ThreadTimeline(threadID, 0)
		if err != nil || len(items) != 1 || items[0].ID != requestID || !items[0].BrainAdmission || items[0].Body != "first accepted body" {
			t.Fatalf("recovered timeline items=%+v err=%v", items, err)
		}
		accepted, found, err := reopened.BrainInputAdmission(requestID, threadID)
		if err != nil || !found {
			t.Fatalf("accepted admission found=%v err=%v", found, err)
		}
		if err := restarted.AdmitHostUserInput(accepted); err != nil {
			t.Fatalf("duplicate accepted admission: %v", err)
		}
		events, _ = reopened.ListWorkEvents(item.ID)
		items, _ = reopened.ThreadTimeline(threadID, 0)
		if countUnhandledEventKind(events, "user.input") != 1 || len(items) != 1 {
			t.Fatalf("duplicate admission was not a no-op: events=%+v items=%+v", events, items)
		}
	})
}

func TestSignalAdversarialFirstSeenTerminalCalendarFaultReopenMatrix(t *testing.T) {
	for _, terminalStatus := range []calendar.Status{calendar.StatusCompleted, calendar.StatusFailed} {
		t.Run(string(terminalStatus), func(t *testing.T) {
			newFixture := func(t *testing.T) (*Store, *Service, Work, calendar.Event, string) {
				t.Helper()
				store, err := NewStore(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				threadID, err := store.ChatThreadID()
				if err != nil {
					t.Fatal(err)
				}
				itemID := "first-terminal-item-" + string(terminalStatus)
				runID := "first-terminal-run-" + string(terminalStatus)
				contextRef := "calendar:" + itemID + ":" + runID
				consumer, err := store.CreateWork(Work{
					Title: "First terminal Calendar consumer", Objective: "Wake on the exact first-seen occurrence.",
					Status: WorkWaiting, CompletionPolicy: CompletionBounded,
					Wake: &WorkWake{Kind: WorkWakeCalendarResult, Ref: contextRef},
				})
				if err != nil {
					t.Fatal(err)
				}
				finished := time.Date(2026, 8, 10, 4, 0, 0, 0, time.UTC)
				run := calendar.Run{
					ID: runID, Title: "First terminal Calendar occurrence", SourceThreadID: threadID,
					ScheduledFor: finished.Add(-time.Minute), StartedAt: finished.Add(-30 * time.Second),
					FinishedAt: &finished, Status: terminalStatus,
				}
				body := "Canonical Calendar result"
				if terminalStatus == calendar.StatusCompleted {
					run.Result = body
				} else {
					run.FailureReason = "Canonical Calendar failure"
					body = run.FailureReason
				}
				event := calendar.Event{
					Item: calendar.Item{
						ID: itemID, Title: run.Title, Kind: calendar.KindScheduledAction,
						ActionInstruction: "Produce one exact terminal occurrence.", SourceThreadID: threadID,
						Runs: []calendar.Run{run},
					},
					ScheduledResult: &calendar.ScheduledResult{
						ID: "first-terminal-result-" + string(terminalStatus), ThreadID: threadID,
						Body: body, CreatedAt: finished, Status: terminalStatus, Title: run.Title,
						CalendarItemID: itemID, CalendarRunID: runID, ScheduledFor: run.ScheduledFor,
					},
				}
				return store, NewService(store, nil, nil), consumer, event, calendarWorkID(itemID, runID)
			}

			t.Run("write failure exposes before-state", func(t *testing.T) {
				store, service, consumer, event, producerWorkID := newFixture(t)
				store.writeOrchestration = func(string, any) error { return errors.New("injected first terminal Calendar write failure") }
				if _, err := service.RouteCalendarEvent(event); err == nil {
					t.Fatal("first terminal Calendar persistence failure was ignored")
				}
				reopened, err := NewStore(store.Root)
				if err != nil {
					t.Fatal(err)
				}
				complete, err := NewService(reopened, nil, nil).ReconcileSignalSystemStartup(nil, 8)
				if err != nil || !complete {
					t.Fatalf("startup complete=%v err=%v", complete, err)
				}
				if producer, err := reopened.Work(producerWorkID); !errors.Is(err, ErrWorkNotFound) {
					t.Fatalf("write failure persisted intermediate producer: Work=%+v err=%v", producer, err)
				}
				current, err := reopened.Work(consumer.ID)
				if err != nil || current.Status != WorkWaiting || current.Wake == nil {
					t.Fatalf("write failure changed consumer: Work=%+v err=%v", current, err)
				}
				events, err := reopened.ListWorkEvents("")
				if err != nil || len(events) != 0 {
					t.Fatalf("write failure partial events=%+v err=%v", events, err)
				}
			})

			t.Run("one write exposes after-state and duplicate is no-op", func(t *testing.T) {
				store, service, consumer, event, producerWorkID := newFixture(t)
				writes := 0
				write := store.writeOrchestration
				store.writeOrchestration = func(path string, value any) error {
					writes++
					if writes > 1 {
						return errors.New("unexpected second first-terminal Calendar write")
					}
					return write(path, value)
				}
				if woke, err := service.RouteCalendarEvent(event); err != nil || !woke {
					t.Fatalf("first terminal Calendar wake=%v err=%v", woke, err)
				}
				if writes != 1 {
					t.Fatalf("first terminal Calendar writes=%d want 1", writes)
				}
				reopened, err := NewStore(store.Root)
				if err != nil {
					t.Fatal(err)
				}
				complete, err := NewService(reopened, nil, nil).ReconcileSignalSystemStartup(nil, 8)
				if err != nil || !complete {
					t.Fatalf("startup complete=%v err=%v", complete, err)
				}
				producer, err := reopened.Work(producerWorkID)
				if err != nil {
					t.Fatal(err)
				}
				wantProducerStatus := WorkDone
				if terminalStatus == calendar.StatusFailed {
					wantProducerStatus = WorkNeedsInput
				}
				if producer.Status != wantProducerStatus || producer.Wake != nil {
					t.Fatalf("terminal producer=%+v want status=%s without wake", producer, wantProducerStatus)
				}
				current, err := reopened.Work(consumer.ID)
				if err != nil || current.Wake != nil {
					t.Fatalf("terminal occurrence did not wake consumer: Work=%+v err=%v", current, err)
				}
				kind := "calendar.result"
				if terminalStatus == calendar.StatusFailed {
					kind = "calendar.failure"
				}
				events, err := reopened.ListWorkEvents("")
				if err != nil || len(events) != 2 || countUnhandledEventKind(events, kind) != 2 {
					t.Fatalf("terminal producer plus consumer events=%+v err=%v", events, err)
				}

				duplicateWrites := 0
				reopened.writeOrchestration = func(string, any) error {
					duplicateWrites++
					return errors.New("duplicate attempted persistence")
				}
				if woke, err := NewService(reopened, nil, nil).RouteCalendarEvent(event); err != nil || woke {
					t.Fatalf("duplicate terminal Calendar wake=%v err=%v", woke, err)
				}
				if duplicateWrites != 0 {
					t.Fatalf("duplicate terminal Calendar writes=%d want 0", duplicateWrites)
				}
			})
		})
	}
}

func TestSignalAdversarialSuccessorReservationSurvivesRequeueRestartAndOwnsFinalization(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	hostID := "brain-agent-brain-hidden:@1"
	incumbent := "brain-agent-incumbent:@1"
	s1 := "brain-agent-successor:@2"
	s2 := "brain-agent-successor:@3"
	item := createSignalTestWork(t, store, "Exclusive successor", incumbent)
	appendSignalTestEvent(t, store, item, "reserve-s1")
	delivered, _ := deliverSignalTestEvent(t, store, hostID)
	if _, err := store.ReserveWorkSuccessor(item.ID, s1); err != nil {
		t.Fatal(err)
	}
	s1Turn := s1 + ":turn:1"
	s1AcceptedAt := time.Now().UTC()
	s1Admission := watcher.TurnAdmission{
		Stream: "provider", ID: "admission-" + s1Turn, Cursor: 1,
		SHA256: pendingSubmissionDigest("continue authority " + s1Turn), At: s1AcceptedAt.Add(time.Second),
	}
	seedContinueAuthorityTurn(t, store, item.ID, s1, s1Turn, watcher.TurnAccepted, s1Admission, s1AcceptedAt)
	bindContinueAuthorityReservation(t, store, item.ID, s1, s1Turn)
	if _, created, err := store.RequeueUnhandledHostAttention(delivered.ID, delivered.HandlingID, delivered.ProviderTurnID); err != nil || !created {
		t.Fatalf("requeue created=%v err=%v", created, err)
	}

	restarted, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	current, err := restarted.Work(item.ID)
	if err != nil || current.SuccessorReservation == nil || current.SuccessorReservation.SessionID != s1 || current.SuccessorReservation.ProviderTurnID != s1Turn {
		t.Fatalf("S1 reservation did not survive restart: Work=%+v err=%v", current, err)
	}
	if _, err := restarted.ReserveWorkSuccessor(item.ID, s2); !errors.Is(err, ErrWorkOwnerConflict) {
		t.Fatalf("S2 replaced admitted S1: err=%v", err)
	}
	if projected := activeWorkByID(t, restarted, item.ID); projected.ProgressMode != WorkProgressReady ||
		projected.SuccessorReservation == nil || projected.SuccessorReservation.SessionID != s1 {
		t.Fatalf("requeued disposition did not remain the singular ready mode with exclusive S1: %+v", projected)
	}
	reconcile, _ := deliverSignalTestEvent(t, restarted, hostID)
	_, continued := resolveAdversarialEvent(t, restarted, reconcile, WorkDispositionContinue, nil, s1)
	if continued.OwnerSessionID != s1 || continued.SuccessorReservation != nil {
		t.Fatalf("requeued S1 did not promote through exact continue: %+v", continued)
	}
	if projected := activeWorkByID(t, restarted, item.ID); projected.ProgressMode != WorkProgressOwned {
		t.Fatalf("continued S1 progress mode = %+v", projected)
	}
	if _, changed, err := restarted.ApplyTurnFact(watcher.TurnFact{
		SessionID: s1, TurnID: s1Turn,
		Class: watcher.EvidenceProvider, Kind: "done", SourceID: "provider-s1-done",
		Admission: s1Admission, ActivityID: "activity-s1", StartedAt: s1AcceptedAt.Add(time.Second),
		SettledAt: s1AcceptedAt.Add(2 * time.Second), At: s1AcceptedAt.Add(2 * time.Second),
	}); err != nil || !changed {
		t.Fatalf("terminalize promoted S1 changed=%v err=%v", changed, err)
	}
	cancelHandling, _ := deliverSignalTestEvent(t, restarted, hostID)
	_, terminal := resolveAdversarialEvent(t, restarted, cancelHandling, WorkDispositionCancel, nil, "")
	foundS1 := false
	for _, finalization := range terminal.SessionFinalizations {
		if finalization.SessionID == s1 {
			foundS1 = true
		}
	}
	if !foundS1 {
		t.Fatalf("accepted S1 has no terminal finalization owner: %+v", terminal)
	}
}

func TestSignalAdversarialSuccessorReleaseRequiresProvedNonAdmission(t *testing.T) {
	for _, test := range []struct {
		name    string
		proved  bool
		cleared bool
	}{
		{name: "ambiguous input retains reservation", proved: false, cleared: false},
		{name: "proved non-admission releases reservation", proved: true, cleared: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			item := createSignalTestWork(t, store, "Successor failure", "brain-agent-incumbent:@1")
			appendSignalTestEvent(t, store, item, "successor-failure")
			handling, _ := deliverSignalTestEvent(t, store, "brain-agent-brain-hidden:@1")
			s1 := "brain-agent-successor:@1"
			if _, err := store.ReserveWorkSuccessor(item.ID, s1); err != nil {
				t.Fatal(err)
			}
			current, err := store.RecordSuccessorLaunchFailure(item.ID, s1, "provider submission failed", test.proved)
			if err != nil {
				t.Fatal(err)
			}
			if (current.SuccessorReservation == nil) != test.cleared {
				t.Fatalf("reservation after failure=%+v cleared=%v", current.SuccessorReservation, test.cleared)
			}
			_, secondErr := store.ReserveWorkSuccessor(item.ID, "brain-agent-successor:@2")
			if test.cleared && secondErr != nil {
				t.Fatalf("proved release did not allow a new reservation: %v", secondErr)
			}
			if !test.cleared && !errors.Is(secondErr, ErrWorkOwnerConflict) {
				t.Fatalf("ambiguous reservation allowed replacement: %v", secondErr)
			}
			if _, _, err := store.RequeueUnhandledHostAttention(handling.ID, handling.HandlingID, handling.ProviderTurnID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSignalAdversarialHeldClaimResolutionAndReconcileUseOneWrite(t *testing.T) {
	for _, test := range []struct {
		name   string
		apply  func(*Store, string) error
		result string
	}{
		{name: "mark delivered", result: EventResolutionMarkDelivered, apply: func(store *Store, eventID string) error {
			return store.MarkDeliveredClaim(eventID, "user", "visible in Host transcript")
		}},
		{name: "discard", result: EventResolutionDiscard, apply: func(store *Store, eventID string) error {
			return store.DiscardClaim(eventID, "user", "obsolete delivery")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, _, event := claimResolutionStore(t)
			writes := 0
			write := store.writeOrchestration
			store.writeOrchestration = func(path string, value any) error {
				writes++
				if writes > 1 {
					return fmt.Errorf("unexpected split persistence")
				}
				return write(path, value)
			}
			if err := test.apply(store, event.ID); err != nil {
				t.Fatalf("claim resolution was not one write: %v", err)
			}
			if writes != 1 {
				t.Fatalf("persistence writes=%d want 1", writes)
			}
			reopened, err := NewStore(store.Root)
			if err != nil {
				t.Fatal(err)
			}
			events, _ := reopened.ListWorkEvents(event.WorkID)
			if len(events) != 2 || events[0].Resolution != test.result || !containsUnhandledEventKind(events, "brain.reconcile_required") {
				t.Fatalf("reopened after-state is incomplete: %+v", events)
			}
		})

		t.Run(test.name+" write failure", func(t *testing.T) {
			store, _, event := claimResolutionStore(t)
			store.writeOrchestration = func(string, any) error { return fmt.Errorf("injected write failure") }
			if err := test.apply(store, event.ID); err == nil {
				t.Fatal("injected persistence failure was ignored")
			}
			reopened, err := NewStore(store.Root)
			if err != nil {
				t.Fatal(err)
			}
			events, _ := reopened.ListWorkEvents(event.WorkID)
			if len(events) != 1 || events[0].Resolution != "" || events[0].DiscardedAt != nil || events[0].DeliveredAt != nil {
				t.Fatalf("write failure exposed a partial after-state: %+v", events)
			}
		})
	}
}

func containsUnhandledEventKind(events []WorkEvent, kind string) bool {
	return countUnhandledEventKind(events, kind) != 0
}

func countUnhandledEventKind(events []WorkEvent, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind && event.HandledAt == nil && event.DiscardedAt == nil {
			count++
		}
	}
	return count
}
