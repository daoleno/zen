package work

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

// Deterministic OpenCode conversation fixtures for performance regression.
//
// The fixture generator mirrors the row/part mix of real large local OpenCode
// sessions: user turns, assistant turns with step-start/step-finish, growing
// text parts (streaming simulation), reasoning parts, and tool parts with
// state transitions. All bodies are synthetic and content-free by design;
// timings never depend on body semantics.

type openCodePerfFixtureOptions struct {
	turns         int
	partsPerTurn  int
	textPartBytes int
	toolPartBytes int
}

func defaultOpenCodePerfFixtureOptions() openCodePerfFixtureOptions {
	return openCodePerfFixtureOptions{
		turns:         100,
		partsPerTurn:  8,
		textPartBytes: 4000,
		toolPartBytes: 12000,
	}
}

type openCodePerfFixture struct {
	dbPath          string
	sessionID       string
	directory       string
	startedAt       time.Time
	messageCount    int
	partCount       int
	partBytes       int64
	messageCursorMS int64
	partCursorMS    int64
	// nextID tracks the per-prefix monotonic counter for new rows.
	nextID map[string]int
}

func buildOpenCodePerfFixture(tb testing.TB, options openCodePerfFixtureOptions) *openCodePerfFixture {
	tb.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		tb.Skip("sqlite3 required")
	}
	options = applyOpenCodePerfFixtureDefaults(options)
	dbPath := filepath.Join(tb.TempDir(), "opencode.db")
	started := time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)
	sessionID := "ses_perf_large"
	directory := "/repo/perf"
	ms := func(t time.Time) int64 { return t.UnixMilli() }

	fixture := &openCodePerfFixture{
		dbPath:     dbPath,
		sessionID:  sessionID,
		directory:  directory,
		startedAt:  started,
		nextID:     map[string]int{"msg": 0, "prt": 0},
	}

	var b strings.Builder
	b.WriteString(openCodePerfFixtureSchema())
	fmt.Fprintf(&b,
		"INSERT INTO session(id, project_id, parent_id, slug, directory, title, version, time_created, time_updated) VALUES ('%s', 'proj', NULL, 'slug', '%s', 't', '1', %d, %d);\n",
		sessionID, directory, ms(started), ms(started),
	)

	cursor := started
	for turn := 0; turn < options.turns; turn++ {
		msgUser := fixture.nextRowID("msg")
		cursor = cursor.Add(time.Second)
		fmt.Fprintf(&b,
			"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', %d, %d, '%s');\n",
			msgUser, sessionID, ms(cursor), ms(cursor), `{"role":"user"}`,
		)
		fmt.Fprintf(&b,
			"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', '%s', %d, %d, '%s');\n",
			fixture.nextRowID("prt"), msgUser, sessionID, ms(cursor), ms(cursor),
			`{"type":"text","text":"`+strings.Repeat("u", options.textPartBytes/2)+`"}`,
		)
		fixture.messageCount++
		fixture.partCount++

		msgAsst := fixture.nextRowID("msg")
		cursor = cursor.Add(time.Second)
		finish := `"finish":"stop","time":{"created":` + fmt.Sprintf("%d", ms(cursor)) + `,"completed":` + fmt.Sprintf("%d", ms(cursor.Add(time.Second))) + `}`
		fmt.Fprintf(&b,
			"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', %d, %d, '{\"role\":\"assistant\",%s}');\n",
			msgAsst, sessionID, ms(cursor), ms(cursor.Add(time.Second)), finish,
		)
		parts := []string{
			`{"type":"step-start"}`,
			`{"type":"reasoning","text":"` + strings.Repeat("r", options.textPartBytes/4) + `"}`,
			`{"type":"text","text":"` + strings.Repeat("a", options.textPartBytes) + `"}`,
			`{"type":"tool","tool":"shell","callID":"call_` + fmt.Sprintf("%d", turn) + `","state":{"status":"completed","input":"{\"command\":\"ls\"}","output":"` + strings.Repeat("o", options.toolPartBytes) + `"}}`,
			`{"type":"step-finish","reason":"stop"}`,
		}
		for partIndex, part := range parts {
			partID := fixture.nextRowID("prt")
			fmt.Fprintf(&b,
				"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', '%s', %d, %d, '%s');\n",
				partID, msgAsst, sessionID, ms(cursor), ms(cursor), part,
			)
			fixture.partCount++
			fixture.partBytes += int64(len(part))
			_ = partIndex
		}
		fixture.messageCount++
		cursor = cursor.Add(2 * time.Second)
	}
	fixture.messageCursorMS = ms(cursor.Add(-2 * time.Second))
	fixture.partCursorMS = ms(cursor.Add(-2 * time.Second))

	if out, err := exec.Command("sqlite3", dbPath).CombinedOutput(); err == nil {
		_ = out
	}
	cmd := exec.Command("sqlite3", dbPath)
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("sqlite3 fixture: %v: %s", err, out)
	}
	return fixture
}

