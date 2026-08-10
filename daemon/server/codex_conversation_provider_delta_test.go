package server

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/watcher"
	"github.com/daoleno/zen/daemon/work"
)

// P0 regression: non-OpenCode providers (Codex, Claude, Cursor, Grok, Pi) and
// Brain-thread scopes have no authoritative content version, so every poll
// must use the full fingerprint and delta path. A stable-ID in-place update
// (streaming text, tool state, partial flags, Brain work-card changes) must
// produce a delta — the memoized fingerprint shortcut must never suppress it.

func TestNonOpenCodeProvidersFlowStableIDInPlaceDeltas(t *testing.T) {
	baseTime := time.Date(2026, 7, 16, 6, 0, 0, 0, time.UTC)
	providers := []struct {
		command string
		source  string
	}{
		{command: "codex", source: "codex_rollout"},
		{command: "claude", source: "claude_transcript"},
		{command: "cursor", source: "cursor_transcript"},
		{command: "grok", source: "grok_session"},
		{command: "pi", source: "pi_session"},
	}
	for _, provider := range providers {
		t.Run(provider.command, func(t *testing.T) {
			var loads atomic.Int32
			srv := &Server{
				watcher: watcher.New(time.Second),
				providerConversationLoader: func(_ *work.ProviderConversationReader, agentID string) (work.CodexConversation, error) {
					load := loads.Add(1)
					events := []work.CodexConversationEvent{{
						ID:        "event:streaming",
						Seq:       1,
						Timestamp: baseTime.Format(time.RFC3339Nano),
						Kind:      "assistant_message",
						Role:      "assistant",
						Body:      "First",
						Partial:   load == 1,
					}}
					if load >= 2 {
						// Stable-ID in-place update: same id, changed body and
						// partial state, exactly like streaming text/tool output.
						events[0].Body = "First Second"
						events[0].Partial = false
					}
					return work.CodexConversation{
						Available: true,
						Source:    provider.source,
						Path:      "/provider/session.jsonl",
						SessionID: "provider-session",
						CWD:       "/provider/workspace",
						Events:    events,
					}, nil
				},
			}
			conn := openThinProxyTestSocket(t, srv)
			request := clientMessage{
				Type:      "codex_conversation_subscribe",
				RequestID: "subscription-inplace",
				TargetID:  "provider-agent",
				Cwd:       "/provider/workspace",
				Command:   provider.command,
				StartedAt: json.RawMessage(`"2026-07-16T06:00:00Z"`),
			}
			if err := conn.WriteJSON(request); err != nil {
				t.Fatal(err)
			}
			if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
				t.Fatal(err)
			}
			var snapshot struct {
				Type         string                 `json:"type"`
				Conversation work.CodexConversation `json:"conversation"`
			}
			if err := conn.ReadJSON(&snapshot); err != nil {
				t.Fatal(err)
			}
			if snapshot.Type != "codex_conversation_snapshot" ||
				snapshot.Conversation.Events[0].Body != "First" ||
				!snapshot.Conversation.Events[0].Partial {
				t.Fatalf("snapshot = %#v", snapshot)
			}
			// The second poll carries the stable-ID in-place update; the full
			// fingerprint path must send a delta with the upserted event.
			var delta struct {
				Type    string                     `json:"type"`
				Upserts []work.CodexConversationEvent `json:"upserts"`
			}
			if err := conn.ReadJSON(&delta); err != nil {
				t.Fatal(err)
			}
			if delta.Type != "codex_conversation_delta" {
				t.Fatalf("second message = %#v", delta)
			}
			if len(delta.Upserts) != 1 ||
				delta.Upserts[0].ID != "event:streaming" ||
				delta.Upserts[0].Body != "First Second" ||
				delta.Upserts[0].Partial {
				t.Fatalf("in-place upsert = %#v", delta.Upserts)
			}
		})
	}
}

