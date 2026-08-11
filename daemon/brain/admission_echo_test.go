package brain

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

func acceptAndProjectEchoTestAdmission(
	t *testing.T,
	store *Store,
	requestID string,
	threadID string,
	sessionID string,
	body string,
	createdAt time.Time,
	acceptedAt time.Time,
) BrainInputAdmission {
	t.Helper()
	store.now = func() time.Time { return createdAt }
	candidate := BrainInputAdmission{
		RequestID: requestID, ThreadID: threadID, HostSessionID: "brain-host:@echo-test",
		SessionID: sessionID, DisplayBody: body,
	}
	if _, created, err := store.PrepareBrainInputAdmission(candidate); err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	store.now = func() time.Time { return acceptedAt }
	accepted, _, changed, err := store.AcceptBrainInputAdmission(candidate)
	if err != nil || !changed {
		t.Fatalf("accept changed=%v err=%v", changed, err)
	}
	if err := store.ProjectBrainInputAdmission(accepted); err != nil {
		t.Fatal(err)
	}
	return accepted
}

func TestProviderEchoMaterializedDuringAdmissionWindowReconcilesAtProjection(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-raced-provider-echo"
	sessionID := "019fec64-raced-provider-session"
	body := "continue through the exact admission window"
	providerID := sessionID + ":19020607"
	createdAt := time.Date(2026, 8, 11, 13, 27, 27, 110000000, time.UTC)
	providerAt := createdAt.Add(531 * time.Millisecond)
	acceptedAt := createdAt.Add(1285 * time.Millisecond)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}

	store.now = func() time.Time { return createdAt }
	candidate := BrainInputAdmission{
		RequestID: "receipt-raced-provider-echo", ThreadID: threadID,
		HostSessionID: "brain-host:@admission-window", SessionID: sessionID, DisplayBody: body,
	}
	prepared, created, err := store.PrepareBrainInputAdmission(candidate)
	if err != nil || !created {
		t.Fatalf("prepare admission created=%v row=%+v err=%v", created, prepared, err)
	}
	conversation := work.CodexConversation{
		Available: true,
		SessionID: sessionID,
		Events: []work.CodexConversationEvent{{
			ID: providerID, Timestamp: providerAt.Format(time.RFC3339Nano), Kind: timelineKindUserMessage,
			Role: "user", Body: body, AdmissionSHA256: AdmissionDigest(body),
		}},
	}
	if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
		t.Fatal(err)
	}
	premature, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(premature) != 1 || premature[0].ID != providerID || premature[0].BrainAdmission {
		t.Fatalf("premature provider row=%+v err=%v", premature, err)
	}

	store.now = func() time.Time { return acceptedAt }
	accepted, _, changed, err := store.AcceptBrainInputAdmission(candidate)
	if err != nil || !changed {
		t.Fatalf("accept admission changed=%v row=%+v err=%v", changed, accepted, err)
	}
	// Simulate an upgrade from the former projection path, which appended the
	// canonical receipt after the premature provider row but could not remove
	// the already-durable echo. A startup/idempotent retry must repair it too.
	if _, err := store.AppendTimelineItem(brainInputAdmissionTimelineItem(accepted)); err != nil {
		t.Fatal(err)
	}
	beforeRetry, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(beforeRetry) != 2 {
		t.Fatalf("pre-upgrade duplicate timeline=%+v err=%v", beforeRetry, err)
	}
	if err := store.ProjectBrainInputAdmission(accepted); err != nil {
		t.Fatal(err)
	}
	assertRacedAdmissionTimeline := func(t *testing.T, current *Store) {
		t.Helper()
		items, err := current.ThreadTimeline(threadID, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0].ID != candidate.RequestID || !items[0].BrainAdmission ||
			items[0].AdmissionSHA256 != AdmissionDigest(body) || items[0].AdmissionEchoEventID != providerID {
			t.Fatalf("reconciled timeline=%+v", items)
		}
	}
	assertRacedAdmissionTimeline(t, store)

	reopened, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := reopened.ProjectBrainInputAdmission(accepted); err != nil {
		t.Fatalf("idempotent projection after reopen: %v", err)
	}
	if err := reopened.MaterializeProviderConversation(threadID, conversation); err != nil {
		t.Fatalf("provider replay after reopen: %v", err)
	}
	assertRacedAdmissionTimeline(t, reopened)
}

