package brain

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/lifecycle"
	"github.com/daoleno/zen/daemon/watcher"
)

func TestAdmissionProjectionRebuildOrdersReceiptsForOneProviderTurn(t *testing.T) {
	for _, step := range []time.Duration{time.Second, 0, -time.Second} {
		for _, removePresentation := range []bool{false, true} {
			t.Run(fmt.Sprintf("step=%s/remove=%t", step, removePresentation), func(t *testing.T) {
				testAdmissionProjectionOrder(t, step, removePresentation)
			})
		}
	}
}

func testAdmissionProjectionOrder(t *testing.T, step time.Duration, removePresentation bool) {
	t.Helper()
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	item, err := store.CreateWork(Work{Title: "ordered receipts", Objective: "rebuild exactly", CompletionPolicy: CompletionBounded})
	if err != nil {
		t.Fatal(err)
	}
	const session, original = "worker:@1", "turn:z-original"
	for index, token := range []string{original, "turn:y-steer", "turn:a-steer"} {
		now = now.Add(step)
		candidate := delegatedSubmissionCandidate(item.ID, session, token, token, now)
		if index > 0 {
			candidate.Mode = watcher.InputAdmissionConditionalSteer
			candidate.ExistingTurnID, candidate.BaselineActivityID = original, "activity"
		}
		pending, created, err := store.PrepareInputAdmission(candidate)
		if err != nil || !created {
			t.Fatalf("prepare=%+v created=%v err=%v", pending, created, err)
		}
		_, err = store.ResolveInputAdmission(watcher.InputAdmissionResolution{
			SessionID: session, ProposedTurnID: token, Receipt: token, PayloadSHA256: pending.PayloadSHA256,
			ActivityID: "activity", ResolvedAt: now.Add(time.Millisecond),
			Admission: watcher.TurnAdmission{Stream: "provider", ID: fmt.Sprintf("input-%d", index), Cursor: uint64(index + 1), SHA256: pending.PayloadSHA256, At: now.Add(time.Millisecond)},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	before, err := store.FSM().State(lifecycle.WorkID(item.ID))
	if err != nil {
		t.Fatal(err)
	}
	if step == 0 {
		for _, admission := range before.Admissions {
			if !admission.PreparedAt.Equal(now) {
				t.Fatalf("expected equal canonical preparation timestamps: %+v", admission)
			}
		}
	}
	var lastSequence uint64
	for _, token := range []lifecycle.TurnToken{original, "turn:y-steer", "turn:a-steer"} {
		sequence := before.Admissions[token].AcceptedSeq
		if sequence <= lastSequence {
			t.Fatalf("acceptance sequence is not increasing: %d after %d", sequence, lastSequence)
		}
		lastSequence = sequence
	}
	for attempt := 0; attempt < 12; attempt++ {
		if removePresentation {
			if err := os.Remove(store.presentationPath()); err != nil {
				t.Fatal(err)
			}
		}
		reopened, err := NewStore(root)
		if err != nil {
			t.Fatal(err)
		}
		turn, found, err := reopened.Turn(session)
		if err != nil || !found || turn.TurnID != original || turn.Admission.ID != "input-2" || turn.Admission.Cursor != 3 {
			t.Fatalf("rebuild %d selected old receipt: turn=%+v found=%v err=%v", attempt, turn, found, err)
		}
		after, err := reopened.FSM().State(lifecycle.WorkID(item.ID))
		if err != nil || after.Revision != before.Revision || after.Attempt.TurnToken != original || len(after.Admissions) != 3 {
			t.Fatalf("projection rebuild changed ownership: %+v err=%v", after, err)
		}
	}
}
