package brain

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
)

func TestStartupRecoveryBatchesPresentationAndPreservesHistory(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	store.now = func() time.Time { return now }
	for i := 0; i < 4; i++ {
		item, err := store.CreateWork(Work{Title: "Recovery fixture", Objective: "Preserve accepted input", SourceThreadID: "thread", CompletionPolicy: CompletionBounded})
		if err != nil {
			t.Fatal(err)
		}
		session, turn := fmt.Sprintf("fixture:@%d", i), fmt.Sprintf("turn:%d", i)
		candidate := delegatedSubmissionCandidate(item.ID, session, turn, "fixture", now)
		pending, _, err := store.PrepareInputAdmission(candidate)
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.ResolveInputAdmission(watcher.InputAdmissionResolution{SessionID: session, ProposedTurnID: turn, Receipt: pending.Receipt, PayloadSHA256: pending.PayloadSHA256, ActivityID: "activity", ResolvedAt: now, Admission: watcher.TurnAdmission{Stream: "provider", ID: turn, Cursor: 1, SHA256: pending.PayloadSHA256, At: now}})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = store.ApplyTurnFact(watcher.TurnFact{SessionID: session, TurnID: turn, Class: watcher.EvidenceControl, Kind: "done", SourceID: turn + ":done", Summary: "Finished", At: now.Add(time.Second)})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = store.AppendTimelineItem(TimelineItem{ID: "history", ThreadID: "thread", SessionID: "history-session", Role: "assistant", Body: "Unrelated history survives", Kind: timelineKindAssistantMessage, CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	canonical := store.fsm.ListViews()
	before, err := os.ReadFile(store.presentationPath())
	if err != nil {
		t.Fatal(err)
	}
	timelineBefore, err := os.ReadFile(store.messagesPath())
	if err != nil {
		t.Fatal(err)
	}
	// An interrupted presentation commit must not publish the new cards.
	failure := errors.New("fixture disk failure")
	store.writePresentation = func(string, any) error { return failure }
	if err := store.rebuildFSMProjections(); !errors.Is(err, failure) {
		t.Fatalf("lost write error: %v", err)
	}
	after, _ := os.ReadFile(store.presentationPath())
	timelineAfter, _ := os.ReadFile(store.messagesPath())
	if !bytes.Equal(before, after) || !bytes.Equal(timelineBefore, timelineAfter) {
		t.Fatal("failed recovery published partial projection")
	}
	writes := 0
	store.writePresentation = func(path string, value any) error { writes++; return writeJSONFile(path, value) }
	if err := store.rebuildFSMProjections(); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("recovery persisted presentation %d times, want one", writes)
	}
	if !reflect.DeepEqual(canonical, store.fsm.ListViews()) {
		t.Fatal("repair changed canonical ownership")
	}
	items, err := store.ThreadTimeline("thread", 100)
	if err != nil {
		t.Fatal(err)
	}
	history, cards := 0, 0
	for _, item := range items {
		if item.ID == "history" {
			history++
		}
		if item.Kind == timelineKindWorkCard {
			cards++
		}
	}
	if history != 1 || cards != 4 {
		t.Fatalf("history=%d cards=%d", history, cards)
	}
	first, _ := os.ReadFile(store.presentationPath())
	firstTimeline, _ := os.ReadFile(store.messagesPath())
	if err := store.rebuildFSMProjections(); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(store.presentationPath())
	secondTimeline, _ := os.ReadFile(store.messagesPath())
	if !bytes.Equal(first, second) || !bytes.Equal(firstTimeline, secondTimeline) {
		t.Fatal("repeated recovery changed the repaired image")
	}
}