func applyOpenCodePerfFixtureDefaults(options openCodePerfFixtureOptions) openCodePerfFixtureOptions {
	defaults := defaultOpenCodePerfFixtureOptions()
	if options.turns == 0 {
		options.turns = defaults.turns
	}
	if options.partsPerTurn == 0 {
		options.partsPerTurn = defaults.partsPerTurn
	}
	if options.textPartBytes == 0 {
		options.textPartBytes = defaults.textPartBytes
	}
	if options.toolPartBytes == 0 {
		options.toolPartBytes = defaults.toolPartBytes
	}
	return options
}

func openCodePerfFixtureSchema() string {
	return `
CREATE TABLE project (id TEXT PRIMARY KEY);
CREATE TABLE session (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  parent_id TEXT,
  slug TEXT NOT NULL,
  directory TEXT NOT NULL,
  title TEXT NOT NULL,
  version TEXT NOT NULL,
  time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL
);
CREATE TABLE message (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL,
  data TEXT NOT NULL
);
CREATE TABLE part (
  id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL,
  session_id TEXT NOT NULL,
  time_created INTEGER NOT NULL,
  time_updated INTEGER NOT NULL,
  data TEXT NOT NULL
);
INSERT INTO project(id) VALUES ('proj');
`
}

func (f *openCodePerfFixture) nextRowID(prefix string) string {
	f.nextID[prefix]++
	return fmt.Sprintf("%s_%08d", prefix, f.nextID[prefix])
}

func (f *openCodePerfFixture) agent() classifier.Agent {
	return classifier.Agent{Cwd: f.directory, Command: "opencode", StartedAt: f.startedAt}
}

// appendStreamingRows simulates one OpenCode incremental write burst: a new
// user message with text parts, an assistant message, and one tool state
// update to an existing part (streaming text growth + tool output update).
func (f *openCodePerfFixture) appendStreamingRows(tb testing.TB, textBytes int) {
	tb.Helper()
	now := time.UnixMilli(f.messageCursorMS).Add(10 * time.Second)
	var b strings.Builder
	msgUser := f.nextRowID("msg")
	fmt.Fprintf(&b,
		"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', %d, %d, '%s');\n",
		msgUser, f.sessionID, now.UnixMilli(), now.UnixMilli(), `{"role":"user"}`,
	)
	fmt.Fprintf(&b,
		"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', '%s', %d, %d, '%s');\n",
		f.nextRowID("prt"), msgUser, f.sessionID, now.UnixMilli(), now.UnixMilli(),
		`{"type":"text","text":"`+strings.Repeat("u", textBytes/2)+`"}`,
	)
	msgAsst := f.nextRowID("msg")
	fmt.Fprintf(&b,
		"INSERT INTO message(id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', %d, %d, '%s');\n",
		msgAsst, f.sessionID, now.UnixMilli(), now.UnixMilli(), `{"role":"assistant"}`,
	)
	fmt.Fprintf(&b,
		"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', '%s', %d, %d, '%s');\n",
		f.nextRowID("prt"), msgAsst, f.sessionID, now.UnixMilli(), now.UnixMilli(), `{"type":"step-start"}`,
	)
	textPart := f.nextRowID("prt")
	fmt.Fprintf(&b,
		"INSERT INTO part(id, message_id, session_id, time_created, time_updated, data) VALUES ('%s', '%s', '%s', %d, %d, '%s');\n",
		textPart, msgAsst, f.sessionID, now.UnixMilli(), now.UnixMilli(),
		`{"type":"text","text":"`+strings.Repeat("a", textBytes)+`"}`,
	)
	f.messageCursorMS = now.UnixMilli()
	f.partCursorMS = now.UnixMilli()
	f.messageCount += 2
	f.partCount += 3
	cmd := exec.Command("sqlite3", f.dbPath)
	cmd.Stdin = strings.NewReader(b.String())
	if out, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("sqlite3 append: %v: %s", err, out)
	}
}

