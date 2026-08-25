package brain

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestPresentationReadCacheIsolatedAndInvalidated(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetChatState(ChatState{ThreadID: "thread-cache"}); err != nil {
		t.Fatal(err)
	}
	item, err := store.CreateWork(Work{
		Title: "cache-one", Objective: "verify presentation cache",
		Status: WorkOpen, CompletionPolicy: CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}

	store.mu.Lock()
	first, err := store.loadPresentationLocked()
	if err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	if !store.presentationCache.valid {
		store.mu.Unlock()
		t.Fatal("presentation cache was not populated")
	}
	index := workIndex(first.BrainWork, item.ID)
	first.BrainWork[index].Title = "caller-mutation"
	second, err := store.loadPresentationLocked()
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if second.BrainWork[workIndex(second.BrainWork, item.ID)].Title != "cache-one" {
		t.Fatal("caller mutation aliased the cached presentation database")
	}

	path := store.presentationPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	replaced := strings.Replace(string(raw), "cache-one", "cache-two", 1)
	if replaced == string(raw) {
		t.Fatal("fixture title not found in presentation database")
	}
	if err := os.WriteFile(path, []byte(replaced), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	external, err := store.loadPresentationLocked()
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if external.BrainWork[workIndex(external.BrainWork, item.ID)].Title != "cache-two" {
		t.Fatal("external presentation replacement did not invalidate the cache")
	}

	store.mu.Lock()
	external.BrainWork[workIndex(external.BrainWork, item.ID)].Title = "persisted"
	if err := store.persistPresentationLocked(external); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	if store.presentationCache.valid || store.turnCache.valid {
		store.mu.Unlock()
		t.Fatal("presentation mutation did not invalidate dependent caches")
	}
	persisted, err := store.loadPresentationLocked()
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if persisted.BrainWork[workIndex(persisted.BrainWork, item.ID)].Title != "persisted" {
		t.Fatal("persisted presentation mutation was not reloaded")
	}
}

func TestTimelineReadCacheIsolatedAndInvalidatedAfterAppend(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scheduled := time.Date(2026, 8, 26, 1, 0, 0, 0, time.UTC)
	_, err = store.AppendTimelineItem(TimelineItem{
		ID: "timeline-one", ThreadID: "thread-timeline-cache", SessionID: "host",
		Role: "assistant", Kind: timelineKindAssistantMessage, Body: "one", ScheduledFor: &scheduled,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ThreadTimeline("thread-timeline-cache", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || !store.timelineCache.valid {
		t.Fatalf("first timeline read = %#v cache=%+v", first, store.timelineCache)
	}
	*first[0].ScheduledFor = scheduled.Add(24 * time.Hour)
	second, err := store.ThreadTimeline("thread-timeline-cache", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !second[0].ScheduledFor.Equal(scheduled) {
		t.Fatal("caller mutation aliased the cached timeline")
	}
	_, err = store.AppendTimelineItem(TimelineItem{
		ID: "timeline-two", ThreadID: "thread-timeline-cache", SessionID: "host",
		Role: "assistant", Kind: timelineKindAssistantMessage, Body: "two",
	})
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.ThreadTimeline("thread-timeline-cache", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(third) != 2 || third[1].ID != "timeline-two" {
		t.Fatalf("append did not invalidate timeline cache: %#v", third)
	}
}

func TestTurnReadCacheInvalidatedAfterPresentationMutation(t *testing.T) {
	store, sessionID, turnID := ledgerTestStore(t)
	first, found, err := store.TurnByID(sessionID, turnID)
	if err != nil || !found {
		t.Fatalf("first TurnByID found=%v err=%v", found, err)
	}
	if !store.turnCache.valid {
		t.Fatal("turn cache was not populated")
	}

	store.mu.Lock()
	database, err := store.loadPresentationLocked()
	if err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	for index := range database.BrainTurns {
		if database.BrainTurns[index].SessionID == sessionID && database.BrainTurns[index].TurnID == turnID {
			database.BrainTurns[index].Summary = "changed after cache fill"
		}
	}
	if err := store.persistPresentationLocked(database); err != nil {
		store.mu.Unlock()
		t.Fatal(err)
	}
	store.mu.Unlock()
	if store.turnCache.valid {
		t.Fatal("presentation mutation left turn cache valid")
	}
	second, found, err := store.TurnByID(sessionID, turnID)
	if err != nil || !found {
		t.Fatalf("second TurnByID found=%v err=%v", found, err)
	}
	if first.Summary == second.Summary || second.Summary != "changed after cache fill" {
		t.Fatalf("turn cache did not reload mutation: first=%q second=%q", first.Summary, second.Summary)
	}
}