// The provider stamps its user_message row when the turn loop processes the
// input, always after the daemon's transport acceptance. An echo recorded
// after AcceptedAt is therefore the admission's own echo, not an independent
// input: materialized before acceptance it becomes a durable provider row that
// projection must reconcile into the canonical admission row.
func TestEchoRecordedAfterAcceptanceReconcilesAtProjection(t *testing.T) {
	base := time.Date(2026, 8, 11, 14, 1, 0, 0, time.UTC)
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-window-safety"
	body := "same body but independent input"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return base }
	candidate := BrainInputAdmission{
		RequestID: "receipt-window-safety", ThreadID: threadID,
		HostSessionID: "brain-host:@window-safety", SessionID: "provider-session", DisplayBody: body,
	}
	if _, created, err := store.PrepareBrainInputAdmission(candidate); err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	// The provider row is recorded 1ns after the daemon would accept (accepted
	// at base+1s): a bounded [CreatedAt, AcceptedAt] window could never match
	// it, which is exactly the live duplicate-bubble defect.
	providerID := "provider-session:user"
	conversation := work.CodexConversation{
		Available: true, SessionID: "provider-session",
		Events: []work.CodexConversationEvent{{
			ID: providerID, Timestamp: base.Add(time.Second + time.Nanosecond).Format(time.RFC3339Nano),
			Kind: timelineKindUserMessage, Role: "user", Body: body,
		}},
	}
	if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return base.Add(time.Second) }
	accepted, _, changed, err := store.AcceptBrainInputAdmission(candidate)
	if err != nil || !changed {
		t.Fatalf("accept changed=%v err=%v", changed, err)
	}
	if err := store.ProjectBrainInputAdmission(accepted); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("timeline=%+v err=%v", items, err)
	}
	if items[0].ID != candidate.RequestID || !items[0].BrainAdmission ||
		items[0].AdmissionEchoEventID != providerID {
		t.Fatalf("echo row was not reconciled into the admission: %+v", items)
	}
}

