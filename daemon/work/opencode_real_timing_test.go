package work

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

type realSession struct {
	ID        string
	Directory string
}

// Real-local-OpenCode verification. Gated behind ZEN_PERF_REAL_OPENCODE=1 so
// CI never touches a machine's real conversation database.
//
// Runs the exact production path (reader binding, cache, incremental polls)
// against a real local OpenCode session and prints content-free phase timings
// (counts, durations, and sizes only — never message bodies or credentials).
//
// Usage:
//
//	cd daemon && ZEN_PERF_REAL_OPENCODE=1 go test ./work/ -run TestRealOpenCodeTiming -v
func TestRealOpenCodeTiming(t *testing.T) {
	if os.Getenv("ZEN_PERF_REAL_OPENCODE") != "1" {
		t.Skip("gated behind ZEN_PERF_REAL_OPENCODE=1")
	}
	dbPath, err := openCodeDBPath()
	if err != nil || dbPath == "" {
		t.Fatal("no local OpenCode DB found")
	}
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Fatal("sqlite3 required")
	}
	// Pick the largest fresh root session content-free: sizes only, no bodies.
	query := `SELECT s.id, s.directory, (SELECT count(*) FROM message m WHERE m.session_id=s.id), (SELECT count(*) FROM part p WHERE p.session_id=s.id), (SELECT sum(length(p.data)) FROM part p WHERE p.session_id=s.id), (SELECT sum(length(m.data)) FROM message m WHERE m.session_id=s.id) FROM session s WHERE s.parent_id IS NULL AND s.time_updated >= ` + fmt.Sprintf("%d", time.Now().Add(-72*time.Hour).UnixMilli()) + ` ORDER BY 5 DESC LIMIT 3;`
	rows, err := queryOpenCodeSessionRows(sqlite3, dbPath, query)
	if err != nil {
		t.Fatal(err)
	}
	sessions := make([]realSession, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, realSession{
			ID:        row.ID,
			Directory: row.Directory,
		})
	}
	started := time.Now()
	conversation, err := parseOpenCodeConversation(dbPath, sessions[0].ID)
	coldMs := time.Since(started).Milliseconds()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("real-session cold-full-read: %d ms (events=%d)", coldMs, len(conversation.Events))

	reader := NewProviderConversationReader()
	agent := classifier.Agent{
		Cwd:     sessions[0].Directory,
		Command: "opencode",
	}
	started = time.Now()
	warm, err := reader.Load(agent, AgentProviderOpenCode, time.Now())
	warmMs := time.Since(started).Microseconds()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("real-session warm-subscription-load: %d us (events=%d version=%d)", warmMs, len(warm.Events), reader.ConversationVersion())

	started = time.Now()
	again, err := reader.Load(agent, AgentProviderOpenCode, time.Now())
	idleMs := time.Since(started).Microseconds()
	if err != nil {
		t.Fatal(err)
	}
	if reader.ConversationVersion() != 0 {
		t.Logf("real-session idle-poll: %d us (version unchanged=%v)", idleMs, true)
	}
	_ = again
}

func joinSessionIDs(sessions []realSession) string {
	parts := make([]string, 0, len(sessions))
	for _, session := range sessions {
		parts = append(parts, sqliteStringLiteral(session.ID))
	}
	return strings.Join(parts, ",")
}
