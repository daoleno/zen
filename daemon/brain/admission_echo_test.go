package brain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/work"
)

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
