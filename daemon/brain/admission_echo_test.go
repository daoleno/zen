package brain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

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

func TestProviderRowsOutsideExactAdmissionWindowRemainIndependent(t *testing.T) {
	base := time.Date(2026, 8, 11, 14, 0, 0, 0, time.UTC)
	tests := []struct {
		name            string
		providerSession string
		providerAt      time.Time
	}{
		{name: "before prepare", providerSession: "provider-session", providerAt: base.Add(-time.Nanosecond)},
		{name: "after accept", providerSession: "provider-session", providerAt: base.Add(time.Second + time.Nanosecond)},
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
	first, err := store.AdmitUserMessage(threadID, "host-session", "receipt-a", body)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AdmitUserMessage(threadID, "host-session", "receipt-b", body)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || !first.BrainAdmission || !second.BrainAdmission {
		t.Fatalf("admissions = %#v %#v", first, second)
	}

	echoA := "provider-session:11"
	echoB := "provider-session:12"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "provider-session",
		Events: []work.CodexConversationEvent{{
			ID:              echoA,
			Timestamp:       "2026-08-06T05:00:00Z",
			Kind:            "user_message",
			Role:            "user",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}, {
			ID:              echoB,
			Timestamp:       "2026-08-06T05:00:01Z",
			Kind:            "user_message",
			Role:            "user",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}, {
			ID:        "provider-session:13",
			Timestamp: "2026-08-06T05:00:02Z",
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
	if _, err := store.AdmitUserMessage(threadID, "host-session", "receipt-once", body); err != nil {
		t.Fatal(err)
	}
	echoID := "019fcf7a-session:10"
	terminalID := "019fcf7a-session:20"
	if err := store.MaterializeProviderConversation(threadID, work.CodexConversation{
		Available: true,
		SessionID: "019fcf7a-session",
		Events: []work.CodexConversationEvent{{
			ID:              echoID,
			Timestamp:       "2026-08-06T05:10:00Z",
			Kind:            "user_message",
			Body:            body,
			AdmissionSHA256: AdmissionDigest(body),
		}, {
			ID:              terminalID,
			Timestamp:       "2026-08-06T05:11:00Z",
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

func TestLegacyPreFieldAdmissionMigrationAndProviderDigestStrip(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	threadID := "thread-legacy"
	legacyAdmission := TimelineItem{
		ID:              "msh1e2ak_atzbs1",
		ThreadID:        threadID,
		SessionID:       "019fcf7a-485b-7961-ad2f-dbe9f6eab2d2",
		Role:            "user",
		Body:            "pre-field admitted body",
		CreatedAt:       time.Date(2026, 8, 6, 4, 49, 0, 0, time.UTC),
		Kind:            "user_message",
		AdmissionSHA256: AdmissionDigest("pre-field admitted body"),
	}
	legacyProvider := TimelineItem{
		ID:              "rollout-2026-08-05T09-12-05-019fcf7a.jsonl:1791317",
		ThreadID:        threadID,
		SessionID:       "019fcf7a-485b-7961-ad2f-dbe9f6eab2d2",
		Role:            "user",
		Body:            "older provider user",
		CreatedAt:       time.Date(2026, 8, 5, 9, 12, 0, 0, time.UTC),
		Kind:            "user_message",
		AdmissionSHA256: AdmissionDigest("older provider user"),
	}
	path := filepath.Join(stateDir, "messages.jsonl")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []TimelineItem{legacyProvider, legacyAdmission} {
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(append(raw, '\n')); err != nil {
			t.Fatal(err)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ThreadTimeline(threadID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("migrated timeline = %#v", items)
	}
	byID := map[string]TimelineItem{}
	for _, item := range items {
		byID[item.ID] = item
	}
	admission := byID[legacyAdmission.ID]
	provider := byID[legacyProvider.ID]
	if !admission.BrainAdmission || admission.AdmissionSHA256 == "" {
		t.Fatalf("legacy admission not promoted: %#v", admission)
	}
	if provider.BrainAdmission || provider.AdmissionSHA256 != "" {
		t.Fatalf("legacy provider still has admission credit: %#v", provider)
	}
	if _, err := os.Stat(filepath.Join(stateDir, admissionProvenanceMarker)); err != nil {
		t.Fatalf("missing provenance marker: %v", err)
	}
	// Runtime identity is explicit-only after migration.
	if !IsBrainInputAdmission(admission) || IsBrainInputAdmission(provider) {
		t.Fatalf("runtime admission identity drifted: adm=%#v prov=%#v", admission, provider)
	}
}

func TestMigrationIgnoresNonPreFieldShapes(t *testing.T) {
	// Digest that does not match body is not the unreleased AdmitUserMessage shape.
	item := TimelineItem{
		ID:              "receipt-shaped",
		Kind:            "user_message",
		Body:            "actual body",
		AdmissionSHA256: AdmissionDigest("different body"),
	}
	if isUnreleasedPreFieldAdmission(item) {
		t.Fatal("mismatched digest must not promote")
	}
	applyAdmissionProvenanceMigration(&item)
	if item.BrainAdmission || item.AdmissionSHA256 != "" {
		t.Fatalf("mismatched digest must strip correlation, not promote: %#v", item)
	}
	providerShaped := TimelineItem{
		ID:              "session:1",
		Kind:            "user_message",
		Body:            "x",
		AdmissionSHA256: AdmissionDigest("x"),
	}
	if isUnreleasedPreFieldAdmission(providerShaped) {
		t.Fatal("provider id shape must not promote")
	}
	applyAdmissionProvenanceMigration(&providerShaped)
	if providerShaped.BrainAdmission || providerShaped.AdmissionSHA256 != "" {
		t.Fatalf("provider shape must strip correlation: %#v", providerShaped)
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

	if _, err := store.AdmitUserMessage(threadID, "host-session", "receipt-later", body); err != nil {
		t.Fatal(err)
	}
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
			Timestamp:       "2026-08-06T05:30:00Z",
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
		ID:              "receipt-1",
		Kind:            "user_message",
		Body:            body,
		BrainAdmission:  true,
		AdmissionSHA256: digest,
	}, {
		ID:              "receipt-2",
		Kind:            "user_message",
		Body:            body,
		BrainAdmission:  true,
		AdmissionSHA256: digest,
	}, {
		ID:   "provider-native:1",
		Kind: "user_message",
		Body: body,
	}}
	events := []work.CodexConversationEvent{{
		ID:              "provider-native:1",
		Kind:            "user_message",
		Body:            body,
		AdmissionSHA256: digest,
	}, {
		ID:              "echo-1",
		Kind:            "user_message",
		Body:            body,
		AdmissionSHA256: digest,
	}, {
		ID:              "echo-2",
		Kind:            "user_message",
		Body:            body,
		AdmissionSHA256: digest,
	}, {
		ID:              "terminal-3",
		Kind:            "user_message",
		Body:            body,
		AdmissionSHA256: digest,
	}}
	suppress := ProviderUserEchoSuppressions(items, events)
	if suppress["provider-native:1"] {
		t.Fatalf("durable provider row consumed a credit: %#v", suppress)
	}
	if !suppress["echo-1"] || !suppress["echo-2"] || suppress["terminal-3"] {
		t.Fatalf("suppress = %#v", suppress)
	}
}