// updateStreamingTextPart grows one existing text part row in place, exactly
// as OpenCode does while streaming a token burst, and bumps time_updated.
func (f *openCodePerfFixture) updateStreamingTextPart(tb testing.TB, textBytes int) {
	tb.Helper()
	now := time.UnixMilli(f.partCursorMS + 5)
	uri := fmt.Sprintf("file:%s?mode=ro", f.dbPath)
	query := `SELECT id FROM part WHERE session_id = '` + f.sessionID + `' AND data LIKE '{"type":"text"%' ORDER BY time_created DESC LIMIT 1;`
	out, err := exec.Command("sqlite3", "-json", uri, query).CombinedOutput()
	if err != nil {
		tb.Fatalf("sqlite3 lookup: %v: %s", err, out)
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &rows); err != nil || len(rows) == 0 || rows[0].ID == "" {
		tb.Fatalf("sqlite3 lookup rows: %s", string(out))
	}
	id := rows[0].ID
	body := `{"type":"text","text":"` + strings.Repeat("a", textBytes) + `"}`
	update := fmt.Sprintf(
		"UPDATE part SET data = %s, time_updated = %d WHERE id = '%s';\n",
		sqliteStringLiteral(body), now.UnixMilli(), id,
	)
	cmd := exec.Command("sqlite3", f.dbPath)
	cmd.Stdin = strings.NewReader(update)
	if out, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("sqlite3 update: %v: %s", err, out)
	}
	f.partCursorMS = now.UnixMilli()
}

// BenchmarkOpenCodeLoadFullConversation measures the initial-load cost: the
// reader binding, session selection, full SQLite read, row JSON parse, and
// conversation build for a representative large history (~200 turns, ~1.2k
// parts, ~2MB of part payloads).
func BenchmarkOpenCodeLoadFullConversation(b *testing.B) {
	fixture := buildOpenCodePerfFixture(b, openCodePerfFixtureOptions{
		turns:         200,
		textPartBytes: 4000,
		toolPartBytes: 12000,
	})
	b.Setenv("ZEN_OPENCODE_DB", fixture.dbPath)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewProviderConversationReader()
		conversation, err := reader.Load(classifier.Agent{Cwd: fixture.directory, Command: "opencode", StartedAt: fixture.startedAt}, AgentProviderOpenCode, fixture.startedAt.Add(time.Hour))
		if err != nil {
			b.Fatal(err)
		}
		if !conversation.Available || len(conversation.Events) == 0 {
			b.Fatalf("conversation unavailable: %+v", conversation)
		}
	}
}