// TestEchoAfterAcceptanceIsClaimedAndStartupRepairsExistingDuplicate mirrors
// the live duplicate-bubble defect end to end: the provider records its
// user_message echo only when the turn loop processes the input — seconds
// after the daemon's transport acceptance — so a bounded echo window could
// never claim it and the echo was materialized as a second durable user row.
func TestEchoAfterAcceptanceIsClaimedAndStartupRepairsExistingDuplicate(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-live-duplicate"
	sessionID := "019fef19-live-provider-session"
	body := "deployment message shown twice"
	createdAt := time.Date(2026, 8, 11, 6, 0, 21, 45000000, time.UTC)
	acceptedAt := createdAt.Add(261 * time.Millisecond)
	accepted := acceptAndProjectEchoTestAdmission(
		t, store, "mso94a8r_bs6v62", threadID, sessionID, body, createdAt, acceptedAt,
	)
	// The provider stamps the echo when the turn starts processing, 2.6s
	// after the daemon accepted the transport write (live defect timings).
	echoAt := acceptedAt.Add(2589 * time.Millisecond)
	echoID := sessionID + ":7005935"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true, SessionID: sessionID,
		Events: []work.CodexConversationEvent{{
			ID: echoID, Timestamp: echoAt.Format(time.RFC3339Nano),
			Kind: timelineKindUserMessage, Role: "user", Body: body,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("post-fix timeline=%+v err=%v", items, err)
	}
	if items[0].ID != accepted.RequestID || items[0].AdmissionEchoEventID != echoID {
		t.Fatalf("admission did not claim its after-acceptance echo: %+v", items)
	}

	// Pre-fix live state: the echo was already materialized as a second
	// durable user row (the bounded window never matched). The idempotent
	// startup projection pass must repair it without any migration.
	legacy, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	legacy.now = func() time.Time { return acceptedAt }
	legacyPrepared, created, err := legacy.PrepareBrainInputAdmission(BrainInputAdmission{
		RequestID: accepted.RequestID, ThreadID: threadID,
		HostSessionID: "brain-host:@live", SessionID: sessionID, DisplayBody: body,
	})
	if err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	legacyAccepted, _, changed, err := legacy.AcceptBrainInputAdmission(legacyPrepared)
	if err != nil || !changed {
		t.Fatalf("accept changed=%v err=%v", changed, err)
	}
	if _, err := legacy.AppendTimelineItem(brainInputAdmissionTimelineItem(legacyAccepted)); err != nil {
		t.Fatal(err)
	}
	echoRow, ok := timelineItemFromProviderEvent(threadID, sessionID, work.CodexConversationEvent{
		ID: echoID, Timestamp: echoAt.Format(time.RFC3339Nano),
		Kind: timelineKindUserMessage, Role: "user", Body: body,
	})
	if !ok {
		t.Fatal("provider echo row projection failed")
	}
	if _, err := legacy.AppendTimelineItem(echoRow); err != nil {
		t.Fatal(err)
	}
	duplicated, err := legacy.ThreadTimeline(threadID, 0)
	if err != nil || len(duplicated) != 2 {
		t.Fatalf("pre-fix duplicate timeline=%+v err=%v", duplicated, err)
	}

	unprojected, more, err := legacy.UnprojectedBrainInputAdmissions(10)
	if err != nil || more {
		t.Fatalf("unprojected batch err=%v more=%v", err, more)
	}
	if len(unprojected) != 1 || unprojected[0].RequestID != accepted.RequestID {
		t.Fatalf("startup pass did not select the unclaimed admission: %+v", unprojected)
	}
	if err := legacy.ProjectBrainInputAdmission(unprojected[0]); err != nil {
		t.Fatal(err)
	}
	repaired, err := legacy.ThreadTimeline(threadID, 0)
	if err != nil || len(repaired) != 1 {
		t.Fatalf("repaired timeline=%+v err=%v", repaired, err)
	}
	if repaired[0].ID != accepted.RequestID || !repaired[0].BrainAdmission ||
		repaired[0].AdmissionEchoEventID != echoID {
		t.Fatalf("repair did not converge on the canonical admission row: %+v", repaired)
	}
}

func TestProviderRowsBeforeAdmissionCreationOrOtherSessionRemainIndependent(t *testing.T) {
	base := time.Date(2026, 8, 11, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		providerSession string
		providerAt      time.Time
	}{
		{name: "before prepare", providerSession: "provider-session", providerAt: base.Add(-time.Nanosecond)},
		{name: "other provider session", providerSession: "other-session", providerAt: base.Add(500 * time.Millisecond)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			threadID := "thread-window-safety"
			body := "same body but independent input"
			if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
				t.Fatal(err)
			}
			store.now = func() time.Time { return base }
			candidate := BrainInputAdmission{
				RequestID: "receipt-window-safety", ThreadID: threadID,
				HostSessionID: "brain-host:@window-safety", SessionID: "provider-session", DisplayBody: body,
			}
			if _, created, err := store.PrepareBrainInputAdmission(candidate); err != nil || !created {
				t.Fatalf("prepare created=%v err=%v", created, err)
			}
			providerID := test.providerSession + ":user"
			conversation := work.CodexConversation{
				Available: true, SessionID: test.providerSession,
				Events: []work.CodexConversationEvent{{
					ID: providerID, Timestamp: test.providerAt.Format(time.RFC3339Nano),
					Kind: timelineKindUserMessage, Role: "user", Body: body,
				}},
			}
			if err := store.MaterializeProviderConversation(threadID, conversation); err != nil {
				t.Fatal(err)
			}
			store.now = func() time.Time { return base.Add(time.Second) }
			accepted, _, changed, err := store.AcceptBrainInputAdmission(candidate)
			if err != nil || !changed {
				t.Fatalf("accept changed=%v err=%v", changed, err)
			}
			if err := store.ProjectBrainInputAdmission(accepted); err != nil {
				t.Fatal(err)
			}
			items, err := store.ThreadTimeline(threadID, 0)
			if err != nil || len(items) != 2 {
				t.Fatalf("timeline=%+v err=%v", items, err)
			}
			byID := map[string]TimelineItem{}
			for _, item := range items {
				byID[item.ID] = item
			}
			if provider := byID[providerID]; provider.ID == "" || provider.BrainAdmission {
				t.Fatalf("independent provider row changed: %+v", items)
			}
			if admission := byID[candidate.RequestID]; admission.ID == "" || !admission.BrainAdmission ||
				admission.AdmissionEchoEventID != "" {
				t.Fatalf("admission consumed independent row: %+v", items)
			}
		})
	}
}