func TestBrainThreadScopeFlowsWorkCardDeltas(t *testing.T) {
	baseTime := time.Date(2026, 7, 16, 6, 0, 0, 0, time.UTC)
	service, calendarStore := newBrainCalendarFixture(t, "thread-1")
	result := finishScheduledResult(t, calendarStore, "item", "Daily papers", "thread-1", "Three papers.", "")
	srv := &Server{
		watcher:  watcher.New(time.Second),
		brain:    service,
		calendar: calendarStore,
		providerConversationLoader: func(_ *work.ProviderConversationReader, agentID string) (work.CodexConversation, error) {
			return work.CodexConversation{
				Available: true,
				Source:    "codex_rollout",
				Path:      "/provider/session.jsonl",
				SessionID: "provider-session",
				CWD:       "/provider/workspace",
				Events: []work.CodexConversationEvent{{
					ID:        "event:stable",
					Seq:       1,
					Timestamp: baseTime.Format(time.RFC3339Nano),
					Kind:      "assistant_message",
					Role:      "assistant",
					Body:      "Stable history.",
				}},
			}, nil
		},
	}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:                 "codex_conversation_subscribe",
		RequestID:            "subscription-brain",
		TargetID:             "provider-agent",
		Cwd:                  "/provider/workspace",
		Command:              "codex",
		StartedAt:            json.RawMessage(`"2026-07-16T06:00:00Z"`),
		ConversationScopeKey: "brain-thread:thread-1",
	}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Type         string                 `json:"type"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	hasWorkCard := func(events []work.CodexConversationEvent) bool {
		for _, event := range events {
			if event.ID == result.ID && event.Source == "calendar_result" {
				return true
			}
		}
		return false
	}
	if !hasWorkCard(snapshot.Conversation.Events) {
		t.Fatalf("first snapshot lacks the Brain work card: %#v", snapshot.Conversation.Events)
	}

	// The Brain overlay changes independently of the provider source: a second
	// result lands while the provider history is byte-identical. The scope is
	// not eligible for the memoized path, so the full fingerprint must emit a
	// delta carrying the new work card.
	second := finishScheduledResult(t, calendarStore, "item-2", "Evening notes", "thread-1", "Second card.", "")
	var delta struct {
		Type    string                     `json:"type"`
		Upserts []work.CodexConversationEvent `json:"upserts"`
	}
	if err := conn.ReadJSON(&delta); err != nil {
		t.Fatal(err)
	}
	if delta.Type != "codex_conversation_delta" {
		t.Fatalf("second message = %#v", delta)
	}
	found := false
	for _, event := range delta.Upserts {
		if event.ID == second.ID && event.Source == "calendar_result" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Brain work-card change missing from delta: %#v", delta.Upserts)
	}
}

func recentOpenCodeServerDeltaFixtureStart(now time.Time) time.Time {
	return now.UTC().Add(-time.Hour).Truncate(time.Second)
}

// P1 regression: a deleted OpenCode row must be reported through the server
// delta as an authoritative delete (the reader's count-mismatch full read
// removes it from the conversation, and the id-set diff emits the delete).
func TestOpenCodeDeletionFlowsServerDelta(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	started := recentOpenCodeServerDeltaFixtureStart(time.Now())
	dbPath := t.TempDir() + "/opencode.db"
	schema := `
CREATE TABLE project (id TEXT PRIMARY KEY);
CREATE TABLE session (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, parent_id TEXT,
  slug TEXT NOT NULL, directory TEXT NOT NULL, title TEXT NOT NULL,
  version TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL
);
CREATE TABLE message (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL, data TEXT NOT NULL
);
CREATE TABLE part (
  id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL
);
INSERT INTO project(id) VALUES ('proj');
`
	seed := fmt.Sprintf(schema+
		"INSERT INTO session(id, project_id, parent_id, slug, directory, title, version, time_created, time_updated) VALUES ('ses_srvdel', 'proj', NULL, 'slug', '/repo/srvdel', 't', '1', %d, %d);\n"+
		"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('msg_a', 'ses_srvdel', %d, %d, '{\"role\":\"user\"}');\n"+
		"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('msg_b', 'ses_srvdel', %d, %d, '{\"role\":\"user\"}');\n"+
		"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('prt_a', 'msg_a', 'ses_srvdel', %d, %d, '{\"type\":\"text\",\"text\":\"alpha\"}');\n"+
		"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('prt_b', 'msg_b', 'ses_srvdel', %d, %d, '{\"type\":\"text\",\"text\":\"beta\"}');\n",
		started.UnixMilli(), started.Add(30*time.Second).UnixMilli(),
		started.Add(time.Second).UnixMilli(), started.Add(time.Second).UnixMilli(),
		started.Add(10*time.Second).UnixMilli(), started.Add(10*time.Second).UnixMilli(),
		started.Add(time.Second).UnixMilli(), started.Add(time.Second).UnixMilli(),
		started.Add(10*time.Second).UnixMilli(), started.Add(10*time.Second).UnixMilli(),
	)
	cmd := exec.Command("sqlite3", dbPath)
	cmd.Stdin = strings.NewReader(seed)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v: %s", err, out)
	}
	t.Setenv("ZEN_OPENCODE_DB", dbPath)
	srv := &Server{watcher: watcher.New(time.Second)}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "subscription-del",
		TargetID:  "agent-del",
		Cwd:       "/repo/srvdel",
		Command:   "opencode",
		StartedAt: json.RawMessage(fmt.Sprintf("%q", started.Format(time.RFC3339Nano))),
	}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Type         string                 `json:"type"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	ids := map[string]bool{}
	for _, event := range snapshot.Conversation.Events {
		ids[event.ID] = true
	}
	if !ids["msg_a"] || !ids["msg_b"] {
		t.Fatalf("snapshot events = %v", ids)
	}

	// Delete the msg_a turn: the next poll must emit a delta deleting msg_a.
	stmt := "DELETE FROM part WHERE id = 'prt_a';\nDELETE FROM message WHERE id = 'msg_a';\n"
	delCmd := exec.Command("sqlite3", dbPath)
	delCmd.Stdin = strings.NewReader(stmt)
	if out, err := delCmd.CombinedOutput(); err != nil {
		t.Fatalf("delete: %v: %s", err, out)
	}
	var delta struct {
		Type    string   `json:"type"`
		Deletes []string `json:"deletes"`
	}
	if err := conn.ReadJSON(&delta); err != nil {
		t.Fatal(err)
	}
	if delta.Type != "codex_conversation_delta" {
		t.Fatalf("second message = %#v", delta)
	}
	found := false
	for _, id := range delta.Deletes {
		if id == "msg_a" {
			found = true
		}
	}
	if !found {
		t.Fatalf("deleted event id missing from delta deletes: %#v", delta.Deletes)
	}
}