// BenchmarkOpenCodeIncrementalPoll measures the steady-state incremental
// poll: an unchanged source must be O(1) (two stats), and a WAL change with a
// handful of new/updated rows must not re-read the full history.
func BenchmarkOpenCodeIncrementalPoll(b *testing.B) {
	fixture := buildOpenCodePerfFixture(b, openCodePerfFixtureOptions{
		turns:         200,
		textPartBytes: 4000,
		toolPartBytes: 12000,
	})
	b.Setenv("ZEN_OPENCODE_DB", fixture.dbPath)
	agent := classifier.Agent{Cwd: fixture.directory, Command: "opencode", StartedAt: fixture.startedAt}
	reader := NewProviderConversationReader()
	now := fixture.startedAt.Add(time.Hour)
	if _, err := reader.Load(agent, AgentProviderOpenCode, now); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i%10 == 9 {
			fixture.updateStreamingTextPart(b, 120)
		}
		if _, err := reader.Load(agent, AgentProviderOpenCode, now.Add(time.Duration(i+1)*time.Millisecond)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOpenCodeIncrementalBurst measures one incremental write burst
// (new user + assistant message rows with parts) applied through the reader.
func BenchmarkOpenCodeIncrementalBurst(b *testing.B) {
	fixture := buildOpenCodePerfFixture(b, openCodePerfFixtureOptions{
		turns:         200,
		textPartBytes: 4000,
		toolPartBytes: 12000,
	})
	b.Setenv("ZEN_OPENCODE_DB", fixture.dbPath)
	agent := classifier.Agent{Cwd: fixture.directory, Command: "opencode", StartedAt: fixture.startedAt}
	now := fixture.startedAt.Add(time.Hour)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader := NewProviderConversationReader()
		if _, err := reader.Load(agent, AgentProviderOpenCode, now); err != nil {
			b.Fatal(err)
		}
		fixture.appendStreamingRows(b, 4000)
		if _, err := reader.Load(agent, AgentProviderOpenCode, now.Add(time.Duration(i+1)*time.Millisecond)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkOpenCodePhaseBreakdown times the individual poll phases against
// the deterministic large fixture so the regression fixture can identify the
// dominant owner without message content.
func BenchmarkOpenCodePhaseBreakdown(b *testing.B) {
	fixture := buildOpenCodePerfFixture(b, openCodePerfFixtureOptions{
		turns:         200,
		textPartBytes: 4000,
		toolPartBytes: 12000,
	})
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		b.Skip("sqlite3 required")
	}
	uri := fmt.Sprintf("file:%s?mode=ro", fixture.dbPath)
	b.Run("full-message-query", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			rows, err := queryOpenCodeMessagesSince(sqlite3, fixture.dbPath, fixture.sessionID, 0)
			if err != nil || len(rows) == 0 {
				b.Fatal(err)
			}
		}
	})
	b.Run("full-part-query", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			rows, err := queryOpenCodePartsSince(sqlite3, fixture.dbPath, fixture.sessionID, 0)
			if err != nil || len(rows) == 0 {
				b.Fatal(err)
			}
		}
	})
	b.Run("full-parse-build", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := parseOpenCodeConversation(fixture.dbPath, fixture.sessionID); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("stamp-stat", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, err := openCodeConversationStamp(fixture.dbPath); err != nil {
				b.Fatal(err)
			}
		}
	})
	_ = uri
}

// TestOpenCodePerfFixtureSize is a deterministic regression fixture sanity
// check: the fixture must represent a large history so the performance tests
// measure something real.
func TestOpenCodePerfFixtureSize(t *testing.T) {
	fixture := buildOpenCodePerfFixture(t, openCodePerfFixtureOptions{
		turns:         200,
		textPartBytes: 4000,
		toolPartBytes: 12000,
	})
	if fixture.messageCount < 300 {
		t.Fatalf("fixture too small: %d messages", fixture.messageCount)
	}
	if fixture.partCount < 700 {
		t.Fatalf("fixture too small: %d parts", fixture.partCount)
	}
	if fixture.partBytes < 2<<20 {
		t.Fatalf("fixture too small: %d part bytes", fixture.partBytes)
	}
	conversation, err := parseOpenCodeConversation(fixture.dbPath, fixture.sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(conversation.Events) < 600 {
		t.Fatalf("conversation too small: %d events", len(conversation.Events))
	}
	_ = os.Getenv("ZEN_OPENCODE_DB")
}
