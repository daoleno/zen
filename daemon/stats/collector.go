package stats

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Collector scans local Claude Code and Codex CLI data files and builds
// aggregated stats. All reads are read-only — it never modifies source files.
type Collector struct {
	mu     sync.RWMutex
	cached *StatsResponse
}

// NewCollector creates a stats collector.
func NewCollector() *Collector {
	loadPricingCache(homeDir())
	return &Collector{}
}

// Start begins periodic background scanning. The first scan runs immediately.
func (c *Collector) Start(ctx context.Context) {
	home := homeDir()
	if pricingIsStale() {
		go func() {
			if err := syncPricing(ctx, home); err == nil {
				c.refresh()
			}
		}()
	}

	c.refresh()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	pricingTicker := time.NewTicker(pricingSyncEvery)
	defer pricingTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refresh()
		case <-pricingTicker.C:
			if err := syncPricing(ctx, home); err == nil {
				c.refresh()
			}
		}
	}
}

// Stats returns the cached stats response (nil if not yet computed).
func (c *Collector) Stats() *StatsResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cached
}

// ── Pricing table ──────────────────────────────────────────

type modelPricing struct {
	displayName string
	input       float64 // $ per 1M tokens
	output      float64
	cacheRead   float64
	cacheCreate float64
}

var staticPricing = map[string]modelPricing{
	// Anthropic current models (2026)
	"claude-opus-4-6":           {displayName: "Claude Opus 4.6", input: 5, output: 25, cacheRead: 0.50, cacheCreate: 6.25},
	"claude-sonnet-4-6":         {displayName: "Claude Sonnet 4.6", input: 3, output: 15, cacheRead: 0.30, cacheCreate: 3.75},
	"claude-haiku-4-5-20251001": {displayName: "Claude Haiku 4.5", input: 1, output: 5, cacheRead: 0.10, cacheCreate: 1.25},
	// Anthropic legacy models
	"claude-opus-4-5-20251101":   {displayName: "Claude Opus 4.5", input: 15, output: 75, cacheRead: 1.50, cacheCreate: 18.75},
	"claude-sonnet-4-5-20250929": {displayName: "Claude Sonnet 4.5", input: 3, output: 15, cacheRead: 0.30, cacheCreate: 3.75},
	// OpenAI GPT models
	"gpt-4.1":      {displayName: "GPT-4.1", input: 2, output: 8, cacheRead: 0.50, cacheCreate: 2},
	"gpt-4.1-mini": {displayName: "GPT-4.1 Mini", input: 0.40, output: 1.60, cacheRead: 0.10, cacheCreate: 0.40},
	"gpt-4.1-nano": {displayName: "GPT-4.1 Nano", input: 0.10, output: 0.40, cacheRead: 0.025, cacheCreate: 0.10},
	"gpt-4o":       {displayName: "GPT-4o", input: 2.50, output: 10, cacheRead: 1.25, cacheCreate: 2.50},
	"gpt-4o-mini":  {displayName: "GPT-4o Mini", input: 0.15, output: 0.60, cacheRead: 0.075, cacheCreate: 0.15},
	// OpenAI reasoning models
	"o3":      {displayName: "o3", input: 2, output: 8, cacheRead: 0.50, cacheCreate: 2},
	"o3-mini": {displayName: "o3-mini", input: 1.10, output: 4.40, cacheRead: 0.55, cacheCreate: 1.10},
	"o4-mini": {displayName: "o4-mini", input: 1.10, output: 4.40, cacheRead: 0.275, cacheCreate: 1.10},
	// OpenAI GPT-5.x models
	"gpt-5.4":      {displayName: "GPT-5.4", input: 2.50, output: 15, cacheRead: 0.25, cacheCreate: 2.50},
	"gpt-5.4-mini": {displayName: "GPT-5.4 Mini", input: 0.75, output: 4.50, cacheRead: 0.075, cacheCreate: 0.75},
	"gpt-5.4-nano": {displayName: "GPT-5.4 Nano", input: 0.20, output: 1.25, cacheRead: 0.02, cacheCreate: 0.20},
	// OpenAI Codex
	"codex-mini-latest": {displayName: "Codex Mini", input: 1.50, output: 6, cacheRead: 0.375, cacheCreate: 1.50},
}

