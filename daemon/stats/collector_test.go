package stats

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setTestLocalLocation(t *testing.T, loc *time.Location) {
	t.Helper()

	prev := time.Local
	time.Local = loc
	t.Cleanup(func() {
		time.Local = prev
	})
}

func TestCollectorSmoke(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := NewCollector()
	c.refresh()

	resp := c.Stats()
	if resp == nil {
		t.Fatal("Stats() returned nil")
	}

	if len(resp.Ranges) != 4 {
		t.Fatalf("expected 4 ranges, got %d", len(resp.Ranges))
	}

	all := resp.Ranges["all"]
	if all == nil {
		t.Fatal("missing 'all' range")
	}
	if all.Cost != 0 || all.Sessions != 0 || len(all.Models) != 0 || len(all.Projects) != 0 || len(all.Skills) != 0 || len(all.Tools) != 0 || len(all.Days) != 0 {
		t.Fatalf("empty-home stats contain host data: %#v", all)
	}
}

func TestCollectCodexStatsSkipsUnparsableRollouts(t *testing.T) {
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 not found")
	}

	home := t.TempDir()
	codexDir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatalf("mkdir codex dir: %v", err)
	}

	dbPath := filepath.Join(codexDir, "state_5.sqlite")
	sql := `
CREATE TABLE threads (
	id TEXT,
	cwd TEXT,
	model TEXT,
	tokens_used INTEGER,
	created_at INTEGER,
	updated_at INTEGER,
	rollout_path TEXT
);
INSERT INTO threads (id, cwd, model, tokens_used, created_at, updated_at, rollout_path)
VALUES ('bad-thread', '/tmp/onlora', 'gpt-5.5', 362727822, 1710000000, 1710003600, '/tmp/missing-rollout.jsonl');
`
	if out, err := exec.Command(sqlite3, dbPath, sql).CombinedOutput(); err != nil {
		t.Fatalf("create sqlite fixture: %v\n%s", err, out)
	}

	c := &Collector{}
	daily, modelsByDate, projectsByDate := c.collectCodexStats(home)
	if len(daily) != 0 || len(modelsByDate) != 0 || len(projectsByDate) != 0 {
		t.Fatalf("unparsable rollout should not create synthetic usage: daily=%v models=%v projects=%v", daily, modelsByDate, projectsByDate)
	}
}

func TestRangeAggregatesStayScoped(t *testing.T) {
	claudeByDate := map[string]*dateAgg{
		"2026-04-04": {
			models: map[string]modelAggEntry{
				"claude-sonnet-4-6": {inputTokens: 1000, outputTokens: 500, sessions: 1},
			},
			tools: map[string]int{"Read": 2},
			skills: map[string]*skillEntry{
				"review": {calls: 1, projects: map[string]bool{"zen": true}},
			},
			projects: map[string]*projectAggEntry{
				"zen": {inputTokens: 1000, outputTokens: 500, sessions: 1},
			},
		},
		"2026-03-01": {
			models: map[string]modelAggEntry{
				"claude-opus-4-6": {inputTokens: 3000, outputTokens: 1200, sessions: 2},
			},
			tools: map[string]int{"Bash": 4},
			skills: map[string]*skillEntry{
				"ship": {calls: 3, projects: map[string]bool{"older": true}},
			},
			projects: map[string]*projectAggEntry{
				"older": {inputTokens: 3000, outputTokens: 1200, sessions: 2},
			},
		},
	}
	codexModelsByDate := map[string]map[string]modelAggEntry{
		"2026-04-04": {
			"codex-mini": {inputTokens: 700, sessions: 1},
		},
		"2026-03-01": {
			"codex-max": {inputTokens: 5000, sessions: 4},
		},
	}
	codexProjectsByDate := map[string]map[string]*projectAggEntry{
		"2026-04-04": {
			"zen": {inputTokens: 700, sessions: 1},
		},
		"2026-03-01": {
			"older": {inputTokens: 5000, sessions: 4},
		},
	}

	dayModelAgg := aggregateModelsByDate(claudeByDate, "2026-04-04", "9999-99-99")
	mergeModelAgg(dayModelAgg, aggregateCodexModelsByDate(codexModelsByDate, "2026-04-04", "9999-99-99"))
	dayModels := buildModelStats(dayModelAgg)
	dayProjects := buildProjectStats(
		aggregateProjectsByDate(claudeByDate, "2026-04-04", "9999-99-99"),
		aggregateCodexProjectsByDate(codexProjectsByDate, "2026-04-04", "9999-99-99"),
	)
	dayTools := buildToolStats(aggregateToolsByDate(claudeByDate, "2026-04-04", "9999-99-99"))
	daySkills := buildSkillStats(aggregateSkillsByDate(claudeByDate, "2026-04-04", "9999-99-99"))

	allModels := aggregateModelsByDate(claudeByDate, "0000-00-00", "9999-99-99")
	mergeModelAgg(allModels, aggregateCodexModelsByDate(codexModelsByDate, "0000-00-00", "9999-99-99"))
	allProjects := buildProjectStats(
		aggregateProjectsByDate(claudeByDate, "0000-00-00", "9999-99-99"),
		aggregateCodexProjectsByDate(codexProjectsByDate, "0000-00-00", "9999-99-99"),
	)

	if len(dayModels) != 2 {
		t.Fatalf("day models should only include the selected date, got %+v", dayModels)
	}
	if len(dayTools) != 1 || dayTools[0].Name != "Read" || dayTools[0].Calls != 2 {
		t.Fatalf("day tools should only include scoped tool calls, got %+v", dayTools)
	}
	if len(daySkills) != 1 || daySkills[0].Name != "review" || daySkills[0].Calls != 1 {
		t.Fatalf("day skills should only include scoped skill calls, got %+v", daySkills)
	}
	if len(dayProjects) != 1 || dayProjects[0].Name != "zen" || dayProjects[0].Sessions != 2 {
		t.Fatalf("day projects should merge only same-day project sessions, got %+v", dayProjects)
	}
	if len(allModels) != 4 {
		t.Fatalf("all models should include both Claude and Codex models, got %+v", allModels)
	}
	if len(allProjects) != 2 {
		t.Fatalf("all projects should include both date buckets, got %+v", allProjects)
	}
}