func TestAdmissionEchoMatchingRequiresExactSessionAndCreationBound(t *testing.T) {
	base := time.Date(2026, 8, 11, 14, 15, 0, 0, time.UTC)
	tests := []struct {
		name            string
		providerSession string
		providerAt      time.Time
		wantSuppressed  bool
	}{
		{name: "exact causal echo", providerSession: "provider-session", providerAt: base.Add(500 * time.Millisecond), wantSuppressed: true},
		// The provider records its user_message row only when the turn starts
		// processing, which is always after the daemon's transport acceptance;
		// this is the admission's own echo and must be suppressed.
		{name: "echo recorded after accept", providerSession: "provider-session", providerAt: base.Add(time.Second + time.Nanosecond), wantSuppressed: true},
		{name: "same text before prepare", providerSession: "provider-session", providerAt: base.Add(-time.Nanosecond)},
		{name: "same text replacement session", providerSession: "replacement-session", providerAt: base.Add(500 * time.Millisecond)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			threadID := "thread-admission-first-window"
			body := "same text with distinct causal authority"
			if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
				t.Fatal(err)
			}
			acceptAndProjectEchoTestAdmission(
				t, store, "receipt-admission-first", threadID, "provider-session", body,
				base, base.Add(time.Second),
			)
			providerID := test.providerSession + ":user"
			if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
				Available: true, SessionID: test.providerSession,
				Events: []work.CodexConversationEvent{{
					ID: providerID, Timestamp: test.providerAt.Format(time.RFC3339Nano),
					Kind: timelineKindUserMessage, Role: "user", Body: body,
				}},
			}); err != nil {
				t.Fatal(err)
			}
			items, err := store.ThreadTimeline(threadID, 0)
			if err != nil {
				t.Fatal(err)
			}
			byID := map[string]TimelineItem{}
			for _, item := range items {
				byID[item.ID] = item
			}
			admission := byID["receipt-admission-first"]
			if test.wantSuppressed {
				if len(items) != 1 || admission.AdmissionEchoEventID != providerID {
					t.Fatalf("exact echo was not claimed once: %+v", items)
				}
				return
			}
			provider := byID[providerID]
			if len(items) != 2 || provider.ID == "" || provider.BrainAdmission ||
				admission.AdmissionEchoEventID != "" {
				t.Fatalf("independent same-text input was deleted/suppressed: %+v", items)
			}
		})
	}
}