func computeCost(modelID string, input, output, reasoning, cacheRead, cacheCreate int64) float64 {
	p, ok := currentPricing(modelID)
	if !ok {
		return 0
	}
	return float64(input)/1e6*p.input +
		float64(output+reasoning)/1e6*p.output +
		float64(cacheRead)/1e6*p.cacheRead +
		float64(cacheCreate)/1e6*p.cacheCreate
}

func displayName(modelID string) string {
	if p, ok := currentPricing(modelID); ok {
		return p.displayName
	}
	return modelID
}

type dateAgg struct {
	models   map[string]modelAggEntry
	tools    map[string]int
	skills   map[string]*skillEntry
	projects map[string]*projectAggEntry
	slots    [4]slotAgg // Night(0-5), Morning(6-11), Afternoon(12-17), Evening(18-23)
}

type slotAgg struct {
	totalTokens  int64
	inputTokens  int64
	outputTokens int64
	reasoning    int64
	cacheRead    int64
	cacheCreate  int64
	sessions     int
}

type projectAggEntry struct {
	totalTokens  int64
	inputTokens  int64
	outputTokens int64
	reasoning    int64
	cacheRead    int64
	cacheCreate  int64
	cost         float64
	sessions     int
}

type sessionUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// ── Refresh logic ──────────────────────────────────────────

func (c *Collector) refresh() {
	home := homeDir()
	if home == "" {
		return
	}

	// Collect range-scoped Claude usage from timestamped session JSONLs.
	claudeByDate := c.collectClaudeSessionStats(home)

	// Collect Codex CLI data.
	codexDaily, codexModelsByDate, codexProjectsByDate := c.collectCodexStats(home)

	dailyMap := buildDailySessions(claudeByDate, codexDaily)

	// Build time-range aggregates
	now := time.Now()
	today := now.Format("2006-01-02")
	weekAgo := now.AddDate(0, 0, -6).Format("2006-01-02")
	monthAgo := now.AddDate(0, 0, -29).Format("2006-01-02")

	ranges := map[string]*RangeData{
		"day":   c.aggregateRange(dailyMap, today, "9999-99-99"),
		"week":  c.aggregateRange(dailyMap, weekAgo, "9999-99-99"),
		"month": c.aggregateRange(dailyMap, monthAgo, "9999-99-99"),
		"all":   c.aggregateRange(dailyMap, "0000-00-00", "9999-99-99"),
	}
	for rangeName, rd := range ranges {
		fromDate := "0000-00-00"
		switch rangeName {
		case "day":
			fromDate = today
		case "week":
			fromDate = weekAgo
		case "month":
			fromDate = monthAgo
		}

		modelAgg := aggregateModelsByDate(claudeByDate, fromDate, "9999-99-99")
		mergeModelAgg(modelAgg, aggregateCodexModelsByDate(codexModelsByDate, fromDate, "9999-99-99"))
		rd.Models = buildModelStats(modelAgg)
		rd.Tools = buildToolStats(aggregateToolsByDate(claudeByDate, fromDate, "9999-99-99"))
		rd.Skills = buildSkillStats(aggregateSkillsByDate(claudeByDate, fromDate, "9999-99-99"))
		rd.Projects = buildProjectStats(
			aggregateProjectsByDate(claudeByDate, fromDate, "9999-99-99"),
			aggregateCodexProjectsByDate(codexProjectsByDate, fromDate, "9999-99-99"),
		)
		attachRangeTotals(rd)

		// Build per-day activity cells for this range.
		rd.Days = buildDayCells(claudeByDate, codexModelsByDate, fromDate, "9999-99-99")
	}

	resp := &StatsResponse{
		Type:   "stats_data",
		Ranges: ranges,
	}

	c.mu.Lock()
	c.cached = resp
	c.mu.Unlock()

	log.Printf("[stats] refresh complete: %d days of data", len(dailyMap))
}

