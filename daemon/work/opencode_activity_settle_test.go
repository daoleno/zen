package work

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openCodeActivityFixtureDB(t *testing.T, started time.Time, assistantMessages []openCodeMessageSeed, toolRunning bool) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	seeds := []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_act", CreatedMS: started.UnixMilli(), Data: `{"role":"user"}`},
	}
	seeds = append(seeds, assistantMessages...)
	parts := []openCodePartSeed{
		{ID: "p_user", MessageID: "msg_user", SessionID: "ses_act", CreatedMS: started.UnixMilli(), Data: `{"type":"text","text":"task"}`},
	}
	if toolRunning {
		parts = append(parts, openCodePartSeed{
			ID: "p_tool", MessageID: "msg_yield", SessionID: "ses_act",
			CreatedMS: started.Add(2 * time.Second).UnixMilli(),
			Data:      `{"type":"tool","tool":"bash","callID":"call-1","state":{"status":"running"}}`,
		})
	}
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_act", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(5 * time.Second).UnixMilli()},
	}, seeds, parts)
	return dbPath
}

func TestOpenCodeToolCallFinishKeepsTurnActivityRunning(t *testing.T) {
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	dbPath := openCodeActivityFixtureDB(t, started, []openCodeMessageSeed{
		{
			ID: "msg_yield", SessionID: "ses_act",
			CreatedMS: started.Add(time.Second).UnixMilli(),
			Data: fmt.Sprintf(
				`{"role":"assistant","finish":"tool-calls","time":{"created":1,"completed":%d}}`,
				started.Add(3*time.Second).UnixMilli(),
			),
		},
	}, true)
	got, err := parseOpenCodeConversation(dbPath, "ses_act")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityRunning {
		t.Fatalf("tool-calls finish must not settle a live turn: %+v", got.Activity)
	}
}

func TestOpenCodeToolCallYieldThenTerminalFinishSettlesExactlyOnce(t *testing.T) {
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	dbPath := openCodeActivityFixtureDB(t, started, []openCodeMessageSeed{
		{
			ID: "msg_yield", SessionID: "ses_act",
			CreatedMS: started.Add(time.Second).UnixMilli(),
			Data: fmt.Sprintf(
				`{"role":"assistant","finish":"tool-calls","time":{"created":1,"completed":%d}}`,
				started.Add(3*time.Second).UnixMilli(),
			),
		},
		{
			ID: "msg_final", SessionID: "ses_act",
			CreatedMS: started.Add(4 * time.Second).UnixMilli(),
			Data: fmt.Sprintf(
				`{"role":"assistant","finish":"stop","time":{"created":1,"completed":%d}}`,
				started.Add(6*time.Second).UnixMilli(),
			),
		},
	}, true)
	got, err := parseOpenCodeConversation(dbPath, "ses_act")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("terminal stop finish must settle the turn: %+v", got.Activity)
	}
	if got.Activity.SettledAt != normalizeCodexTimestamp(started.Add(6*time.Second).UTC().Format(time.RFC3339Nano)) {
		t.Fatalf("settled at = %q, want final message completed time", got.Activity.SettledAt)
	}
}

func TestOpenCodeUnknownFinishKeepsTurnActivityRunning(t *testing.T) {
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	dbPath := openCodeActivityFixtureDB(t, started, []openCodeMessageSeed{
		{
			ID: "msg_yield", SessionID: "ses_act",
			CreatedMS: started.Add(time.Second).UnixMilli(),
			Data: fmt.Sprintf(
				`{"role":"assistant","finish":"unknown","time":{"created":1,"completed":%d}}`,
				started.Add(2*time.Second).UnixMilli(),
			),
		},
	}, false)
	got, err := parseOpenCodeConversation(dbPath, "ses_act")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityRunning {
		t.Fatalf("unknown finish must keep the turn running, never falsely complete: %+v", got.Activity)
	}
}

