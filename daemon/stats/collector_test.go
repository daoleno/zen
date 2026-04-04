package stats

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestCollectorSmoke(t *testing.T) {
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

	fmt.Printf("Cost: $%.2f\n", all.Cost)
	fmt.Printf("Sessions: %d\n", all.Sessions)
	fmt.Printf("Models: %d\n", len(all.Models))
	fmt.Printf("Projects: %d\n", len(all.Projects))
	fmt.Printf("Skills: %d\n", len(all.Skills))
	fmt.Printf("Tools: %d\n", len(all.Tools))
	fmt.Printf("InputTokens: %d\n", all.InputTokens)
	fmt.Printf("OutputTokens: %d\n", all.OutputTokens)

	for _, m := range all.Models {
		fmt.Printf("  Model: %s cost=$%.2f in=%d out=%d cache_read=%d\n", m.Name, m.Cost, m.InputTokens, m.OutputTokens, m.CacheRead)
	}
	for _, p := range all.Projects {
		fmt.Printf("  Project: %s sessions=%d\n", p.Name, p.Sessions)
	}
	for _, sk := range all.Skills {
		fmt.Printf("  Skill: %s calls=%d projects=%v\n", sk.Name, sk.Calls, sk.Projects)
	}
	for i, t := range all.Tools {
		if i < 10 {
			fmt.Printf("  Tool: %s calls=%d\n", t.Name, t.Calls)
		}
	}

	fmt.Printf("Days (all): %d\n", len(all.Days))
	for i, d := range all.Days {
		if i < 5 || i >= len(all.Days)-3 {
			fmt.Printf("  %s: $%.2f %d sess\n", d.Date, d.Cost, d.Sessions)
		}
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

func TestScanSessionJSONLCrossDayBucketsSessionsPerDay(t *testing.T) {
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

func TestComputeCostUsesReasoningAndUpdatedPricing(t *testing.T) {
	got := computeCost("gpt-5.4-mini", 1_000_000, 1_000_000, 1_000_000, 1_000_000, 0)
	want := 0.75 + 9.0 + 0.075
	if got != want {
		t.Fatalf("cost = %v, want %v", got, want)
	}

	got = computeCost("o3-mini", 0, 0, 0, 1_000_000, 0)
	want = 0.55
	if got != want {
		t.Fatalf("o3-mini cache read cost = %v, want %v", got, want)
	}
}