// ── dailyEntry holds per-date aggregated data ──────────────

type dailyEntry struct {
	sessions int
}

type modelAggEntry struct {
	totalTokens  int64
	inputTokens  int64
	outputTokens int64
	reasoning    int64
	cacheRead    int64
	cacheCreate  int64
	sessions     int
}

// collectClaudeSessionStats scans session JSONL files and groups model/tool/skill/project
// usage by date so the UI range selectors can be scoped correctly.
func (c *Collector) collectClaudeSessionStats(home string) map[string]*dateAgg {
	byDate := make(map[string]*dateAgg)

	projectsDir := filepath.Join(home, ".claude", "projects")
	if _, err := os.Stat(projectsDir); err != nil {
		return byDate
	}

	err := filepath.WalkDir(projectsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		rel, err := filepath.Rel(projectsDir, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		projectName := ""
		if len(parts) == 1 {
			projectName = decodeProjectDir(parts[0])
		} else {
			projectName = decodeProjectDir(parts[0])
		}
		c.scanSessionJSONL(path, projectName, byDate)
		return nil
	})
	if err != nil {
		return byDate
	}

	return byDate
}

type skillEntry struct {
	calls    int
	projects map[string]bool
}

func (c *Collector) scanSessionJSONL(path, projectName string, byDate map[string]*dateAgg) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	type sessionLine struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Cwd       string `json:"cwd"`
		Message   struct {
			ID      string          `json:"id"`
			Model   string          `json:"model"`
			Content json.RawMessage `json:"content"`
			Usage   sessionUsage    `json:"usage"`
		} `json:"message"`
	}
	type usageRecord struct {
		date         string
		hour         int // 0-23, for heatmap slot bucketing
		projectName  string
		modelID      string
		totalTokens  int64
		inputTokens  int64
		outputTokens int64
		reasoning    int64
		cacheRead    int64
		cacheCreate  int64
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	messageUsage := make(map[string]*usageRecord)
	projectDates := make(map[string]map[string]bool)

	for scanner.Scan() {
		line := scanner.Bytes()

		var entry sessionLine
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		date := dateFromTimestamp(entry.Timestamp)
		if date == "" {
			continue
		}

		lineProjectName := projectName
		if entry.Cwd != "" {
			lineProjectName = filepath.Base(entry.Cwd)
		}
		if lineProjectName != "" {
			if projectDates[lineProjectName] == nil {
				projectDates[lineProjectName] = make(map[string]bool)
			}
			projectDates[lineProjectName][date] = true
		}

		if entry.Type == "assistant" {
			if hasUsage(entry.Message.Usage) {
				key := entry.Message.ID
				if key == "" {
					key = entry.Timestamp + ":" + entry.Message.Model
				}
				rec := messageUsage[key]
				if rec == nil {
					rec = &usageRecord{
						date:        date,
						hour:        hourFromTimestamp(entry.Timestamp),
						projectName: lineProjectName,
						modelID:     entry.Message.Model,
					}
					messageUsage[key] = rec
				}
				rec.inputTokens = max64(rec.inputTokens, entry.Message.Usage.InputTokens)
				rec.outputTokens = max64(rec.outputTokens, entry.Message.Usage.OutputTokens)
				rec.cacheRead = max64(rec.cacheRead, entry.Message.Usage.CacheReadInputTokens)
				rec.cacheCreate = max64(rec.cacheCreate, entry.Message.Usage.CacheCreationInputTokens)
				rec.totalTokens = max64(rec.totalTokens, entry.Message.Usage.InputTokens+
					entry.Message.Usage.OutputTokens+
					entry.Message.Usage.CacheReadInputTokens+
					entry.Message.Usage.CacheCreationInputTokens)
			}

			if !strings.Contains(string(line), "tool_use") {
				continue
			}

			var content []struct {
				Type  string          `json:"type"`
				Name  string          `json:"name"`
				Input json.RawMessage `json:"input"`
			}
			if err := json.Unmarshal(entry.Message.Content, &content); err != nil {
				continue
			}

			agg := ensureDateAgg(byDate, date)
			for _, c := range content {
				if c.Type != "tool_use" {
					continue
				}
				if c.Name == "Skill" {
					var inp struct {
						Skill string `json:"skill"`
					}
					if err := json.Unmarshal(c.Input, &inp); err == nil && inp.Skill != "" {
						se := agg.skills[inp.Skill]
						if se == nil {
							se = &skillEntry{projects: make(map[string]bool)}
							agg.skills[inp.Skill] = se
						}
						se.calls++
						if lineProjectName != "" {
							se.projects[lineProjectName] = true
						}
					}
				} else {
					agg.tools[c.Name]++
				}
			}
		}
	}

	for name, dates := range projectDates {
		for date := range dates {
			agg := ensureDateAgg(byDate, date)
			pe := agg.projects[name]
			if pe == nil {
				pe = &projectAggEntry{}
				agg.projects[name] = pe
			}
			pe.sessions++
		}
	}

	modelSessionsByDate := make(map[string]map[string]bool)

	for _, rec := range messageUsage {
		if rec.date == "" || rec.modelID == "" {
			continue
		}
		agg := ensureDateAgg(byDate, rec.date)
		m := agg.models[rec.modelID]
		m.totalTokens += rec.totalTokens
		m.inputTokens += rec.inputTokens
		m.outputTokens += rec.outputTokens
		m.reasoning += rec.reasoning
		m.cacheRead += rec.cacheRead
		m.cacheCreate += rec.cacheCreate
		if modelSessionsByDate[rec.date] == nil {
			modelSessionsByDate[rec.date] = make(map[string]bool)
		}
		if !modelSessionsByDate[rec.date][rec.modelID] {
			m.sessions++
			modelSessionsByDate[rec.date][rec.modelID] = true
		}
		agg.models[rec.modelID] = m

		// Accumulate into time-of-day slot.
		slot := rec.hour / 6
		if slot > 3 {
			slot = 3
		}
		agg.slots[slot].totalTokens += rec.totalTokens
		agg.slots[slot].inputTokens += rec.inputTokens
		agg.slots[slot].outputTokens += rec.outputTokens
		agg.slots[slot].reasoning += rec.reasoning
		agg.slots[slot].cacheRead += rec.cacheRead
		agg.slots[slot].cacheCreate += rec.cacheCreate
		agg.slots[slot].sessions++

		if rec.projectName == "" {
			continue
		}
		p := agg.projects[rec.projectName]
		if p == nil {
			p = &projectAggEntry{}
			agg.projects[rec.projectName] = p
		}
		p.totalTokens += rec.totalTokens
		p.inputTokens += rec.inputTokens
		p.outputTokens += rec.outputTokens
		p.reasoning += rec.reasoning
		p.cacheRead += rec.cacheRead
		p.cacheCreate += rec.cacheCreate
		p.cost += computeCost(rec.modelID, rec.inputTokens, rec.outputTokens, rec.reasoning, rec.cacheRead, rec.cacheCreate)
	}
}