// The memoized path (content-versioned OpenCode reader) must still deliver an
// in-place streaming update: the reader reports the changed event id, the
// memoized fingerprint recomputes it, and the delta carries the new body.
func TestOpenCodeInPlaceUpdateFlowsMemoizedDelta(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	started := recentOpenCodeServerDeltaFixtureStart(time.Now())
	dbPath := t.TempDir() + "/opencode.db"
	schema := `
CREATE TABLE project (id TEXT PRIMARY KEY);
CREATE TABLE session (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, parent_id TEXT,
  slug TEXT NOT NULL, directory TEXT NOT NULL, title TEXT NOT NULL,
  version TEXT NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL
);
CREATE TABLE message (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL, time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL, data TEXT NOT NULL
);
CREATE TABLE part (
  id TEXT PRIMARY KEY, message_id TEXT NOT NULL, session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL
);
INSERT INTO project(id) VALUES ('proj');
`
	seed := fmt.Sprintf(schema+
		"INSERT INTO session(id, project_id, parent_id, slug, directory, title, version, time_created, time_updated) VALUES ('ses_inc', 'proj', NULL, 'slug', '/repo/inc', 't', '1', %d, %d);\n"+
		"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('msg_1', 'ses_inc', %d, %d, '{\"role\":\"user\"}');\n"+
		"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('msg_2', 'ses_inc', %d, %d, '{\"role\":\"assistant\"}');\n"+
		"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('prt_1', 'msg_1', 'ses_inc', %d, %d, '{\"type\":\"text\",\"text\":\"hello\"}');\n"+
		"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('prt_2', 'msg_2', 'ses_inc', %d, %d, '{\"type\":\"text\",\"text\":\"first\"}');\n",
		started.UnixMilli(), started.Add(30*time.Second).UnixMilli(),
		started.Add(time.Second).UnixMilli(), started.Add(time.Second).UnixMilli(),
		started.Add(2*time.Second).UnixMilli(), started.Add(2*time.Second).UnixMilli(),
		started.Add(time.Second).UnixMilli(), started.Add(time.Second).UnixMilli(),
		started.Add(2*time.Second).UnixMilli(), started.Add(2*time.Second).UnixMilli(),
	)
	cmd := exec.Command("sqlite3", dbPath)
	cmd.Stdin = strings.NewReader(seed)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v: %s", err, out)
	}
	t.Setenv("ZEN_OPENCODE_DB", dbPath)
	srv := &Server{watcher: watcher.New(time.Second)}
	conn := openThinProxyTestSocket(t, srv)
	request := clientMessage{
		Type:      "codex_conversation_subscribe",
		RequestID: "subscription-inc",
		TargetID:  "agent-inc",
		Cwd:       "/repo/inc",
		Command:   "opencode",
		StartedAt: json.RawMessage(fmt.Sprintf("%q", started.Format(time.RFC3339Nano))),
	}
	if err := conn.WriteJSON(request); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Type         string                 `json:"type"`
		Conversation work.CodexConversation `json:"conversation"`
	}
	if err := conn.ReadJSON(&snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Type != "codex_conversation_snapshot" {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	// Streaming in-place update: grow the assistant text part row in place.
	update := fmt.Sprintf(
		"UPDATE part SET data = '{\"type\":\"text\",\"text\":\"first second\"}', time_updated = %d WHERE id = 'prt_2';\n",
		started.Add(3*time.Second).UnixMilli(),
	)
	updCmd := exec.Command("sqlite3", dbPath)
	updCmd.Stdin = strings.NewReader(update)
	if out, err := updCmd.CombinedOutput(); err != nil {
		t.Fatalf("update: %v: %s", err, out)
	}
	var delta struct {
		Type    string                     `json:"type"`
		Upserts []work.CodexConversationEvent `json:"upserts"`
	}
	if err := conn.ReadJSON(&delta); err != nil {
		t.Fatal(err)
	}
	if delta.Type != "codex_conversation_delta" {
		t.Fatalf("second message = %#v", delta)
	}
	found := false
	for _, event := range delta.Upserts {
		if event.ID == "prt_2" && event.Body == "first second" {
			found = true
		}
	}
	if !found {
		t.Fatalf("in-place update missing from memoized delta: %#v", delta.Upserts)
	}
}