func TestProviderEchoAdmissionWindowAmbiguityFailsClosed(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-window-ambiguity"
	sessionID := "provider-session-ambiguity"
	body := "ambiguous duplicate body"
	createdAt := time.Date(2026, 8, 11, 14, 30, 0, 0, time.UTC)
	acceptedAt := createdAt.Add(time.Second)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return createdAt }
	candidate := BrainInputAdmission{
		RequestID: "receipt-window-ambiguity", ThreadID: threadID,
		HostSessionID: "brain-host:@window-ambiguity", SessionID: sessionID, DisplayBody: body,
	}
	if _, created, err := store.PrepareBrainInputAdmission(candidate); err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	events := make([]work.CodexConversationEvent, 0, 2)
	for index := 0; index < 2; index++ {
		events = append(events, work.CodexConversationEvent{
			ID:        fmt.Sprintf("%s:%d", sessionID, index+1),
			Timestamp: createdAt.Add(time.Duration(index+1) * 250 * time.Millisecond).Format(time.RFC3339Nano),
			Kind:      timelineKindUserMessage, Role: "user", Body: body,
		})
	}
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true, SessionID: sessionID, Events: events,
	}); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return acceptedAt }
	accepted, _, changed, err := store.AcceptBrainInputAdmission(candidate)
	if err != nil || !changed {
		t.Fatalf("accept changed=%v err=%v", changed, err)
	}
	if err := store.ProjectBrainInputAdmission(accepted); err == nil || !strings.Contains(err.Error(), "2 provider echoes") {
		t.Fatalf("ambiguous projection err=%v", err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 2 {
		t.Fatalf("fail-closed timeline=%+v err=%v", items, err)
	}
	for _, item := range items {
		if item.BrainAdmission || item.ID == candidate.RequestID {
			t.Fatalf("ambiguous projection mutated timeline: %+v", items)
		}
	}
}

func TestProjectionRejectsReconstructedAdmissionAndConsumesOwnEchoRow(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-reconstructed-window"
	sessionID := "provider-reconstructed-window"
	body := "same text echoed after transport acceptance"
	createdAt := time.Date(2026, 8, 11, 14, 45, 0, 0, time.UTC)
	acceptedAt := createdAt.Add(time.Second)
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return createdAt }
	candidate := BrainInputAdmission{
		RequestID: "receipt-reconstructed-window", ThreadID: threadID,
		HostSessionID: "brain-host:@reconstructed-window", SessionID: sessionID, DisplayBody: body,
	}
	if _, created, err := store.PrepareBrainInputAdmission(candidate); err != nil || !created {
		t.Fatalf("prepare created=%v err=%v", created, err)
	}
	store.now = func() time.Time { return acceptedAt }
	accepted, _, changed, err := store.AcceptBrainInputAdmission(candidate)
	if err != nil || !changed {
		t.Fatalf("accept changed=%v err=%v", changed, err)
	}
	providerID := sessionID + ":outside"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true, SessionID: sessionID,
		Events: []work.CodexConversationEvent{{
			ID: providerID, Timestamp: acceptedAt.Add(time.Second).Format(time.RFC3339Nano),
			Kind: timelineKindUserMessage, Role: "user", Body: body,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	reconstructed := accepted
	extendedAcceptedAt := acceptedAt.Add(2 * time.Second)
	reconstructed.AcceptedAt = &extendedAcceptedAt
	if err := store.ProjectBrainInputAdmission(reconstructed); err == nil ||
		!strings.Contains(err.Error(), "differs from durable authority") {
		t.Fatalf("reconstructed projection err=%v", err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 || items[0].ID != providerID || items[0].BrainAdmission {
		t.Fatalf("reconstructed window removed provider row: items=%+v err=%v", items, err)
	}
	if err := store.ProjectBrainInputAdmission(accepted); err != nil {
		t.Fatal(err)
	}
	items, err = store.ThreadTimeline(threadID, 0)
	if err != nil || len(items) != 1 {
		t.Fatalf("durable projection timeline=%+v err=%v", items, err)
	}
	if items[0].ID != accepted.RequestID || !items[0].BrainAdmission ||
		items[0].AdmissionEchoEventID != providerID {
		t.Fatalf("durable projection left the echo row independent: %+v", items)
	}
}

func TestTwoIdenticalAdmissionsKeepTwoRowsAndEchoesAddZero(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-twin-admit"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	body := "same deliberate body"
	base := time.Date(2026, 8, 6, 5, 0, 0, 0, time.UTC)
	first := acceptAndProjectEchoTestAdmission(
		t, store, "receipt-a", threadID, "provider-session", body,
		base.Add(-time.Minute), base.Add(30*time.Second),
	)
	second := acceptAndProjectEchoTestAdmission(
		t, store, "receipt-b", threadID, "provider-session", body,
		base.Add(-30*time.Second), base.Add(30*time.Second),
	)
	if first.RequestID == second.RequestID {
		t.Fatalf("admissions = %#v %#v", first, second)
	}

	echoA := "provider-session:11"
	echoB := "provider-session:12"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "provider-session",
		Events: []work.CodexConversationEvent{{
			ID:              echoA,
			Timestamp:       base.Format(time.RFC3339Nano),
			Kind:            "user_message",
			Role:            "user",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}, {
			ID:              echoB,
			Timestamp:       base.Add(time.Second).Format(time.RFC3339Nano),
			Kind:            "user_message",
			Role:            "user",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}, {
			ID:        "provider-session:13",
			Timestamp: base.Add(2 * time.Second).Format(time.RFC3339Nano),
			Kind:      "assistant_message",
			Role:      "assistant",
			Body:      "ack",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	userIDs := []string{}
	for _, item := range items {
		if item.Kind == "user_message" {
			userIDs = append(userIDs, item.ID)
			if !item.BrainAdmission {
				t.Fatalf("echo leaked as durable user row: %#v", item)
			}
		}
	}
	if len(userIDs) != 2 || userIDs[0] != "receipt-a" || userIDs[1] != "receipt-b" {
		t.Fatalf("want two admission rows, got %#v from %#v", userIDs, items)
	}
	for _, item := range items {
		if item.ID == "receipt-a" && item.AdmissionEchoEventID != echoA {
			t.Fatalf("receipt-a echo claim = %#v", item)
		}
		if item.ID == "receipt-b" && item.AdmissionEchoEventID != echoB {
			t.Fatalf("receipt-b echo claim = %#v", item)
		}
	}

	// Replay is idempotent: still two user rows.
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "provider-session",
		Events: []work.CodexConversationEvent{{
			ID:              echoA,
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}, {
			ID:              echoB,
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	again, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	userCount := 0
	for _, item := range again {
		if item.Kind == "user_message" {
			userCount++
		}
	}
	if userCount != 2 {
		t.Fatalf("replay user count = %d items=%#v", userCount, again)
	}
}

func TestSameBodyDirectProviderInputStillMaterializes(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-terminal-same-body"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	body := "continue"
	base := time.Date(2026, 8, 6, 5, 10, 0, 0, time.UTC)
	acceptAndProjectEchoTestAdmission(
		t, store, "receipt-once", threadID, "019fcf7a-session", body,
		base.Add(-time.Second), base.Add(30*time.Second),
	)
	echoID := "019fcf7a-session:10"
	terminalID := "019fcf7a-session:20"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "019fcf7a-session",
		Events: []work.CodexConversationEvent{{
			ID:              echoID,
			Timestamp:       base.Format(time.RFC3339Nano),
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}, {
			ID:              terminalID,
			Timestamp:       base.Add(time.Minute).Format(time.RFC3339Nano),
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var admission, terminal *TimelineItem
	for index := range items {
		item := items[index]
		switch item.ID {
		case "receipt-once":
			admission = &items[index]
		case terminalID:
			terminal = &items[index]
		case echoID:
			t.Fatalf("provider echo materialized: %#v", item)
		}
	}
	if admission == nil || !admission.BrainAdmission || admission.AdmissionEchoEventID != echoID {
		t.Fatalf("admission = %#v", admission)
	}
	if terminal == nil || terminal.BrainAdmission || terminal.AdmissionSHA256 != "" {
		t.Fatalf("terminal provider row = %#v", terminal)
	}
}

func TestProviderNativeRowsAreNotAdmissionCredits(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-provider-credit"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	body := "provider-only body"
	providerID := "rollout-file.jsonl:42"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "provider-session",
		Events: []work.CodexConversationEvent{{
			ID:              providerID,
			Timestamp:       "2026-08-06T05:20:00Z",
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != providerID {
		t.Fatalf("provider row = %#v", items)
	}
	if items[0].BrainAdmission || items[0].AdmissionSHA256 != "" {
		t.Fatalf("provider row became admission credit: %#v", items[0])
	}

	// A later identical provider input still materializes (no false credit).
	secondID := "rollout-file.jsonl:99"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "provider-session",
		Events: []work.CodexConversationEvent{{
			ID:              providerID,
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}, {
			ID:              secondID,
			Timestamp:       "2026-08-06T05:21:00Z",
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	again, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 2 {
		t.Fatalf("second provider input suppressed: %#v", again)
	}
}

func TestPriorProviderRowDoesNotConsumeLaterAdmissionCredit(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	threadID := "thread-prior-provider"
	if err := store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		t.Fatal(err)
	}
	body := "shared body X"
	oldProviderID := "provider-session:5"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "provider-session",
		Events: []work.CodexConversationEvent{{
			ID:              oldProviderID,
			Timestamp:       "2026-08-06T04:00:00Z",
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 8, 6, 5, 30, 0, 0, time.UTC)
	acceptAndProjectEchoTestAdmission(
		t, store, "receipt-later", threadID, "provider-session", body,
		base.Add(-time.Minute), base.Add(30*time.Second),
	)
	newEchoID := "provider-session:50"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "provider-session",
		Events: []work.CodexConversationEvent{{
			ID:              oldProviderID,
			Timestamp:       "2026-08-06T04:00:00Z",
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}, {
			ID:              newEchoID,
			Timestamp:       base.Format(time.RFC3339Nano),
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]TimelineItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	oldRow, ok := byID[oldProviderID]
	if !ok || oldRow.BrainAdmission {
		t.Fatalf("old provider row missing/mutated: %#v", items)
	}
	admission, ok := byID["receipt-later"]
	if !ok || !admission.BrainAdmission {
		t.Fatalf("admission missing: %#v", items)
	}
	if admission.AdmissionEchoEventID != newEchoID {
		t.Fatalf("admission claimed wrong echo: %#v", admission)
	}
	if _, leaked := byID[newEchoID]; leaked {
		t.Fatalf("new echo materialized despite admission: %#v", items)
	}
	if len(items) != 2 {
		t.Fatalf("want old provider + admission only, got %#v", items)
	}
}

func TestIsBrainInputAdmissionRequiresExplicitFlag(t *testing.T) {
	body := "heuristic body"
	legacyShaped := TimelineItem{
		ID:              "msh1e2ak_atzbs1",
		Kind:            "user_message",
		Body:            body,
		AdmissionSHA256: AdmissionDigest(body),
	}
	if IsBrainInputAdmission(legacyShaped) {
		t.Fatal("runtime identity must not use receipt/id heuristics")
	}
	legacyShaped.BrainAdmission = true
	if !IsBrainInputAdmission(legacyShaped) {
		t.Fatal("explicit brain_admission must qualify")
	}
}

func TestProviderUserEchoSuppressionsAreOneToOne(t *testing.T) {
	body := "echo body"
	digest := AdmissionDigest(body)
	items := []TimelineItem{{
		ID:                   "receipt-1",
		SessionID:            "provider-session",
		Kind:                 "user_message",
		Body:                 body,
		BrainAdmission:       true,
		AdmissionSHA256:      digest,
		AdmissionEchoEventID: "echo-1",
	}, {
		ID:                   "receipt-2",
		SessionID:            "provider-session",
		Kind:                 "user_message",
		Body:                 body,
		BrainAdmission:       true,
		AdmissionSHA256:      digest,
		AdmissionEchoEventID: "echo-2",
	}, {
		ID:   "provider-native:1",
		Kind: "user_message",
		Body: body,
	}}
	suppress := ProviderUserEchoSuppressions(items, "provider-session")
	if suppress["provider-native:1"] {
		t.Fatalf("durable provider row consumed a credit: %#v", suppress)
	}
	if !suppress["echo-1"] || !suppress["echo-2"] || suppress["terminal-3"] {
		t.Fatalf("suppress = %#v", suppress)
	}
	if replacement := ProviderUserEchoSuppressions(items, "replacement-session"); len(replacement) != 0 {
		t.Fatalf("replacement Session inherited echo claims: %#v", replacement)
	}
}