// decodeProjectDir converts "-home-daoleno-workspace-zen" to "zen" (last path component).
// The Claude Code directory encoding replaces "/" with "-" and prepends "-".
func decodeProjectDir(name string) string {
	name = strings.TrimSuffix(name, ".jsonl")
	// Reconstruct the path: leading "-" becomes "/", internal "-" become "/"
	if strings.HasPrefix(name, "-") {
		path := strings.ReplaceAll(name, "-", "/")
		// Use the last path component as the project name
		parts := strings.Split(strings.TrimRight(path, "/"), "/")
		if len(parts) > 0 {
			last := parts[len(parts)-1]
			if last != "" {
				return last
			}
		}
	}
	return name
}

// ── Codex CLI collection ───────────────────────────────────

type codexDailyEntry struct {
	sessions        int
	totalTokens     int64
	inputTokens     int64
	outputTokens    int64
	reasoningTokens int64
	cacheRead       int64
}

func (c *Collector) collectCodexStats(home string) (map[string]codexDailyEntry, map[string]map[string]modelAggEntry, map[string]map[string]*projectAggEntry) {
	daily := make(map[string]codexDailyEntry)
	modelsByDate := make(map[string]map[string]modelAggEntry)
	projectsByDate := make(map[string]map[string]*projectAggEntry)

	dbPath := filepath.Join(home, ".codex", "state_5.sqlite")
	if _, err := os.Stat(dbPath); err != nil {
		return daily, modelsByDate, projectsByDate
	}

	// Check if sqlite3 is available
	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		log.Printf("[stats] sqlite3 not found, skipping Codex stats")
		return daily, modelsByDate, projectsByDate
	}

	out, err := exec.Command(sqlite3, "-json", dbPath,
		"SELECT id, cwd, model, tokens_used, created_at, rollout_path FROM threads WHERE tokens_used > 0").Output()
	if err != nil {
		log.Printf("[stats] sqlite3 query failed: %v", err)
		return daily, modelsByDate, projectsByDate
	}

	var threads []struct {
		ID          string `json:"id"`
		Cwd         string `json:"cwd"`
		Model       string `json:"model"`
		TokensUsed  int64  `json:"tokens_used"`
		CreatedAt   int64  `json:"created_at"`
		RolloutPath string `json:"rollout_path"`
	}
	if err := json.Unmarshal(out, &threads); err != nil {
		log.Printf("[stats] failed to parse sqlite3 output: %v", err)
		return daily, modelsByDate, projectsByDate
	}

	for _, t := range threads {
		// created_at is Unix timestamp in seconds
		date := time.Unix(t.CreatedAt, 0).Format("2006-01-02")
		usage, err := readCodexUsage(t.RolloutPath)
		if err != nil {
			usage = codexUsage{
				totalTokens: t.TokensUsed,
				inputTokens: t.TokensUsed,
			}
		}

		d := daily[date]
		d.sessions++
		d.totalTokens += usage.totalTokens
		d.inputTokens += usage.inputTokens
		d.outputTokens += usage.outputTokens
		d.reasoningTokens += usage.reasoningTokens
		d.cacheRead += usage.cacheRead
		daily[date] = d

		modelID := t.Model
		if modelID == "" {
			modelID = "codex-mini-latest" // Codex CLI default model
		}
		models := modelsByDate[date]
		if models == nil {
			models = make(map[string]modelAggEntry)
			modelsByDate[date] = models
		}
		m := models[modelID]
		m.totalTokens += usage.totalTokens
		m.inputTokens += usage.inputTokens
		m.outputTokens += usage.outputTokens
		m.reasoning += usage.reasoningTokens
		m.cacheRead += usage.cacheRead
		m.sessions++
		models[modelID] = m

		projectName := filepath.Base(t.Cwd)
		if projectName == "" || projectName == "." || projectName == "/" {
			continue
		}
		projects := projectsByDate[date]
		if projects == nil {
			projects = make(map[string]*projectAggEntry)
			projectsByDate[date] = projects
		}
		p := projects[projectName]
		if p == nil {
			p = &projectAggEntry{}
			projects[projectName] = p
		}
		p.totalTokens += usage.totalTokens
		p.inputTokens += usage.inputTokens
		p.outputTokens += usage.outputTokens
		p.reasoning += usage.reasoningTokens
		p.cacheRead += usage.cacheRead
		p.cost += computeCost(modelID, usage.inputTokens, usage.outputTokens, usage.reasoningTokens, usage.cacheRead, 0)
		p.sessions++
	}

	return daily, modelsByDate, projectsByDate
}

