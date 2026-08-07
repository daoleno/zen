package work

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// P1 regression: deleted SQLite rows must leave the projected conversation.
// A time_updated cursor can never see a deletion (the deleted row's
// time_updated is older than the cursor), so the count-drop detection must
// trigger a full cursor-zero re-read that atomically replaces the row and
// payload caches and rebuilds without the removed events.

func TestOpenCodeMessageAndPartDeletionLeavesConversation(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	started := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	dbPath := t.TempDir() + "/opencode.db"
	session := []openCodeSessionSeed{
		{ID: "ses_del", Directory: "/repo/del", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(30 * time.Second).UnixMilli()},
	}
	messages := []openCodeMessageSeed{
		{ID: "msg_a", SessionID: "ses_del", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_b", SessionID: "ses_del", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_c", SessionID: "ses_del", CreatedMS: started.Add(20 * time.Second).UnixMilli(), Data: `{"role":"assistant","finish":"stop","time":{"created":1,"completed":` + fmt.Sprintf("%d", started.Add(22*time.Second).UnixMilli()) + `}}`},
	}
	parts := []openCodePartSeed{
		{ID: "prt_a", MessageID: "msg_a", SessionID: "ses_del", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"alpha"}`},
		{ID: "prt_b", MessageID: "msg_b", SessionID: "ses_del", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: `{"type":"text","text":"beta"}`},
		{ID: "prt_tool", MessageID: "msg_c", SessionID: "ses_del", CreatedMS: started.Add(21 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"bash","callID":"call_del","state":{"status":"completed","input":"{}","output":"out"}}`},
	}
	createOpenCodeFixtureDB(t, dbPath, session, messages, parts)
	t.Setenv("ZEN_OPENCODE_DB", dbPath)

	reader := NewProviderConversationReader()
	agent := classifier.Agent{Cwd: "/repo/del", Command: "opencode", StartedAt: started}
	first, err := reader.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reader.ConversationVersion() == 0 {
		t.Fatal("no content version")
	}
	eventIDs := func(events []CodexConversationEvent) map[string]bool {
		ids := map[string]bool{}
		for _, event := range events {
			ids[event.ID] = true
		}
		return ids
	}
	before := eventIDs(first.Events)
	for _, want := range []string{"msg_a", "msg_b", "prt_tool"} {
		if !before[want] {
			t.Fatalf("initial conversation missing %q: %v", want, before)
		}
	}

	// Delete one full message turn (message row + its text part) and one tool
	// part from the other message. The SQLite file changes, but none of the
	// remaining rows have time_updated newer than the cursor, so only the
	// count-drop detection can notice.
	stmt := "DELETE FROM part WHERE id = 'prt_a';\n" +
		"DELETE FROM message WHERE id = 'msg_a';\n" +
		"DELETE FROM part WHERE id = 'prt_tool';\n"
	cmd := exec.Command("sqlite3", dbPath)
	cmd.Stdin = strings.NewReader(stmt)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("delete rows: %v: %s", err, out)
	}

	second, err := reader.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if reader.ConversationVersion() == 0 {
		t.Fatal("version lost after deletion")
	}
	after := eventIDs(second.Events)
	if after["msg_a"] {
		t.Fatalf("deleted user message still visible: %v", after)
	}
	if after["prt_tool"] {
		t.Fatalf("deleted tool part still visible: %v", after)
	}
	if !after["msg_b"] {
		t.Fatalf("surviving message vanished: %v", after)
	}

	// The refresh must also survive a subsequent unchanged poll (the cursors
	// and stamp must have advanced past the deletion full read).
	third, err := reader.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ids := eventIDs(third.Events); ids["msg_a"] || ids["prt_tool"] || !ids["msg_b"] {
		t.Fatalf("post-deletion state drifted: %v", ids)
	}

	// A fresh subscription (new reader) must observe the deleted state too.
	fresh := NewProviderConversationReader()
	freshConversation, err := fresh.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if ids := eventIDs(freshConversation.Events); ids["msg_a"] || ids["prt_tool"] {
		t.Fatalf("fresh reader sees deleted rows: %v", ids)
	}
}

func TestOpenCodeDeletionThenReinsertRecovers(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	started := time.Date(2026, 8, 7, 0, 0, 0, 0, time.UTC)
	dbPath := t.TempDir() + "/opencode.db"
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_del2", Directory: "/repo/del2", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(30 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_x", SessionID: "ses_del2", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
	}, []openCodePartSeed{
		{ID: "prt_x", MessageID: "msg_x", SessionID: "ses_del2", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"x"}`},
	})
	t.Setenv("ZEN_OPENCODE_DB", dbPath)
	reader := NewProviderConversationReader()
	agent := classifier.Agent{Cwd: "/repo/del2", Command: "opencode", StartedAt: started}
	first, err := reader.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 1 {
		t.Fatalf("initial events = %d", len(first.Events))
	}
	// Delete the only turn (count drops to zero), then insert a fresh turn:
	// the reader must converge to the empty state and then to the new state.
	stmt := "DELETE FROM part WHERE id = 'prt_x';\nDELETE FROM message WHERE id = 'msg_x';\n"
	cmd := exec.Command("sqlite3", dbPath)
	cmd.Stdin = strings.NewReader(stmt)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("delete rows: %v: %s", err, out)
	}
	empty, err := reader.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Events) != 0 {
		t.Fatalf("post-deletion events = %#v", empty.Events)
	}
	insert := fmt.Sprintf(
		"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('msg_y', 'ses_del2', %d, %d, '{\"role\":\"user\"}');\n"+
			"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('prt_y', 'msg_y', 'ses_del2', %d, %d, '{\"type\":\"text\",\"text\":\"y\"}');\n",
		started.Add(3*time.Second).UnixMilli(), started.Add(3*time.Second).UnixMilli(),
		started.Add(3*time.Second).UnixMilli(), started.Add(3*time.Second).UnixMilli(),
	)
	cmd = exec.Command("sqlite3", dbPath)
	cmd.Stdin = strings.NewReader(insert)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("reinsert: %v: %s", err, out)
	}
	second, err := reader.Load(agent, AgentProviderOpenCode, started.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 1 || second.Events[0].Body != "y" {
		t.Fatalf("post reinsert events = %#v", second.Events)
	}
}