func TestReadCodexUsageFromRollout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := `{"timestamp":"2026-04-04T09:15:41.759Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":107939,"cached_input_tokens":72960,"output_tokens":1313,"reasoning_output_tokens":211,"total_tokens":109252}}}}
{"timestamp":"2026-04-04T09:15:50.723Z","type":"event_msg","payload":{"type":"token_count","info":null}}
{"timestamp":"2026-04-04T09:16:10.723Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":148323,"cached_input_tokens":111232,"output_tokens":1528,"reasoning_output_tokens":243,"total_tokens":149851}}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	usage, err := readCodexUsage(path)
	if err != nil {
		t.Fatalf("readCodexUsage: %v", err)
	}

	if usage.totalTokens != 149851 {
		t.Fatalf("expected total 149851, got %d", usage.totalTokens)
	}
	if usage.inputTokens != 37091 {
		t.Fatalf("expected uncached input 37091, got %d", usage.inputTokens)
	}
	if usage.cacheRead != 111232 {
		t.Fatalf("expected cached input 111232, got %d", usage.cacheRead)
	}
	if usage.outputTokens != 1528 {
		t.Fatalf("expected output 1528, got %d", usage.outputTokens)
	}
	if usage.reasoningTokens != 243 {
		t.Fatalf("expected reasoning 243, got %d", usage.reasoningTokens)
	}
}

func TestReadCodexUsageByDateSplitsAcrossLocalDays(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := `{"timestamp":"2026-04-05T15:59:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110}}}}
{"timestamp":"2026-04-05T15:59:10.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110}}}}
{"timestamp":"2026-04-05T16:01:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"cached_input_tokens":40,"output_tokens":18,"reasoning_output_tokens":4,"total_tokens":178}}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	byDate, err := readCodexUsageByDate(path, shanghai)
	if err != nil {
		t.Fatalf("readCodexUsageByDate: %v", err)
	}

	day1 := byDate["2026-04-05"]
	if day1.totalTokens != 110 || day1.inputTokens != 80 || day1.outputTokens != 10 || day1.reasoningTokens != 2 || day1.cacheRead != 20 {
		t.Fatalf("unexpected day1 usage: %+v", day1)
	}

	day2 := byDate["2026-04-06"]
	if day2.totalTokens != 68 || day2.inputTokens != 40 || day2.outputTokens != 8 || day2.reasoningTokens != 2 || day2.cacheRead != 20 {
		t.Fatalf("unexpected day2 usage: %+v", day2)
	}
}

func TestCodexRolloutCacheIsolatedAndInvalidatedAfterAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	firstLine := `{"timestamp":"2026-04-05T15:59:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110}}}}` + "\n"
	if err := os.WriteFile(path, []byte(firstLine), 0o600); err != nil {
		t.Fatal(err)
	}
	collector := NewCollector()
	first, err := collector.readCodexUsageByDate(path, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(collector.codexRolloutCache) != 1 || first["2026-04-05"].totalTokens != 110 {
		t.Fatalf("first cached usage = %#v cache=%#v", first, collector.codexRolloutCache)
	}
	first["2026-04-05"] = codexUsage{totalTokens: 999}
	second, err := collector.readCodexUsageByDate(path, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if second["2026-04-05"].totalTokens != 110 {
		t.Fatal("caller mutation aliased the Codex rollout cache")
	}

	secondLine := `{"timestamp":"2026-04-05T16:01:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"cached_input_tokens":40,"output_tokens":18,"reasoning_output_tokens":4,"total_tokens":178}}}}` + "\n"
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(secondLine); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := collector.readCodexUsageByDate(path, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if third["2026-04-05"].totalTokens != 178 {
		t.Fatalf("appended rollout did not invalidate cache: %#v", third)
	}
}

func TestReadCodexUsageHandlesLargeRolloutLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	content := `{"timestamp":"2026-05-08T01:00:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":10,"reasoning_output_tokens":2,"total_tokens":110}}}}
{"type":"event_msg","payload":{"type":"large_context","data":"` + strings.Repeat("x", 2*1024*1024) + `"}}
{"timestamp":"2026-05-08T01:01:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":160,"cached_input_tokens":40,"output_tokens":18,"reasoning_output_tokens":4,"total_tokens":178}}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write rollout: %v", err)
	}

	usage, err := readCodexUsage(path)
	if err != nil {
		t.Fatalf("readCodexUsage: %v", err)
	}

	if usage.totalTokens != 178 || usage.inputTokens != 120 || usage.cacheRead != 40 || usage.outputTokens != 18 || usage.reasoningTokens != 4 {
		t.Fatalf("unexpected usage: %+v", usage)
	}
}

func TestTimestampBucketingUsesLocalTimezone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	date, hour, ok := localDateHourFromTimestamp("2026-04-05T17:30:00.000Z", shanghai)
	if !ok {
		t.Fatal("expected timestamp to parse")
	}
	if date != "2026-04-06" {
		t.Fatalf("date = %s, want 2026-04-06", date)
	}
	if hour != 1 {
		t.Fatalf("hour = %d, want 1", hour)
	}
}

func TestCodexUnixTimestampUsesLocalTimezone(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	createdAt := time.Date(2026, time.April, 5, 17, 30, 0, 0, time.UTC).Unix()
	if got := localDateFromUnixTimestamp(createdAt, shanghai); got != "2026-04-06" {
		t.Fatalf("date = %s, want 2026-04-06", got)
	}
}

func TestScanSessionJSONLCrossDayBucketsSessionsPerDay(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"assistant","timestamp":"2026-04-04T23:59:00.000Z","cwd":"/tmp/zen","message":{"id":"m1","model":"claude-sonnet-4-6","content":[{"type":"text","text":"a"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}}
{"type":"assistant","timestamp":"2026-04-05T00:01:00.000Z","cwd":"/tmp/zen","message":{"id":"m2","model":"claude-sonnet-4-6","content":[{"type":"text","text":"b"}],"usage":{"input_tokens":20,"output_tokens":7,"cache_read_input_tokens":3,"cache_creation_input_tokens":0}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	c := NewCollector()
	byDate := make(map[string]*dateAgg)
	c.scanSessionJSONL(path, "zen", byDate)

	day1 := byDate["2026-04-04"]
	if day1 == nil {
		t.Fatal("missing day 1 bucket")
	}
	day2 := byDate["2026-04-05"]
	if day2 == nil {
		t.Fatal("missing day 2 bucket")
	}

	if got := day1.models["claude-sonnet-4-6"].sessions; got != 1 {
		t.Fatalf("day1 model sessions = %d, want 1", got)
	}
	if got := day2.models["claude-sonnet-4-6"].sessions; got != 1 {
		t.Fatalf("day2 model sessions = %d, want 1", got)
	}
	if got := day1.projects["zen"].sessions; got != 1 {
		t.Fatalf("day1 project sessions = %d, want 1", got)
	}
	if got := day2.projects["zen"].sessions; got != 1 {
		t.Fatalf("day2 project sessions = %d, want 1", got)
	}
}

func TestScanSessionJSONLUsesLocalDateForShanghai(t *testing.T) {
	shanghai, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	setTestLocalLocation(t, shanghai)

	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	content := `{"type":"assistant","timestamp":"2026-04-05T17:30:00.000Z","cwd":"/tmp/zen","message":{"id":"m1","model":"claude-sonnet-4-6","content":[{"type":"text","text":"late"}],"usage":{"input_tokens":20,"output_tokens":7,"cache_read_input_tokens":3,"cache_creation_input_tokens":0}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	c := NewCollector()
	byDate := make(map[string]*dateAgg)
	c.scanSessionJSONL(path, "zen", byDate)

	if _, ok := byDate["2026-04-05"]; ok {
		t.Fatal("unexpected UTC date bucket")
	}

	day := byDate["2026-04-06"]
	if day == nil {
		t.Fatal("missing local-date bucket")
	}
	if got := day.models["claude-sonnet-4-6"].sessions; got != 1 {
		t.Fatalf("model sessions = %d, want 1", got)
	}
	if got := day.projects["zen"].sessions; got != 1 {
		t.Fatalf("project sessions = %d, want 1", got)
	}
	if got := day.slots[0].sessions; got != 1 {
		t.Fatalf("night slot sessions = %d, want 1", got)
	}
}

func TestCollectClaudeSessionStatsIncludesSubagents(t *testing.T) {
	home := t.TempDir()
	subagentDir := filepath.Join(home, ".claude", "projects", "-tmp-zen", "session-a", "subagents")
	if err := os.MkdirAll(subagentDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(subagentDir, "agent-1.jsonl")
	content := `{"type":"assistant","timestamp":"2026-04-04T10:00:00.000Z","cwd":"/tmp/zen","message":{"id":"m1","model":"claude-sonnet-4-6","content":[{"type":"tool_use","name":"Read","input":{"file_path":"x"}}],"usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write subagent session: %v", err)
	}

	c := NewCollector()
	byDate := c.collectClaudeSessionStats(home)
	day := byDate["2026-04-04"]
	if day == nil {
		t.Fatal("missing aggregated day")
	}
	if got := day.models["claude-sonnet-4-6"].totalTokens; got != 15 {
		t.Fatalf("subagent model total = %d, want 15", got)
	}
	if got := day.tools["Read"]; got != 1 {
		t.Fatalf("subagent tool calls = %d, want 1", got)
	}
}

func TestCollectGrokStatsFromUpdates(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()
	sessionDir := filepath.Join(home, ".grok", "sessions", "%2Ftmp%2Fzen", "session-1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir grok session: %v", err)
	}
	summary := `{"info":{"id":"session-1","cwd":"/tmp/zen"},"current_model_id":"grok-4.5","created_at":"2026-04-04T09:00:00Z","updated_at":"2026-04-05T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.json"), []byte(summary), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	day1 := time.Date(2026, time.April, 4, 9, 0, 0, 0, time.UTC).Unix()
	day1Later := time.Date(2026, time.April, 4, 9, 1, 0, 0, time.UTC).Unix()
	day2 := time.Date(2026, time.April, 5, 10, 0, 0, 0, time.UTC).Unix()
	updates := fmt.Sprintf(`{"timestamp":%d,"params":{"_meta":{"totalTokens":100,"modelId":"grok-4.5"}}}
{"timestamp":%d,"params":{"_meta":{"totalTokens":160}}}
{"timestamp":%d,"params":{"_meta":{"totalTokens":200}}}
`, day1, day1Later, day2)
	if err := os.WriteFile(filepath.Join(sessionDir, "updates.jsonl"), []byte(updates), 0o644); err != nil {
		t.Fatalf("write updates: %v", err)
	}

	c := &Collector{}
	byDate := c.collectGrokStats(home)
	first := byDate["2026-04-04"]
	if first == nil {
		t.Fatal("missing first day")
	}
	second := byDate["2026-04-05"]
	if second == nil {
		t.Fatal("missing second day")
	}

	if got := first.models["grok-4.5"].totalTokens; got != 160 {
		t.Fatalf("first day grok total = %d, want 160", got)
	}
	if got := first.models["grok-4.5"].sessions; got != 1 {
		t.Fatalf("first day grok sessions = %d, want 1", got)
	}
	if got := first.projects["zen"].totalTokens; got != 160 {
		t.Fatalf("first day project total = %d, want 160", got)
	}
	if got := first.projects["zen"].sessions; got != 1 {
		t.Fatalf("first day project sessions = %d, want 1", got)
	}
	if got := first.slots[1].totalTokens; got != 160 {
		t.Fatalf("first day morning slot total = %d, want 160", got)
	}
	if got := first.slots[1].sessions; got != 1 {
		t.Fatalf("first day morning slot sessions = %d, want 1", got)
	}
	if got := second.models["grok-4.5"].totalTokens; got != 40 {
		t.Fatalf("second day grok total = %d, want 40", got)
	}
	if got := second.models["grok-4.5"].inputTokens; got != 0 {
		t.Fatalf("grok input token subtotal = %d, want 0 when breakdown is unavailable", got)
	}
	if second.models["grok-4.5"].totalTokensUnknown || !second.models["grok-4.5"].tokenBreakdownUnknown {
		t.Fatalf("grok availability = %+v, want known total and unavailable breakdown", second.models["grok-4.5"])
	}

	modelStats := buildModelStats(aggregateModelsByDate(byDate, "0000-00-00", "9999-99-99"))
	if len(modelStats) != 1 {
		t.Fatalf("model stats = %+v, want one grok model", modelStats)
	}
	if modelStats[0].CostKnown {
		t.Fatalf("grok cost should be unknown when only totalTokens is available: %+v", modelStats[0])
	}
	if modelStats[0].Cost != 0 {
		t.Fatalf("grok cost should not be estimated from totalTokens, got %v", modelStats[0].Cost)
	}
	if !modelStats[0].TotalTokensKnown || modelStats[0].TokenBreakdownKnown {
		t.Fatalf("grok model availability = %+v, want known total and unavailable breakdown", modelStats[0])
	}

	projects := buildProjectStats(aggregateProjectsByDate(byDate, "0000-00-00", "9999-99-99"))
	if len(projects) != 1 || projects[0].CostKnown || !projects[0].TotalTokensKnown || projects[0].TokenBreakdownKnown {
		t.Fatalf("grok project cost should be unknown, got %+v", projects)
	}

	cells := buildDayCells(byDate, nil, "2026-04-04", "2026-04-05")
	if len(cells) != 2 || cells[0].CostKnown || cells[1].CostKnown {
		t.Fatalf("grok day cells should have unknown cost, got %+v", cells)
	}
	if !cells[0].TotalTokensKnown || cells[0].TokenBreakdownKnown {
		t.Fatalf("grok day availability = %+v, want known total and unavailable breakdown", cells[0])
	}
}

func TestCollectGrokStatsDoesNotAddUnprovenChildTokens(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()
	sessionDir := filepath.Join(home, ".grok", "sessions", "%2Ftmp%2Fzen", "session-1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir grok session: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.json"), []byte(`{"info":{"cwd":"/tmp/zen"},"current_model_id":"grok-4.5"}`), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	timestamp := time.Date(2026, time.April, 4, 9, 0, 0, 0, time.UTC).Unix()
	updates := fmt.Sprintf(`{"timestamp":%d,"params":{"_meta":{"totalTokens":100,"modelId":"grok-4.5"}}}
{"timestamp":%d,"params":{"update":{"sessionUpdate":"subagent_spawned","subagent_id":"child-1","model":"grok-build"}}}
{"timestamp":%d,"params":{"update":{"sessionUpdate":"subagent_finished","subagent_id":"child-1","tokens_used":500}}}
{"timestamp":%d,"params":{"_meta":{"totalTokens":120}}}
`, timestamp, timestamp+1, timestamp+2, timestamp+3)
	if err := os.WriteFile(filepath.Join(sessionDir, "updates.jsonl"), []byte(updates), 0o644); err != nil {
		t.Fatalf("write updates: %v", err)
	}

	byDate := (&Collector{}).collectGrokStats(home)
	day := byDate["2026-04-04"]
	if day == nil {
		t.Fatal("missing grok day")
	}
	if got := day.models["grok-4.5"].totalTokens; got != 120 {
		t.Fatalf("parent total = %d, want 120 without unproven child tokens", got)
	}
	if _, ok := day.models["grok-build"]; ok {
		t.Fatalf("unproven child usage should not create a model row: %+v", day.models)
	}
}

func TestCollectGrokFallbackUsesSourceLabelWhenModelAndTokensAreUnreported(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()
	sessionDir := filepath.Join(home, ".grok", "sessions", "%2Ftmp%2Fzen", "session-1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir grok session: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.json"), []byte(`{"info":{"cwd":"/tmp/zen"},"created_at":"2026-04-04T09:00:00Z"}`), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	byDate := (&Collector{}).collectGrokStats(home)
	day := byDate["2026-04-04"]
	if day == nil {
		t.Fatal("missing fallback grok day")
	}
	model, ok := day.models[grokAgentUnreportedModelID]
	if !ok || model.sessions != 1 || !model.totalTokensUnknown || !model.tokenBreakdownUnknown || !model.costUnknown {
		t.Fatalf("fallback grok model = %+v, want source label with unavailable usage", model)
	}
	models := buildModelStats(day.models)
	if len(models) != 1 || models[0].Name != "Grok Agent" || models[0].TotalTokensKnown || models[0].TokenBreakdownKnown || models[0].CostKnown {
		t.Fatalf("fallback grok stats = %+v", models)
	}
}

func TestCollectCursorAgentStatsSessionsOnly(t *testing.T) {
	setTestLocalLocation(t, time.UTC)

	home := t.TempDir()
	transcriptDir := filepath.Join(home, ".cursor", "projects", "home-daoleno-workspace-zen", "agent-transcripts", "session-1")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatalf("mkdir cursor transcript: %v", err)
	}
	transcriptPath := filepath.Join(transcriptDir, "session-1.jsonl")
	content := `{"role":"user","message":{"content":[{"type":"text","text":"hello"}]}}
{"role":"assistant","message":{"content":[{"type":"text","text":"done"}]}}
`
	if err := os.WriteFile(transcriptPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write cursor transcript: %v", err)
	}
	mtime := time.Date(2026, time.April, 4, 15, 0, 0, 0, time.UTC)
	if err := os.Chtimes(transcriptPath, mtime, mtime); err != nil {
		t.Fatalf("chtimes cursor transcript: %v", err)
	}

	c := &Collector{}
	byDate := c.collectCursorAgentStats(home)
	day := byDate["2026-04-04"]
	if day == nil {
		t.Fatal("missing cursor day")
	}
	if len(day.models) != 1 {
		t.Fatalf("cursor source label should preserve session activity, got %+v", day.models)
	}
	cursorModel, ok := day.models[cursorAgentUnreportedModelID]
	if !ok || cursorModel.sessions != 1 || !cursorModel.totalTokensUnknown || !cursorModel.tokenBreakdownUnknown || !cursorModel.costUnknown {
		t.Fatalf("cursor model availability = %+v, want one session with source-unavailable usage", cursorModel)
	}
	if got := day.projects["zen"].sessions; got != 1 {
		t.Fatalf("cursor project sessions = %d, want 1", got)
	}
	if got := day.projects["zen"].totalTokens; got != 0 {
		t.Fatalf("cursor project tokens should remain zero, got %d", got)
	}
	if !day.projects["zen"].costUnknown {
		t.Fatal("cursor project cost should be marked unknown")
	}
	if !day.projects["zen"].totalTokensUnknown || !day.projects["zen"].tokenBreakdownUnknown {
		t.Fatalf("cursor project tokens should be unavailable: %+v", day.projects["zen"])
	}

	models := buildModelStats(aggregateModelsByDate(byDate, "0000-00-00", "9999-99-99"))
	if len(models) != 1 || models[0].Name != "Cursor Agent" || models[0].TotalTokensKnown || models[0].TokenBreakdownKnown || models[0].CostKnown {
		t.Fatalf("cursor model stats = %+v, want source-specific label and unavailable usage", models)
	}

	cells := buildDayCells(byDate, nil, "2026-04-04", "2026-04-04")
	if len(cells) != 1 {
		t.Fatalf("day cells = %+v, want one cursor-only cell", cells)
	}
	if cells[0].Sessions != 1 || cells[0].TotalTokens != 0 || cells[0].TotalTokensKnown || cells[0].TokenBreakdownKnown || cells[0].CostKnown {
		t.Fatalf("cursor-only day cell = %+v, want session activity with source-unavailable tokens and cost", cells[0])
	}
}

func TestRangeTotalsKeepKnownSubtotalWhenAnotherSourceDoesNotReportTokens(t *testing.T) {
	rangeData := &RangeData{
		Models: []ModelStat{{
			Name:                "GPT-5.6 Sol",
			TotalTokens:         100,
			TotalTokensKnown:    true,
			InputTokens:         70,
			OutputTokens:        30,
			TokenBreakdownKnown: true,
			Cost:                1,
			CostKnown:           true,
			Sessions:            1,
		}},
		Projects: []ProjectStat{{
			Name:                "cursor-project",
			TotalTokensKnown:    false,
			TokenBreakdownKnown: false,
			CostKnown:           false,
			Sessions:            1,
		}},
	}

	attachRangeTotals(rangeData)
	if rangeData.TotalTokens != 100 || rangeData.TotalTokensKnown || rangeData.TokenBreakdownKnown {
		t.Fatalf("range total = %+v, want visible known subtotal marked incomplete", rangeData)
	}
	if rangeData.Cost != 1 || rangeData.CostKnown {
		t.Fatalf("range cost = %+v, want visible known subtotal marked incomplete", rangeData)
	}
}

func TestRangeTotalsKeepCompleteGrokTotalAndIncompleteBreakdown(t *testing.T) {
	rangeData := &RangeData{
		Models: []ModelStat{
			{TotalTokens: 100, TotalTokensKnown: true, TokenBreakdownKnown: true, CostKnown: true},
			{TotalTokens: 50, TotalTokensKnown: true, TokenBreakdownKnown: false, CostKnown: false},
		},
	}

	attachRangeTotals(rangeData)
	if rangeData.TotalTokens != 150 || !rangeData.TotalTokensKnown || rangeData.TokenBreakdownKnown {
		t.Fatalf("range total = %+v, want complete total and unavailable breakdown", rangeData)
	}
}

func TestTokenAvailabilitySerializesKnownZeroSeparatelyFromUnreportedZero(t *testing.T) {
	known, err := json.Marshal(ModelStat{TotalTokensKnown: true, TokenBreakdownKnown: true})
	if err != nil {
		t.Fatalf("marshal known zero: %v", err)
	}
	unreported, err := json.Marshal(ModelStat{TotalTokensKnown: false, TokenBreakdownKnown: false})
	if err != nil {
		t.Fatalf("marshal unreported zero: %v", err)
	}
	if string(known) == string(unreported) || !strings.Contains(string(unreported), `"totalTokensKnown":false`) || !strings.Contains(string(unreported), `"tokenBreakdownKnown":false`) {
		t.Fatalf("availability JSON must distinguish known and unreported zero: known=%s unreported=%s", known, unreported)
	}
}

func TestModelDisplayNamesDoNotRequirePricing(t *testing.T) {
	tests := map[string]string{
		"grok-composer-2.5-fast":     "Grok Composer 2.5",
		"grok-build":                 "Grok Build 0.1",
		grokAgentUnreportedModelID:   "Grok Agent",
		cursorAgentUnreportedModelID: "Cursor Agent",
		codexUnreportedModelID:       "Codex",
		"grok-future-coder":          "Grok Future Coder",
	}
	for modelID, want := range tests {
		if got := displayName(modelID); got != want {
			t.Errorf("displayName(%q) = %q, want %q", modelID, got, want)
		}
	}
}

func TestStaticPricingIncludesGPT56SolFallback(t *testing.T) {
	got, ok := staticPricing["gpt-5.6-sol"]
	if !ok || got.input != 5 || got.output != 30 || got.cacheRead != 0.5 || got.cacheCreate != 6.25 || got.displayName != "GPT-5.6 Sol" {
		t.Fatalf("unexpected gpt-5.6-sol fallback: %+v ok=%v", got, ok)
	}
}

func TestComputeCostDoesNotDoubleCountReasoning(t *testing.T) {
	got := computeCost("gpt-5.4-mini", 1_000_000, 1_000_000, 1_000_000, 1_000_000, 0)
	want := 0.75 + 4.5 + 0.075
	if got != want {
		t.Fatalf("cost = %v, want %v", got, want)
	}

	got = computeCost("o3-mini", 0, 0, 0, 1_000_000, 0)
	want = 0.55
	if got != want {
		t.Fatalf("o3-mini cache read cost = %v, want %v", got, want)
	}
}