type codexUsage struct {
	totalTokens     int64
	inputTokens     int64
	outputTokens    int64
	reasoningTokens int64
	cacheRead       int64
}

func readCodexUsage(path string) (codexUsage, error) {
	if path == "" {
		return codexUsage{}, fmt.Errorf("empty rollout path")
	}

	f, err := os.Open(path)
	if err != nil {
		return codexUsage{}, err
	}
	defer f.Close()

	type tokenCountLine struct {
		Type    string `json:"type"`
		Payload struct {
			Type string `json:"type"`
			Info *struct {
				TotalTokenUsage *struct {
					InputTokens           int64 `json:"input_tokens"`
					CachedInputTokens     int64 `json:"cached_input_tokens"`
					OutputTokens          int64 `json:"output_tokens"`
					ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
					TotalTokens           int64 `json:"total_tokens"`
				} `json:"total_token_usage"`
			} `json:"info"`
		} `json:"payload"`
	}

	var usage codexUsage
	found := false

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var line tokenCountLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Type != "event_msg" || line.Payload.Type != "token_count" {
			continue
		}
		if line.Payload.Info == nil || line.Payload.Info.TotalTokenUsage == nil {
			continue
		}

		total := line.Payload.Info.TotalTokenUsage
		uncachedInput := total.InputTokens - total.CachedInputTokens
		if uncachedInput < 0 {
			uncachedInput = total.InputTokens
		}
		totalTokens := total.TotalTokens
		if totalTokens <= 0 {
			totalTokens = total.InputTokens + total.OutputTokens
		}

		usage = codexUsage{
			totalTokens:     totalTokens,
			inputTokens:     uncachedInput,
			outputTokens:    total.OutputTokens,
			reasoningTokens: total.ReasoningOutputTokens,
			cacheRead:       total.CachedInputTokens,
		}
		found = true
	}
	if err := scanner.Err(); err != nil {
		return codexUsage{}, err
	}
	if !found {
		return codexUsage{}, fmt.Errorf("no token_count event found")
	}
	return usage, nil
}

