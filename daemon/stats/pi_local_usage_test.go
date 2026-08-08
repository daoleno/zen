package stats

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── Fixture helpers ────────────────────────────────────────
//
// Fixture session files follow Pi's documented durable-session JSONL schema
// (~/.pi/agent/sessions/--<encoded-cwd>--/<timestamp>_<uuid>.jsonl): a
// "session" header carrying the authoritative cwd, then a tree of entries
// (message/model_change/compaction/branch_summary) linked by id/parentId.
// All content is synthetic.

func piHeader(cwd, ts string) string {
	return fmt.Sprintf(`{"type":"session","version":3,"id":"session-uuid","timestamp":%q,"cwd":%q}`, ts, cwd)
}

func piUserLine(id, ts string) string {
	return fmt.Sprintf(`{"type":"message","id":%q,"parentId":null,"timestamp":%q,"message":{"role":"user","content":"fixture"}}`, id, ts)
}

func piAssistantLine(id, ts, provider, model, usage string) string {
	return fmt.Sprintf(`{"type":"message","id":%q,"parentId":null,"timestamp":%q,"message":{"role":"assistant","content":[{"type":"text","text":"fixture"}],"api":"openai-completions","provider":%q,"model":%q,"usage":%s,"stopReason":"stop"}}`, id, ts, provider, model, usage)
}

func piToolResultLine(id, ts, usage string) string {
	return fmt.Sprintf(`{"type":"message","id":%q,"parentId":null,"timestamp":%q,"message":{"role":"toolResult","toolCallId":"call-1","toolName":"bash","content":[{"type":"text","text":"fixture"}],"isError":false,"usage":%s}}`, id, ts, usage)
}

func piModelChangeLine(id, ts, provider, modelID string) string {
	return fmt.Sprintf(`{"type":"model_change","id":%q,"parentId":null,"timestamp":%q,"provider":%q,"modelId":%q}`, id, ts, provider, modelID)
}

func piCompactionLine(id, ts, usage string) string {
	return fmt.Sprintf(`{"type":"compaction","id":%q,"parentId":null,"timestamp":%q,"summary":"fixture","firstKeptEntryId":"a1","tokensBefore":50000,"usage":%s}`, id, ts, usage)
}

func piBranchSummaryLine(id, ts, usage string) string {
	return fmt.Sprintf(`{"type":"branch_summary","id":%q,"parentId":null,"timestamp":%q,"fromId":"a1","summary":"fixture","usage":%s}`, id, ts, usage)
}

func piUsageJSON(input, output, cacheRead, cacheWrite int64, reasoning *int64, costTotal float64) string {
	total := input + output + cacheRead + cacheWrite
	reasoningJSON := ""
	if reasoning != nil {
		reasoningJSON = fmt.Sprintf(`,"reasoning":%d`, *reasoning)
	}
	return fmt.Sprintf(
		`{"input":%d,"output":%d,"cacheRead":%d,"cacheWrite":%d%s,"totalTokens":%d,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":%v}}`,
		input, output, cacheRead, cacheWrite, reasoningJSON, total, costTotal)
}

func piReasoning(v int64) *int64 { return &v }

func writePiSession(t *testing.T, home, encodedDir, fileName string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(home, ".pi", "agent", "sessions", encodedDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir pi session dir: %v", err)
	}
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write pi session: %v", err)
	}
	return path
}

func writePiOwnedSession(t *testing.T, home, fileName string, lines ...string) string {
	t.Helper()
	dir := filepath.Join(home, ".zen", "provider-sessions", "pi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir Zen-owned Pi session dir: %v", err)
	}
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write Zen-owned Pi session: %v", err)
	}
	return path
}

