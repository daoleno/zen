package work

import (
	"fmt"
	"path/filepath"
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
