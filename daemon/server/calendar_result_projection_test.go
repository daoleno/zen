package server

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/calendar"
	"github.com/daoleno/zen/daemon/work"
)

func TestBrainSnapshotProjectsOnlyKnownThreadCalendarRunsWithoutWriting(t *testing.T) {
	brainStore, err := brain.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := brainStore.SetChatState(brain.ChatState{
		ThreadID: "thread-current", ThreadIDs: []string{"thread-history"},
	}); err != nil {
		t.Fatal(err)
	}
	service := brain.NewService(brainStore, nil, nil)
	calendarStore, err := calendar.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	current := finishScheduledResult(t, calendarStore, "item-current", "Current", "thread-current", "current result", "")
	history := finishScheduledResult(t, calendarStore, "item-history", "History", "thread-history", "", "historical failure")
	_ = finishScheduledResult(t, calendarStore, "item-unknown", "Unknown", "thread-unknown", "must stay hidden", "")

	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	calendarRaw, calendarInfo := fileIdentity(t, calendarStore.Path())
	registryRaw, registryInfo := fileIdentity(t, brainStore.ChatStatePath())
	srv := &Server{brain: service, calendar: calendarStore}

	for range 2 {
		wire, err := srv.brainSnapshotWire(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		payload, ok := wire.(map[string]any)
		if !ok {
			t.Fatalf("snapshot wire = %T", wire)
		}
		results, ok := payload["scheduled_results"].([]calendar.ScheduledResult)
		if !ok || len(results) != 2 {
			t.Fatalf("scheduled results = %#v", payload["scheduled_results"])
		}
		seen := map[string]bool{}
		for _, result := range results {
			seen[result.ID] = true
			if result.ThreadID == "thread-unknown" {
				t.Fatalf("unknown thread projected: %#v", result)
			}
		}
		if !seen[current.ID] || !seen[history.ID] {
			t.Fatalf("known results missing: %#v", results)
		}
	}
	assertFileIdentity(t, calendarStore.Path(), calendarRaw, calendarInfo)
	assertFileIdentity(t, brainStore.ChatStatePath(), registryRaw, registryInfo)
}

func TestBrainSnapshotWithNilCalendarHasEmptyResultProjection(t *testing.T) {
	service, _ := newBrainCalendarFixture(t, "thread-current")
	snapshot, err := service.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := (&Server{brain: service}).brainSnapshotWire(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	results, ok := wire.(map[string]any)["scheduled_results"].([]calendar.ScheduledResult)
	if !ok || len(results) != 0 {
		t.Fatalf("nil-Calendar results = %#v", wire)
	}
}

func TestHistoricalBrainThreadUsesOnlyItsCalendarProjection(t *testing.T) {
	service, calendarStore := newBrainCalendarFixture(t, "thread-current", "thread-history")
	result := finishScheduledResult(t, calendarStore, "item-history", "History", "thread-history", "historical result", "")
	srv := &Server{brain: service, calendar: calendarStore}
	conversation := work.CodexConversation{
		Available: true,
		Activity:  &work.ProviderActivity{ID: "current-activity", Status: work.ProviderActivityRunning},
		Events:    []work.CodexConversationEvent{{ID: "current-provider-event", Kind: "assistant_message"}},
	}

	got := srv.brainScopedConversation("brain-thread:thread-history", conversation, time.Now())
	if !got.Available || got.Reason != "" || got.Activity != nil || len(got.Events) != 1 || got.Events[0].ID != result.ID {
		t.Fatalf("historical projection = %#v", got)
	}
}

func TestUnknownBrainThreadIsRejectedBeforeCalendarProjection(t *testing.T) {
	service, calendarStore := newBrainCalendarFixture(t, "thread-current")
	_ = finishScheduledResult(t, calendarStore, "item-unknown", "Unknown", "thread-unknown", "hidden", "")
	srv := &Server{brain: service, calendar: calendarStore}
	conversation := work.CodexConversation{
		Available: true,
		Activity:  &work.ProviderActivity{ID: "provider-activity", Status: work.ProviderActivityRunning},
		Events:    []work.CodexConversationEvent{{ID: "provider-event", Kind: "assistant_message"}},
	}

	got := srv.brainScopedConversation("brain-thread:thread-unknown", conversation, time.Now())
	if got.Available || got.Reason != "brain_thread_unknown" || got.Activity != nil || len(got.Events) != 0 {
		t.Fatalf("unknown thread projection = %#v", got)
	}
}

func fileIdentity(t *testing.T, path string) ([]byte, os.FileInfo) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw, info
}

func assertFileIdentity(t *testing.T, path string, want []byte, before os.FileInfo) {
	t.Helper()
	got, after := fileIdentity(t, path)
	if !bytes.Equal(got, want) || after.Mode() != before.Mode() ||
		!after.ModTime().Equal(before.ModTime()) || !os.SameFile(before, after) {
		t.Fatalf("file changed during projection: %s", path)
	}
}