// ── Aggregation helpers ────────────────────────────────────

func (c *Collector) aggregateRange(dailyMap map[string]dailyEntry, fromDate, toDate string) *RangeData {
	rd := &RangeData{}
	for date, d := range dailyMap {
		if date < fromDate || date > toDate {
			continue
		}
		rd.Sessions += d.sessions
	}
	return rd
}

func buildDailySessions(claudeByDate map[string]*dateAgg, codexDaily map[string]codexDailyEntry) map[string]dailyEntry {
	dailyMap := make(map[string]dailyEntry)
	for date, agg := range claudeByDate {
		d := dailyMap[date]
		seenProjects := make(map[string]bool)
		for name, project := range agg.projects {
			if project.sessions <= 0 || seenProjects[name] {
				continue
			}
			d.sessions += project.sessions
			seenProjects[name] = true
		}
		dailyMap[date] = d
	}
	for date, cd := range codexDaily {
		d := dailyMap[date]
		d.sessions += cd.sessions
		dailyMap[date] = d
	}
	return dailyMap
}

func buildModelStats(modelAgg map[string]modelAggEntry) []ModelStat {
	var result []ModelStat
	for modelID, m := range modelAgg {
		cost := computeCost(modelID, m.inputTokens, m.outputTokens, m.reasoning, m.cacheRead, m.cacheCreate)
		result = append(result, ModelStat{
			Name:            displayName(modelID),
			TotalTokens:     m.totalTokens,
			InputTokens:     m.inputTokens,
			OutputTokens:    m.outputTokens,
			ReasoningTokens: m.reasoning,
			CacheRead:       m.cacheRead,
			CacheCreate:     m.cacheCreate,
			Cost:            cost,
			Sessions:        m.sessions,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Cost > result[j].Cost })
	return result
}

func buildProjectStats(projectAgg map[string]*projectAggEntry, extra ...map[string]*projectAggEntry) []ProjectStat {
	for _, more := range extra {
		mergeProjectAgg(projectAgg, more)
	}

	var result []ProjectStat
	for name, p := range projectAgg {
		result = append(result, ProjectStat{
			Name:            name,
			TotalTokens:     p.totalTokens,
			InputTokens:     p.inputTokens,
			OutputTokens:    p.outputTokens,
			ReasoningTokens: p.reasoning,
			CacheRead:       p.cacheRead,
			CacheCreate:     p.cacheCreate,
			Cost:            p.cost,
			Sessions:        p.sessions,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Cost == result[j].Cost {
			return result[i].Sessions > result[j].Sessions
		}
		return result[i].Cost > result[j].Cost
	})
	return result
}

func buildToolStats(tools map[string]int) []ToolStat {
	var result []ToolStat
	for name, calls := range tools {
		result = append(result, ToolStat{Name: name, Calls: calls})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Calls > result[j].Calls })
	return result
}

func buildSkillStats(skills map[string]*skillEntry) []SkillStat {
	var result []SkillStat
	for name, se := range skills {
		projects := make([]string, 0, len(se.projects))
		for p := range se.projects {
			projects = append(projects, p)
		}
		sort.Strings(projects)
		result = append(result, SkillStat{Name: name, Calls: se.calls, Projects: projects})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Calls > result[j].Calls })
	return result
}