func TestOpenCodeFinishLessCompletedMessageStillSettlesWhenToolsFinished(t *testing.T) {
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	dbPath := openCodeActivityFixtureDB(t, started, []openCodeMessageSeed{
		{
			ID: "msg_final", SessionID: "ses_act",
			CreatedMS: started.Add(4 * time.Second).UnixMilli(),
			Data: fmt.Sprintf(
				`{"role":"assistant","time":{"created":1,"completed":%d}}`,
				started.Add(6*time.Second).UnixMilli(),
			),
		},
	}, false)
	got, err := parseOpenCodeConversation(dbPath, "ses_act")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("finish-less completed message must settle the turn: %+v", got.Activity)
	}
}

// openCodeLiveTurnFixtureDB seeds the real OpenCode v1.18 build-mode shape for
// one long turn: the assistant echoes a tool-call step per message, every step
// closes with a step-finish part whose reason is tool-calls, and the message
// row mirrors finish + time.completed. With terminal=true the turn ends with a
// stop-finish message. The observed failure was the parser settling the whole
// turn at each intermediate step-finish while the provider was still Thinking/
// Preparing edit/Build.
func openCodeLiveTurnFixtureDB(t *testing.T, dbPath string, started time.Time, terminal bool) {
	t.Helper()
	session := []openCodeSessionSeed{
		{ID: "ses_live", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(30 * time.Second).UnixMilli()},
	}
	messages := []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_live", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user","time":{"created":1}}`},
		{ID: "msg_asst_1", SessionID: "ses_live", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: fmt.Sprintf(`{"role":"assistant","finish":"tool-calls","time":{"created":1,"completed":%d}}`, started.Add(6*time.Second).UnixMilli())},
		{ID: "msg_asst_2", SessionID: "ses_live", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: fmt.Sprintf(`{"role":"assistant","finish":"tool-calls","time":{"created":1,"completed":%d}}`, started.Add(15*time.Second).UnixMilli())},
	}
	parts := []openCodePartSeed{
		{ID: "p_user", MessageID: "msg_user", SessionID: "ses_live", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"task"}`},
		{ID: "p_s1", MessageID: "msg_asst_1", SessionID: "ses_live", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p_t1", MessageID: "msg_asst_1", SessionID: "ses_live", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"tool","tool":"bash","callID":"call-1","state":{"status":"completed","input":"{\"cmd\":\"go build\"}","output":"ok"}}`},
		{ID: "p_f1", MessageID: "msg_asst_1", SessionID: "ses_live", CreatedMS: started.Add(6 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"tool-calls"}`},
		{ID: "p_s2", MessageID: "msg_asst_2", SessionID: "ses_live", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p_x2", MessageID: "msg_asst_2", SessionID: "ses_live", CreatedMS: started.Add(11 * time.Second).UnixMilli(), Data: `{"type":"reasoning","text":"thinking"}`},
		{ID: "p_f2", MessageID: "msg_asst_2", SessionID: "ses_live", CreatedMS: started.Add(15 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"tool-calls"}`},
	}
	if terminal {
		messages = append(messages, openCodeMessageSeed{
			ID: "msg_asst_3", SessionID: "ses_live", CreatedMS: started.Add(20 * time.Second).UnixMilli(),
			Data: fmt.Sprintf(`{"role":"assistant","finish":"stop","time":{"created":1,"completed":%d}}`, started.Add(24*time.Second).UnixMilli()),
		})
		parts = append(parts,
			openCodePartSeed{ID: "p_s3", MessageID: "msg_asst_3", SessionID: "ses_live", CreatedMS: started.Add(20 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
			openCodePartSeed{ID: "p_t3", MessageID: "msg_asst_3", SessionID: "ses_live", CreatedMS: started.Add(21 * time.Second).UnixMilli(), Data: `{"type":"text","text":"done"}`},
			openCodePartSeed{ID: "p_f3", MessageID: "msg_asst_3", SessionID: "ses_live", CreatedMS: started.Add(24 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"stop"}`},
		)
	}
	createOpenCodeFixtureDB(t, dbPath, session, messages, parts)
}

