package work

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCodeCompletedWithoutFinishSettles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 10, 0, time.UTC)
	createOpenCodeFixtureDB(t, dbPath, []openCodeSessionSeed{
		{ID: "ses_c", Directory: "/repo", CreatedMS: started.UnixMilli(), UpdatedMS: started.Add(5 * time.Second).UnixMilli()},
	}, []openCodeMessageSeed{
		{ID: "msg_user", SessionID: "ses_c", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"role":"user"}`},
		{ID: "msg_asst", SessionID: "ses_c", CreatedMS: started.Add(2 * time.Second).UnixMilli(), Data: fmt.Sprintf(`{"role":"assistant","time":{"created":1,"completed":%d}}`, started.Add(4*time.Second).UnixMilli())},
	}, []openCodePartSeed{
		{ID: "p1", MessageID: "msg_user", SessionID: "ses_c", CreatedMS: started.Add(time.Second).UnixMilli(), Data: `{"type":"text","text":"hi"}`},
	})
	got, err := parseOpenCodeConversation(dbPath, "ses_c")
	if err != nil {
		t.Fatal(err)
	}
	if got.Activity == nil || got.Activity.Status != ProviderActivityCompleted {
		t.Fatalf("completed-without-finish must settle: %+v", got.Activity)
	}
}