func attachRangeTotals(rd *RangeData) {
	var totalCost float64
	var totalTokens, totalInput, totalOutput, totalReasoning, totalCacheRead, totalCacheCreate int64
	for _, m := range rd.Models {
		totalCost += m.Cost
		totalTokens += m.TotalTokens
		totalInput += m.InputTokens
		totalOutput += m.OutputTokens
		totalReasoning += m.ReasoningTokens
		totalCacheRead += m.CacheRead
		totalCacheCreate += m.CacheCreate
	}
	rd.Cost = totalCost
	rd.TotalTokens = totalTokens
	rd.InputTokens = totalInput
	rd.OutputTokens = totalOutput
	rd.ReasoningTokens = totalReasoning
	rd.CacheRead = totalCacheRead
	rd.CacheCreate = totalCacheCreate
}

// buildDayCells aggregates per-day activity from Claude and Codex data.
func buildDayCells(claudeByDate map[string]*dateAgg, codexModelsByDate map[string]map[string]modelAggEntry, fromDate, toDate string) []DayCell {
	dayCosts := make(map[string]*DayCell)

	// Claude data
	for date, agg := range claudeByDate {
		if date < fromDate || date > toDate {
			continue
		}
		dc := dayCosts[date]
		if dc == nil {
			dc = &DayCell{Date: date}
			dayCosts[date] = dc
		}
		for modelID, m := range agg.models {
			dc.TotalTokens += m.totalTokens
			dc.InputTokens += m.inputTokens
			dc.OutputTokens += m.outputTokens
			dc.ReasoningTokens += m.reasoning
			dc.CacheRead += m.cacheRead
			dc.CacheCreate += m.cacheCreate
			dc.Sessions += m.sessions
			dc.Cost += computeCost(modelID, m.inputTokens, m.outputTokens, m.reasoning, m.cacheRead, m.cacheCreate)
		}
	}

	// Codex data
	for date, models := range codexModelsByDate {
		if date < fromDate || date > toDate {
			continue
		}
		dc := dayCosts[date]
		if dc == nil {
			dc = &DayCell{Date: date}
			dayCosts[date] = dc
		}
		for modelID, m := range models {
			dc.TotalTokens += m.totalTokens
			dc.InputTokens += m.inputTokens
			dc.OutputTokens += m.outputTokens
			dc.ReasoningTokens += m.reasoning
			dc.CacheRead += m.cacheRead
			dc.Sessions += m.sessions
			dc.Cost += computeCost(modelID, m.inputTokens, m.outputTokens, m.reasoning, m.cacheRead, m.cacheCreate)
		}
	}

	// Sort by date ascending
	result := make([]DayCell, 0, len(dayCosts))
	for _, dc := range dayCosts {
		result = append(result, *dc)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Date < result[j].Date })
	return result
}

func homeDir() string {
	u, err := user.Current()
	if err != nil {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return h
	}
	return u.HomeDir
}

func ensureDateAgg(byDate map[string]*dateAgg, date string) *dateAgg {
	agg := byDate[date]
	if agg != nil {
		return agg
	}
	agg = &dateAgg{
		models:   make(map[string]modelAggEntry),
		tools:    make(map[string]int),
		skills:   make(map[string]*skillEntry),
		projects: make(map[string]*projectAggEntry),
	}
	byDate[date] = agg
	return agg
}