// TestOpenCodeToolCallStepFinishNeverSettlesLiveTurn reproduces the observed
// failure on f461215: a live turn whose steps each close with a tool-calls
// step-finish must stay running; settling at any intermediate step-finish
// pinned the Session to done while OpenCode was still working.
func TestOpenCodeToolCallStepFinishNeverSettlesLiveTurn(t *testing.T) {
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	openCodeLiveTurnFixtureDB(t, dbPath, started, false)
	got, err := parseOpenCodeConversation(dbPath, "ses_live")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityRunning {
		t.Fatalf("mid-turn DB must stay running: %+v", got.Activity)
	}
	if got.Activity.SettledAt != "" {
		t.Fatalf("mid-turn DB must not settle: %+v", got.Activity)
	}
}

// TestOpenCodeLiveTurnSettlesExactlyOnceAtTerminalMessageFinish verifies the
// authoritative settlement: only the terminal stop-finish message settles the
// turn, exactly once, and the Activity identity stays bound to the user turn
// boundary across all intermediate tool-call steps.
func TestOpenCodeLiveTurnSettlesExactlyOnceAtTerminalMessageFinish(t *testing.T) {
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	openCodeLiveTurnFixtureDB(t, dbPath, started, true)
	got, err := parseOpenCodeConversation(dbPath, "ses_live")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("terminal message finish must settle exactly once: %+v", got.Activity)
	}
	wantSettled := normalizeCodexTimestamp(started.Add(24 * time.Second).UTC().Format(time.RFC3339Nano))
	if got.Activity.SettledAt != wantSettled {
		t.Fatalf("settled at = %q, want final stop message time %q", got.Activity.SettledAt, wantSettled)
	}
	if !strings.Contains(got.Activity.ID, "msg_user") {
		t.Fatalf("activity identity = %q, want the user turn boundary", got.Activity.ID)
	}
}

// TestOpenCodeFollowUpTurnRebindsActivityToNewUserBoundary verifies the
// follow-up causal pairing: after a completed turn, a new user message that
// lands while the new assistant is still mid-step must rebind the Activity to
// the new user boundary and stay running until the new turn's terminal
// message.
func TestOpenCodeFollowUpTurnRebindsActivityToNewUserBoundary(t *testing.T) {
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_fu", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(30 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user_1", SessionID: "ses_fu", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user","time":{"created":1}}`},
		{ID: "msg_asst_1", SessionID: "ses_fu", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: fmt.Sprintf(`{"role":"assistant","finish":"stop","time":{"created":1,"completed":%d}}`, started.Add(5*time.Second).UnixMilli())},
		{ID: "msg_user_2", SessionID: "ses_fu", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: `{"role":"user","time":{"created":1}}`},
		{ID: "msg_asst_2", SessionID: "ses_fu", CreatedMS: started.Add(12 * time.Second).UnixMilli(), Data: fmt.Sprintf(`{"role":"assistant","finish":"tool-calls","time":{"created":1,"completed":%d}}`, started.Add(16*time.Second).UnixMilli())},
	}, []openCodePartSeed{
		{ID: "p_u1", MessageID: "msg_user_1", SessionID: "ses_fu", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"first"}`},
		{ID: "p_s1", MessageID: "msg_asst_1", SessionID: "ses_fu", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p_t1", MessageID: "msg_asst_1", SessionID: "ses_fu", CreatedMS: started.Add(3 * time.Second).UnixMilli(), Data: `{"type":"text","text":"reply"}`},
		{ID: "p_f1", MessageID: "msg_asst_1", SessionID: "ses_fu", CreatedMS: started.Add(5 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"stop"}`},
		{ID: "p_u2", MessageID: "msg_user_2", SessionID: "ses_fu", CreatedMS: started.Add(10 * time.Second).UnixMilli(), Data: `{"type":"text","text":"follow-up"}`},
		{ID: "p_s2", MessageID: "msg_asst_2", SessionID: "ses_fu", CreatedMS: started.Add(12 * time.Second).UnixMilli(), Data: `{"type":"step-start"}`},
		{ID: "p_x2", MessageID: "msg_asst_2", SessionID: "ses_fu", CreatedMS: started.Add(13 * time.Second).UnixMilli(), Data: `{"type":"reasoning","text":"thinking"}`},
		{ID: "p_f2", MessageID: "msg_asst_2", SessionID: "ses_fu", CreatedMS: started.Add(16 * time.Second).UnixMilli(), Data: `{"type":"step-finish","reason":"tool-calls"}`},
	})
	got, err := parseOpenCodeConversation(dbPath, "ses_fu")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityRunning {
		t.Fatalf("follow-up mid-turn must stay running: %+v", got.Activity)
	}
	if !strings.Contains(got.Activity.ID, "msg_user_2") {
		t.Fatalf("follow-up activity identity = %q, want rebind to msg_user_2", got.Activity.ID)
	}
}

