package stats

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestCollectorSmoke(t *testing.T) {
	c := NewCollector()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	go c.Start(ctx)
	time.Sleep(2 * time.Second)

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