// piOwnedAssistantMetadataLine mirrors Pi 0.84.1's installed durable message
// shape while intentionally carrying no prompt or response content.
func piOwnedAssistantMetadataLine(id, ts, provider, model, usage string) string {
	return fmt.Sprintf(`{"type":"message","id":%q,"parentId":null,"timestamp":%q,"message":{"role":"assistant","content":[],"api":"openai-completions","provider":%q,"model":%q,"timestamp":%q,"usage":%s,"stopReason":"stop","rawStopReason":"stop","responseId":"fixture-response"}}`, id, ts, provider, model, ts, usage)
}

func piModelByName(t *testing.T, models []ModelStat, name string) ModelStat {
	t.Helper()
	for _, m := range models {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("model %q not found in %+v", name, models)
	return ModelStat{}
}

func piProjectByName(t *testing.T, projects []ProjectStat, name string) ProjectStat {
	t.Helper()
	for _, p := range projects {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("project %q not found in %+v", name, projects)
	return ProjectStat{}
}

func piClose(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// ── Acceptance: DeepSeek usage flows into StatsResponse ────

func TestPiZenOwnedDeepSeekUsageFlowsIntoEveryCurrentRange(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()
	t.Setenv("HOME", home)
	// Zen binds --session directly to this flat root. Deliberately do not
	// create ~/.pi/agent/sessions: the owned ledger must be collected even
	// when Pi's shared per-CWD history does not exist.
	// A live/partial sibling fails soft and cannot suppress the valid ledger.
	writePiOwnedSession(t, home, "00000000-0000-4000-8000-000000000002.jsonl",
		`{"type":"session","version":3`,
	)

	var resp *StatsResponse
	for attempt := 0; attempt < 2 && resp == nil; attempt++ {
		utcDate := time.Now().UTC().Format("2006-01-02")
		ts := time.Now().UTC().Format(time.RFC3339Nano)
		writePiOwnedSession(t, home, "00000000-0000-4000-8000-000000000001.jsonl",
			piHeader("/home/user/workspace/zen", ts),
			piOwnedAssistantMetadataLine("a1", ts, "opencode-go", "deepseek-v4-flash", piUsageJSON(100, 50, 200, 0, piReasoning(25), 0.0004)),
			piOwnedAssistantMetadataLine("a2", ts, "opencode-go", "deepseek-v4-flash", piUsageJSON(200, 100, 400, 0, piReasoning(50), 0.0008)),
		)

		c := NewCollector()
		c.refresh()
		c.refresh() // rereading the durable ledgers remains idempotent
		if time.Now().UTC().Format("2006-01-02") == utcDate {
			resp = c.Stats()
		}
	}
	if resp == nil {
		t.Fatal("UTC date changed during both Stats refresh attempts")
	}

	for _, rangeName := range []string{"day", "week", "all"} {
		rangeData := resp.Ranges[rangeName]
		if rangeData == nil {
			t.Fatalf("missing %q range", rangeName)
		}
		model := piModelByName(t, rangeData.Models, "deepseek-v4-flash")
		if model.TotalTokens != 1050 || model.InputTokens != 300 || model.OutputTokens != 150 ||
			model.ReasoningTokens != 75 || model.CacheRead != 600 || model.CacheCreate != 0 {
			t.Fatalf("%s DeepSeek tokens = %+v, want installed owned-session metadata totals", rangeName, model)
		}
		if !piClose(model.Cost, 0.0012) || !model.CostKnown || model.Sessions != 1 {
			t.Fatalf("%s DeepSeek cost/session = %+v, want exact 0.0012 observed cost and one session", rangeName, model)
		}
	}
}

func TestPiDeepSeekUsageFlowsIntoStatsResponse(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()
	t.Setenv("HOME", home)
	// A real Pi session ledger: provider "opencode-go" serves the recorded
	// DeepSeek model identity "deepseek-v4-flash" with reasoning and priced
	// usage. The recorded identity must survive to the App's model list
	// unchanged: no allowlist, no generic Pi label.
	writePiSession(t, home, "--home-user-workspace-zen--", "2026-04-04T10-00-00-000Z_abc.jsonl",
		piHeader("/home/user/workspace/zen", "2026-04-04T10:00:00.000Z"),
		piAssistantLine("a1", "2026-04-04T10:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(1000, 500, 200, 100, piReasoning(250), 0.004)),
		piAssistantLine("a2", "2026-04-04T11:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(2000, 1000, 400, 200, piReasoning(500), 0.008)),
		piAssistantLine("a3", "2026-04-04T12:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(3000, 1500, 600, 300, piReasoning(750), 0.012)),
	)

	c := NewCollector()
	c.refresh()
	resp := c.Stats()
	if resp == nil {
		t.Fatal("Stats() returned nil")
	}

	all := resp.Ranges["all"]
	if all == nil {
		t.Fatal("missing 'all' range")
	}

	model := piModelByName(t, all.Models, "deepseek-v4-flash")
	if model.TotalTokens != 10800 || model.InputTokens != 6000 || model.OutputTokens != 3000 ||
		model.ReasoningTokens != 1500 || model.CacheRead != 1200 || model.CacheCreate != 600 {
		t.Fatalf("deepseek token buckets = %+v, want total 10800 input 6000 output 3000 reasoning 1500 cacheRead 1200 cacheCreate 600", model)
	}
	if !piClose(model.Cost, 0.024) || !model.CostKnown {
		t.Fatalf("deepseek cost = %v known=%v, want exact observed 0.024 known", model.Cost, model.CostKnown)
	}
	if !model.TotalTokensKnown || !model.TokenBreakdownKnown {
		t.Fatalf("deepseek availability = %+v, want fully known breakdown", model)
	}
	// One session per (file, model, date): three messages are not three sessions.
	if model.Sessions != 1 {
		t.Fatalf("deepseek sessions = %d, want 1 per session file per date", model.Sessions)
	}

	project := piProjectByName(t, all.Projects, "zen")
	if project.TotalTokens != 10800 || project.InputTokens != 6000 || project.OutputTokens != 3000 ||
		project.ReasoningTokens != 1500 || project.CacheRead != 1200 || project.CacheCreate != 600 {
		t.Fatalf("zen project buckets = %+v", project)
	}
	if !piClose(project.Cost, 0.024) || !project.CostKnown || project.Sessions != 1 {
		t.Fatalf("zen project cost = %v known=%v sessions=%d, want 0.024 known 1", project.Cost, project.CostKnown, project.Sessions)
	}

	if !piClose(all.Cost, 0.024) || !all.CostKnown || all.TotalTokens != 10800 || all.Sessions != 1 {
		t.Fatalf("all-range totals = cost %v known %v tokens %d sessions %d", all.Cost, all.CostKnown, all.TotalTokens, all.Sessions)
	}

	if len(all.Days) != 1 || all.Days[0].Date != "2026-04-04" {
		t.Fatalf("day cells = %+v, want one cell for 2026-04-04", all.Days)
	}
	day := all.Days[0]
	if day.TotalTokens != 10800 || !piClose(day.Cost, 0.024) || !day.CostKnown || day.Sessions != 1 {
		t.Fatalf("day cell = %+v, want 10800 tokens, 0.024 known cost, 1 session", day)
	}
}

// ── Whole-ledger counting: branches, retries, duplicates ───

func TestPiWholeLedgerCountsBranchesRetriesAndDuplicatesOnce(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()
	usageA1 := piUsageJSON(100, 50, 0, 0, nil, 0.0004)
	usageA2 := piUsageJSON(200, 100, 0, 0, nil, 0.0008)
	writePiSession(t, home, "--home-user-workspace-zen--", "2026-04-05T10-00-00-000Z_def.jsonl",
		piHeader("/home/user/workspace/zen", "2026-04-05T10:00:00.000Z"),
		piUserLine("u1", "2026-04-05T10:00:01.000Z"),
		// Branch A: the original answer (billed).
		piAssistantLine("a1", "2026-04-05T10:00:02.000Z", "opencode-go", "deepseek-v4-flash", usageA1),
		// Retry: a new billed assistant record on branch B.
		piUserLine("u2", "2026-04-05T10:01:00.000Z"),
		piAssistantLine("a2", "2026-04-05T10:01:01.000Z", "opencode-go", "deepseek-v4-flash", usageA2),
		// Duplicate snapshot of a1 (interrupted append retried): same entry id,
		// must not double-count.
		piAssistantLine("a1", "2026-04-05T10:00:02.000Z", "opencode-go", "deepseek-v4-flash", usageA1),
	)

	byDate := (&Collector{}).collectPiStats(home)
	day := byDate["2026-04-05"]
	if day == nil {
		t.Fatal("missing pi day")
	}

	model := day.models["deepseek-v4-flash"]
	// Both billed records count (450), the duplicate snapshot does not (would be 600).
	if model.totalTokens != 450 || model.inputTokens != 300 || model.outputTokens != 150 {
		t.Fatalf("deepseek ledger totals = %+v, want 450 total (300 input, 150 output)", model)
	}
	if !piClose(model.recorded.cost, 0.0012) || model.costUnknown {
		t.Fatalf("deepseek recorded cost = %v unknown=%v, want 0.0012 observed", model.recorded.cost, model.costUnknown)
	}
	if model.sessions != 1 {
		t.Fatalf("deepseek sessions = %d, want 1", model.sessions)
	}

	stats := buildModelStats(aggregateModelsByDate(byDate, "0000-00-00", "9999-99-99"))
	got := piModelByName(t, stats, "deepseek-v4-flash")
	if got.TotalTokens != 450 || !piClose(got.Cost, 0.0012) || !got.CostKnown {
		t.Fatalf("deepseek stats = %+v, want 450 tokens, 0.0012 known cost", got)
	}
}

// ── Compaction and branch_summary usage attribution ────────

func TestPiCompactionAndBranchSummaryUsageFollowsModelInEffect(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()
	writePiSession(t, home, "--home-user-workspace-zen--", "2026-04-06T10-00-00-000Z_ghi.jsonl",
		piHeader("/home/user/workspace/zen", "2026-04-06T10:00:00.000Z"),
		piAssistantLine("a1", "2026-04-06T10:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(100, 50, 0, 0, nil, 0.0004)),
		// Compaction summary generation is billed usage without a recorded
		// model; it is attributed to the model in effect at that ledger
		// position (deepseek-v4-flash).
		piCompactionLine("c1", "2026-04-06T10:05:00.000Z", piUsageJSON(30, 20, 0, 0, nil, 0.0001)),
		// Model switch mid-session.
		piModelChangeLine("mc1", "2026-04-06T10:06:00.000Z", "openai", "gpt-5.1-codex"),
		piBranchSummaryLine("b1", "2026-04-06T10:07:00.000Z", piUsageJSON(10, 5, 0, 0, nil, 0.00005)),
		piAssistantLine("a2", "2026-04-06T10:08:00.000Z", "openai", "gpt-5.1-codex", piUsageJSON(200, 100, 0, 0, nil, 0.0006)),
	)

	byDate := (&Collector{}).collectPiStats(home)
	day := byDate["2026-04-06"]
	if day == nil {
		t.Fatal("missing pi day")
	}

	deepseek := day.models["deepseek-v4-flash"]
	if deepseek.totalTokens != 200 || !piClose(deepseek.recorded.cost, 0.0005) {
		t.Fatalf("deepseek (assistant + compaction) = %+v, want 200 tokens and 0.0005 cost", deepseek)
	}
	gpt := day.models["gpt-5.1-codex"]
	if gpt.totalTokens != 315 || !piClose(gpt.recorded.cost, 0.00065) {
		t.Fatalf("gpt (branch summary + assistant) = %+v, want 315 tokens and 0.00065 cost", gpt)
	}
	if deepseek.sessions != 1 || gpt.sessions != 1 {
		t.Fatalf("model sessions = deepseek %d gpt %d, want 1 each", deepseek.sessions, gpt.sessions)
	}
	if day.projects["zen"].sessions != 1 {
		t.Fatalf("project sessions = %d, want 1", day.projects["zen"].sessions)
	}

	stats := buildModelStats(aggregateModelsByDate(byDate, "0000-00-00", "9999-99-99"))
	if len(stats) != 2 {
		t.Fatalf("model stats = %+v, want deepseek and gpt rows", stats)
	}
	if got := piModelByName(t, stats, displayName("gpt-5.1-codex")); !piClose(got.Cost, 0.00065) || !got.CostKnown {
		t.Fatalf("gpt stats = %+v, want 0.00065 known cost", got)
	}
	cells := buildDayCells(byDate, nil, "2026-04-06", "2026-04-06")
	if len(cells) != 1 || !piClose(cells[0].Cost, 0.00115) || !cells[0].CostKnown || cells[0].Sessions != 2 {
		t.Fatalf("day cell = %+v, want 0.00115 known cost and 2 model sessions (one per model)", cells)
	}
}

// ── Malformed, partial and live files fail soft ─────────────

func TestPiMalformedAndPartialFilesFailSoft(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()

	// File A: valid records plus a truncated final line (live append in
	// progress) and a malformed middle line; both must be skipped.
	writePiSession(t, home, "--tmp-zen--", "2026-04-07T10-00-00-000Z_jkl.jsonl",
		piHeader("/tmp/zen", "2026-04-07T10:00:00.000Z"),
		"this is not json at all",
		piAssistantLine("a1", "2026-04-07T10:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(10, 5, 0, 0, nil, 0.0001)),
		`{"type":"message","id":"partial","parentId":null,"timestamp":"2026-04-07T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"fixture"`)

	// File B: no header (cwd fallback decodes the session dir name), plus an
	// assistant that records no model (skipped) and a zero-token aborted
	// assistant (skipped).
	writePiSession(t, home, "--tmp-zen2--", "2026-04-07T11-00-00-000Z_mno.jsonl",
		piAssistantLine("a1", "2026-04-07T11:00:01.000Z", "openrouter", "x-ai/grok-4.1-fast", piUsageJSON(20, 10, 0, 0, nil, 0.0002)),
		`{"type":"message","id":"a2","parentId":null,"timestamp":"2026-04-07T11:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"fixture"}],"api":"openai-completions","provider":"opencode-go","model":"","usage":{"input":1,"output":1,"cacheRead":0,"cacheWrite":0,"totalTokens":2,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"stop"}}`,
		`{"type":"message","id":"a3","parentId":null,"timestamp":"2026-04-07T11:00:03.000Z","message":{"role":"assistant","content":[{"type":"text","text":"fixture"}],"api":"openai-completions","provider":"opencode-go","model":"deepseek-v4-flash","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"totalTokens":0,"cost":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"total":0}},"stopReason":"aborted"}}`)

	// File C: toolResult usage with no model ever recorded in the ledger:
	// unattributable, so it contributes nothing (and does not invent a model).
	writePiSession(t, home, "--tmp-zen3--", "2026-04-07T12-00-00-000Z_pqr.jsonl",
		piToolResultLine("t1", "2026-04-07T12:00:01.000Z", piUsageJSON(50, 25, 0, 0, nil, 0)))

	// Non-session files and nested dirs are ignored.
	writePiSession(t, home, "--tmp-zen--", "notes.txt", "not a session")

	byDate := (&Collector{}).collectPiStats(home)

	zen := byDate["2026-04-07"]
	if zen == nil {
		t.Fatal("missing pi day from partially malformed files")
	}
	deepseek := zen.models["deepseek-v4-flash"]
	if deepseek.totalTokens != 15 || deepseek.inputTokens != 10 {
		t.Fatalf("file A deepseek = %+v, want only the 15-token valid record (10 input + 5 output)", deepseek)
	}
	if zen.projects["zen"] == nil || zen.projects["zen"].sessions != 1 {
		t.Fatalf("file A project = %+v, want header-cwd project zen with 1 session", zen.projects["zen"])
	}
	grok := zen.models["x-ai/grok-4.1-fast"]
	if grok.totalTokens != 30 || grok.sessions != 1 {
		t.Fatalf("file B grok = %+v, want 30 tokens and 1 session", grok)
	}
	if zen.projects["zen2"] == nil || zen.projects["zen2"].sessions != 1 {
		t.Fatalf("file B project = %+v, want decoded-dir project zen2 with 1 session", zen.projects["zen2"])
	}
	// Skipped records: no-model assistant, zero-token aborted assistant,
	// unattributable toolResult usage, notes.txt.
	if _, ok := zen.models[""]; ok {
		t.Fatalf("empty model id must not be aggregated: %+v", zen.models)
	}
	if len(byDate) != 1 {
		t.Fatalf("unexpected date buckets: %v", byDate)
	}
}

// ── Cost semantics: observed exact vs unknown ───────────────

func TestPiCostUnknownWhenNotPriced(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()

	// Model with a mix of observed and unobserved cost: the observed part is
	// exact, the model overall stays cost-unknown.
	writePiSession(t, home, "--tmp-zen-mixed--", "2026-04-08T10-00-00-000Z_aaa.jsonl",
		piHeader("/tmp/zen-mixed", "2026-04-08T10:00:00.000Z"),
		piAssistantLine("m1", "2026-04-08T10:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(1000, 500, 0, 0, nil, 0.004)),
		piAssistantLine("m2", "2026-04-08T10:00:02.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(2000, 1000, 0, 0, nil, 0)),
	)
	// Model never priced: all cost fields are zero.
	writePiSession(t, home, "--tmp-zen-free--", "2026-04-08T11-00-00-000Z_bbb.jsonl",
		piHeader("/tmp/zen-free", "2026-04-08T11:00:00.000Z"),
		piAssistantLine("m1", "2026-04-08T11:00:01.000Z", "openai", "gpt-5.1-codex", piUsageJSON(100, 50, 0, 0, nil, 0)),
	)
	// Model fully priced.
	writePiSession(t, home, "--tmp-zen-paid--", "2026-04-08T12-00-00-000Z_ccc.jsonl",
		piHeader("/tmp/zen-paid", "2026-04-08T12:00:00.000Z"),
		piAssistantLine("m1", "2026-04-08T12:00:01.000Z", "anthropic", "claude-opus-4-8", piUsageJSON(1000, 500, 0, 0, nil, 0.005)),
	)

	stats := buildModelStats(aggregateModelsByDate((&Collector{}).collectPiStats(home), "0000-00-00", "9999-99-99"))

	mixed := piModelByName(t, stats, "deepseek-v4-flash")
	if mixed.TotalTokens != 4500 || !piClose(mixed.Cost, 0.004) || mixed.CostKnown {
		t.Fatalf("mixed-cost model = %+v, want 4500 tokens, 0.004 observed cost, unknown overall", mixed)
	}
	free := piModelByName(t, stats, displayName("gpt-5.1-codex"))
	if free.TotalTokens != 150 || free.Cost != 0 || free.CostKnown {
		t.Fatalf("unpriced model = %+v, want 150 tokens, zero cost, unknown", free)
	}
	paid := piModelByName(t, stats, displayName("claude-opus-4-8"))
	if paid.TotalTokens != 1500 || !piClose(paid.Cost, 0.005) || !paid.CostKnown {
		t.Fatalf("priced model = %+v, want 1500 tokens, 0.005 known cost", paid)
	}
}

// ── Multiple sessions, days, projects; idempotent refresh ──

func TestPiMultipleSessionsDaysProjectsAndIdempotentRefresh(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()
	t.Setenv("HOME", home)
	file1 := writePiSession(t, home, "--home-user-zen--", "2026-04-04T10-00-00-000Z_ddd.jsonl",
		piHeader("/home/user/zen", "2026-04-04T10:00:00.000Z"),
		piAssistantLine("a1", "2026-04-04T10:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(100, 0, 0, 0, nil, 0)),
		piAssistantLine("a2", "2026-04-04T11:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(200, 0, 0, 0, nil, 0)),
		piAssistantLine("a3", "2026-04-05T10:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(300, 0, 0, 0, nil, 0)),
	)
	writePiSession(t, home, "--home-user-onlora--", "2026-04-04T12-00-00-000Z_eee.jsonl",
		piHeader("/home/user/onlora", "2026-04-04T12:00:00.000Z"),
		piAssistantLine("b1", "2026-04-04T12:00:01.000Z", "openai", "gpt-5.1-codex", piUsageJSON(50, 0, 0, 0, nil, 0)),
		piAssistantLine("b2", "2026-04-05T12:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(400, 0, 0, 0, nil, 0)),
	)

	c := NewCollector()
	c.refresh()
	first := c.Stats()
	if first == nil {
		t.Fatal("Stats() returned nil")
	}

	// Repeated refresh is idempotent: no double-counting of rereads.
	c.refresh()
	second := c.Stats()
	all := second.Ranges["all"]
	deepseek := piModelByName(t, all.Models, "deepseek-v4-flash")
	if deepseek.TotalTokens != 1000 || deepseek.Sessions != 3 {
		t.Fatalf("deepseek after refresh = %+v, want 1000 tokens and 3 (file,date) sessions", deepseek)
	}
	if got := piModelByName(t, all.Models, displayName("gpt-5.1-codex")); got.TotalTokens != 50 || got.Sessions != 1 {
		t.Fatalf("gpt after refresh = %+v, want 50 tokens and 1 session", got)
	}
	zen := piProjectByName(t, all.Projects, "zen")
	if zen.TotalTokens != 600 || zen.Sessions != 2 {
		t.Fatalf("zen project = %+v, want 600 tokens and 2 session-days", zen)
	}
	if got := piProjectByName(t, all.Projects, "onlora"); got.TotalTokens != 450 || got.Sessions != 2 {
		t.Fatalf("onlora project = %+v, want 450 tokens and 2 session-days (one per date the ledger touches)", got)
	}
	if len(all.Days) != 2 {
		t.Fatalf("day cells = %+v, want 2", all.Days)
	}
	for _, day := range all.Days {
		if day.Date == "2026-04-04" && (day.TotalTokens != 350 || day.Sessions != 2) {
			t.Fatalf("day 1 cell = %+v, want 350 tokens and 2 sessions", day)
		}
		if day.Date == "2026-04-05" && (day.TotalTokens != 700 || day.Sessions != 2) {
			t.Fatalf("day 2 cell = %+v, want 700 tokens and 2 sessions", day)
		}
	}

	// Simulate a live append after the second refresh: the new record joins
	// exactly once and old records are not re-counted.
	f, err := os.OpenFile(file1, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open session for append: %v", err)
	}
	if _, err := f.WriteString(piAssistantLine("a4", "2026-04-05T11:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(600, 0, 0, 0, nil, 0)) + "\n"); err != nil {
		t.Fatalf("append session line: %v", err)
	}
	f.Close()

	c.refresh()
	third := c.Stats()
	all = third.Ranges["all"]
	deepseek = piModelByName(t, all.Models, "deepseek-v4-flash")
	if deepseek.TotalTokens != 1600 || deepseek.Sessions != 3 {
		t.Fatalf("deepseek after append = %+v, want exactly 1600 tokens (old 1000 + 600) and 3 sessions", deepseek)
	}
	if got := piModelByName(t, all.Models, displayName("gpt-5.1-codex")); got.TotalTokens != 50 {
		t.Fatalf("gpt after append = %+v, want unchanged 50", got)
	}
	if got := piProjectByName(t, all.Projects, "zen"); got.TotalTokens != 1200 {
		t.Fatalf("zen after append = %+v, want 1200", got)
	}
}

// ── Mixed-source same-model cost semantics ──────────────────

func TestPiSameModelMixedSourcesMergeCost(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()
	// Pi records exact observed cost for deepseek-v4-flash.
	writePiSession(t, home, "--tmp-zen-pi--", "2026-04-09T10-00-00-000Z_fff.jsonl",
		piHeader("/tmp/zen-pi", "2026-04-09T10:00:00.000Z"),
		piAssistantLine("p1", "2026-04-09T10:00:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(1000, 500, 0, 0, nil, 0.005)),
	)

	piByDate := (&Collector{}).collectPiStats(home)

	// Another source (OpenCode shape) uses the same model without cost.
	opencodeByDate := map[string]*dateAgg{
		"2026-04-09": {
			models: map[string]modelAggEntry{
				"deepseek-v4-flash": {
					totalTokens:  3000,
					inputTokens:  2000,
					outputTokens: 1000,
					costUnknown:  true,
					sessions:     1,
				},
			},
		},
	}

	merged := aggregateModelsByDate(piByDate, "0000-00-00", "9999-99-99")
	mergeModelAgg(merged, aggregateModelsByDate(opencodeByDate, "0000-00-00", "9999-99-99"))
	stats := buildModelStats(merged)

	if len(stats) != 1 {
		t.Fatalf("model stats = %+v, want a single deepseek row", stats)
	}
	model := stats[0]
	if model.TotalTokens != 4500 {
		t.Fatalf("merged total = %d, want 4500", model.TotalTokens)
	}
	// The exact observed Pi cost is preserved; the unrecorded source keeps
	// the model's overall cost unknown (no DeepSeek price is fabricated).
	if !piClose(model.Cost, 0.005) || model.CostKnown {
		t.Fatalf("merged cost = %v known=%v, want 0.005 observed and unknown overall", model.Cost, model.CostKnown)
	}
}

// ── Local-timezone date/slot bucketing ─────────────────────

func TestPiUsesLocalTimezoneBuckets(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	setTestLocalLocation(t, shanghai)

	home := t.TempDir()
	// 2026-04-05T17:30Z is 2026-04-06 01:30 in Shanghai (night slot 0).
	writePiSession(t, home, "--tmp-zen--", "2026-04-05T17-30-00-000Z_ggg.jsonl",
		piHeader("/tmp/zen", "2026-04-05T17:30:00.000Z"),
		piAssistantLine("a1", "2026-04-05T17:30:01.000Z", "opencode-go", "deepseek-v4-flash", piUsageJSON(40, 20, 0, 0, nil, 0)),
	)

	byDate := (&Collector{}).collectPiStats(home)
	if _, ok := byDate["2026-04-05"]; ok {
		t.Fatal("unexpected UTC date bucket")
	}
	day := byDate["2026-04-06"]
	if day == nil {
		t.Fatal("missing local-date bucket")
	}
	if day.models["deepseek-v4-flash"].totalTokens != 60 || day.models["deepseek-v4-flash"].sessions != 1 {
		t.Fatalf("model = %+v, want 60 tokens and 1 session on the local date", day.models["deepseek-v4-flash"])
	}
	if day.slots[0].totalTokens != 60 || day.slots[0].sessions != 1 {
		t.Fatalf("night slot = %+v, want 60 tokens and 1 session", day.slots[0])
	}
	if day.projects["zen"].sessions != 1 {
		t.Fatalf("project sessions = %d, want 1", day.projects["zen"].sessions)
	}
}