// TestOpenCodeConversationCacheInvalidatesOnWalWrites verifies the reader
// cache invalidates when new rows land only in the SQLite WAL file (the main
// db file's stat changes only at checkpoint). Production OpenCode holds its
// database connection open, so new user turns stay in the WAL between
// checkpoints; without the WAL in the stamp, the provider observation kept
// reusing the previous completed Activity after a new user turn was already
// in the DB and the delegated turn settled prematurely.
func TestOpenCodeConversationCacheInvalidatesOnWalWrites(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 required")
	}
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_wal", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(5 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_wal", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
	}, []openCodePartSeed{
		{ID: "p_user", MessageID: "msg_user", SessionID: "ses_wal", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"first"}`},
	})
	// A persistent writer connection mirrors production OpenCode: it holds the
	// database open in WAL mode, so writes stay in the -wal file.
	writer := exec.Command("sqlite3", dbPath)
	stdin, err := writer.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var writerLog bytes.Buffer
	writer.Stdout = &writerLog
	writer.Stderr = &writerLog
	if err := writer.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = stdin.Close()
		_ = writer.Process.Kill()
		_ = writer.Wait()
	}()
	// Switch to WAL and force a committed write so the -wal file materializes.
	if _, err := fmt.Fprintf(stdin,
		".bail on\nPRAGMA journal_mode=WAL;\nUPDATE session SET time_updated = time_updated + 1 WHERE id='ses_wal';\n",
	); err != nil {
		t.Fatal(err)
	}
	walPath := dbPath + "-wal"
	walAppeared := false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(walPath); err == nil && info.Size() > 0 {
			walAppeared = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !walAppeared {
		t.Fatalf("wal file never appeared: %s", writerLog.String())
	}
	reader := NewProviderConversationReader()
	first, err := reader.loadOpenCodeConversation(dbPath, "ses_wal")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 1 || first.Events[0].Kind != "user_message" {
		t.Fatalf("first conversation = %#v", first.Events)
	}
	walBefore, err := os.Stat(walPath)
	if err != nil {
		t.Fatal(err)
	}
	created := started.Add(2 * time.Second).UnixMilli()
	insert := fmt.Sprintf(
		`INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('msg_new', 'ses_wal', %d, %d, '{"role":"user"}');
		 INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('p_new', 'msg_new', 'ses_wal', %d, %d, '{"type":"text","text":"second"}');`,
		created, created, created, created,
	)
	if _, err := fmt.Fprintf(stdin, "%s\n", insert); err != nil {
		t.Fatal(err)
	}
	// The write lands in the WAL: its size grows while the main db file stays
	// untouched.
	walGrew := false
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		walAfter, err := os.Stat(walPath)
		if err == nil && walAfter.Size() != walBefore.Size() {
			walGrew = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !walGrew {
		t.Fatalf("WAL never grew: before=%d after=%d log=%s", walBefore.Size(), walAfterSize(walPath), writerLog.String())
	}
	second, err := reader.loadOpenCodeConversation(dbPath, "ses_wal")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range second.Events {
		if event.Kind == "user_message" && strings.Contains(event.Body, "second") {
			found = true
		}
	}
	if !found {
		t.Fatalf("WAL-only write invisible to reader cache: %#v", second.Events)
	}
}

func walAfterSize(walPath string) int64 {
	if info, err := os.Stat(walPath); err == nil {
		return info.Size()
	}
	return -1
}