// hourFromTimestamp extracts the hour (0-23) from an ISO 8601 timestamp.
// Returns 12 as a safe default if parsing fails.
func hourFromTimestamp(ts string) int {
	// Format: "2006-01-02T15:04:05.000Z"
	if len(ts) < 13 {
		return 12
	}
	h := 0
	for _, c := range ts[11:13] {
		if c < '0' || c > '9' {
			return 12
		}
		h = h*10 + int(c-'0')
	}
	if h > 23 {
		return 12
	}
	return h
}

func dateFromTimestamp(ts string) string {
	if len(ts) < len("2006-01-02") {
		return ""
	}
	return ts[:10]
}

func hasUsage(usage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}) bool {
	return usage.InputTokens > 0 ||
		usage.OutputTokens > 0 ||
		usage.CacheReadInputTokens > 0 ||
		usage.CacheCreationInputTokens > 0
}

func aggregateModelsByDate(byDate map[string]*dateAgg, fromDate, toDate string) map[string]modelAggEntry {
	result := make(map[string]modelAggEntry)
	for date, agg := range byDate {
		if date < fromDate || date > toDate {
			continue
		}
		mergeModelAgg(result, agg.models)
	}
	return result
}

func aggregateCodexModelsByDate(byDate map[string]map[string]modelAggEntry, fromDate, toDate string) map[string]modelAggEntry {
	result := make(map[string]modelAggEntry)
	for date, models := range byDate {
		if date < fromDate || date > toDate {
			continue
		}
		mergeModelAgg(result, models)
	}
	return result
}

func aggregateToolsByDate(byDate map[string]*dateAgg, fromDate, toDate string) map[string]int {
	result := make(map[string]int)
	for date, agg := range byDate {
		if date < fromDate || date > toDate {
			continue
		}
		for name, calls := range agg.tools {
			result[name] += calls
		}
	}
	return result
}

func aggregateSkillsByDate(byDate map[string]*dateAgg, fromDate, toDate string) map[string]*skillEntry {
	result := make(map[string]*skillEntry)
	for date, agg := range byDate {
		if date < fromDate || date > toDate {
			continue
		}
		for name, skill := range agg.skills {
			dst := result[name]
			if dst == nil {
				dst = &skillEntry{projects: make(map[string]bool)}
				result[name] = dst
			}
			dst.calls += skill.calls
			for project := range skill.projects {
				dst.projects[project] = true
			}
		}
	}
	return result
}

func aggregateProjectsByDate(byDate map[string]*dateAgg, fromDate, toDate string) map[string]*projectAggEntry {
	result := make(map[string]*projectAggEntry)
	for date, agg := range byDate {
		if date < fromDate || date > toDate {
			continue
		}
		mergeProjectAgg(result, agg.projects)
	}
	return result
}

func aggregateCodexProjectsByDate(byDate map[string]map[string]*projectAggEntry, fromDate, toDate string) map[string]*projectAggEntry {
	result := make(map[string]*projectAggEntry)
	for date, projects := range byDate {
		if date < fromDate || date > toDate {
			continue
		}
		mergeProjectAgg(result, projects)
	}
	return result
}

func mergeModelAgg(dst, src map[string]modelAggEntry) {
	for modelID, item := range src {
		current := dst[modelID]
		current.totalTokens += item.totalTokens
		current.inputTokens += item.inputTokens
		current.outputTokens += item.outputTokens
		current.reasoning += item.reasoning
		current.cacheRead += item.cacheRead
		current.cacheCreate += item.cacheCreate
		current.sessions += item.sessions
		dst[modelID] = current
	}
}

func mergeProjectAgg(dst, src map[string]*projectAggEntry) {
	for name, item := range src {
		current := dst[name]
		if current == nil {
			current = &projectAggEntry{}
			dst[name] = current
		}
		current.totalTokens += item.totalTokens
		current.inputTokens += item.inputTokens
		current.outputTokens += item.outputTokens
		current.reasoning += item.reasoning
		current.cacheRead += item.cacheRead
		current.cacheCreate += item.cacheCreate
		current.cost += item.cost
		current.sessions += item.sessions
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
