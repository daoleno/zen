package stats

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Collector scans local Claude Code, Codex CLI, Grok, and Cursor Agent data files and builds
// aggregated stats. All reads are read-only — it never modifies source files.
type Collector struct {
	mu                       sync.RWMutex
	cached                   *StatsResponse
	codexUsageClient         codexUsageHTTPClient
	codexUsageEndpoint       string
	codexUsageTimeout        time.Duration
	opencodeGoClient         openCodeGoHTTPClient
	opencodeGoEndpoint       string
	opencodeGoChatEndpoint   string
	opencodeGoServerEndpoint string
	opencodeGoTimeout        time.Duration
	now                      func() time.Time
	lastCodexSubscription    *CodexSubscriptionUsage
	lastCodexAuthFingerprint string
}

// NewCollector creates a stats collector.
func NewCollector() *Collector {
	loadPricingCache(homeDir())
	return &Collector{
		codexUsageClient:         &http.Client{Timeout: 8 * time.Second},
		codexUsageEndpoint:       codexUsageEndpoint,
		codexUsageTimeout:        8 * time.Second,
		opencodeGoClient:         &http.Client{Timeout: 15 * time.Second},
		opencodeGoEndpoint:       opencodeGoModelsEndpoint,
		opencodeGoChatEndpoint:   opencodeGoChatEndpoint,
		opencodeGoServerEndpoint: opencodeGoServerEndpoint,
		opencodeGoTimeout:        15 * time.Second,
		now:                      time.Now,
	}
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
	"claude-fable-5":            {displayName: "Claude Fable 5", input: 10, output: 50, cacheRead: 1, cacheCreate: 12.5},
	"claude-opus-4-8":           {displayName: "Claude Opus 4.8", input: 5, output: 25, cacheRead: 0.50, cacheCreate: 6.25},
	"claude-opus-4-7":           {displayName: "Claude Opus 4.7", input: 5, output: 25, cacheRead: 0.50, cacheCreate: 6.25},
	"claude-opus-4-6":           {displayName: "Claude Opus 4.6", input: 5, output: 25, cacheRead: 0.50, cacheCreate: 6.25},
	"claude-sonnet-5":           {displayName: "Claude Sonnet 5", input: 2, output: 10, cacheRead: 0.20, cacheCreate: 2.5},
	"claude-sonnet-4-6":         {displayName: "Claude Sonnet 4.6", input: 3, output: 15, cacheRead: 0.30, cacheCreate: 3.75},
	"claude-haiku-4-5":          {displayName: "Claude Haiku 4.5 (latest)", input: 1, output: 5, cacheRead: 0.10, cacheCreate: 1.25},
	"claude-haiku-4-5-20251001": {displayName: "Claude Haiku 4.5", input: 1, output: 5, cacheRead: 0.10, cacheCreate: 1.25},
	// Anthropic legacy models
	"claude-opus-4-5":            {displayName: "Claude Opus 4.5 (latest)", input: 5, output: 25, cacheRead: 0.50, cacheCreate: 6.25},
	"claude-opus-4-5-20251101":   {displayName: "Claude Opus 4.5", input: 5, output: 25, cacheRead: 0.50, cacheCreate: 6.25},
	"claude-sonnet-4-5":          {displayName: "Claude Sonnet 4.5 (latest)", input: 3, output: 15, cacheRead: 0.30, cacheCreate: 3.75},
	"claude-sonnet-4-5-20250929": {displayName: "Claude Sonnet 4.5", input: 3, output: 15, cacheRead: 0.30, cacheCreate: 3.75},
	"claude-opus-4-1":            {displayName: "Claude Opus 4.1 (latest)", input: 15, output: 75, cacheRead: 1.50, cacheCreate: 18.75},
	"claude-opus-4-1-20250805":   {displayName: "Claude Opus 4.1", input: 15, output: 75, cacheRead: 1.50, cacheCreate: 18.75},
	"claude-opus-4-0":            {displayName: "Claude Opus 4 (latest)", input: 15, output: 75, cacheRead: 1.50, cacheCreate: 18.75},
	"claude-opus-4-20250514":     {displayName: "Claude Opus 4", input: 15, output: 75, cacheRead: 1.50, cacheCreate: 18.75},
	"claude-sonnet-4-0":          {displayName: "Claude Sonnet 4 (latest)", input: 3, output: 15, cacheRead: 0.30, cacheCreate: 3.75},
	"claude-sonnet-4-20250514":   {displayName: "Claude Sonnet 4", input: 3, output: 15, cacheRead: 0.30, cacheCreate: 3.75},
	// OpenAI GPT models
	"gpt-4.1":      {displayName: "GPT-4.1", input: 2, output: 8, cacheRead: 0.50},
	"gpt-4.1-mini": {displayName: "GPT-4.1 mini", input: 0.40, output: 1.60, cacheRead: 0.10},
	"gpt-4.1-nano": {displayName: "GPT-4.1 nano", input: 0.10, output: 0.40, cacheRead: 0.03},
	"gpt-4o":       {displayName: "GPT-4o", input: 2.50, output: 10, cacheRead: 1.25},
	"gpt-4o-mini":  {displayName: "GPT-4o mini", input: 0.15, output: 0.60, cacheRead: 0.08},
	// OpenAI reasoning models
	"o3":      {displayName: "o3", input: 2, output: 8, cacheRead: 0.50},
	"o3-mini": {displayName: "o3-mini", input: 1.10, output: 4.40, cacheRead: 0.55},
	"o3-pro":  {displayName: "o3-pro", input: 20, output: 80},
	"o4-mini": {displayName: "o4-mini", input: 1.10, output: 4.40, cacheRead: 0.28},
	// OpenAI GPT-5.x models
	"gpt-5":               {displayName: "GPT-5", input: 1.25, output: 10, cacheRead: 0.125},
	"gpt-5-mini":          {displayName: "GPT-5 Mini", input: 0.25, output: 2, cacheRead: 0.025},
	"gpt-5-nano":          {displayName: "GPT-5 Nano", input: 0.05, output: 0.40, cacheRead: 0.005},
	"gpt-5-pro":           {displayName: "GPT-5 Pro", input: 15, output: 120},
	"gpt-5-chat-latest":   {displayName: "GPT-5 Chat (latest)", input: 1.25, output: 10},
	"gpt-5.1":             {displayName: "GPT-5.1", input: 1.25, output: 10, cacheRead: 0.13},
	"gpt-5.1-chat-latest": {displayName: "GPT-5.1 Chat", input: 1.25, output: 10, cacheRead: 0.125},
	"gpt-5.2":             {displayName: "GPT-5.2", input: 1.75, output: 14, cacheRead: 0.175},
	"gpt-5.2-chat-latest": {displayName: "GPT-5.2 Chat", input: 1.75, output: 14, cacheRead: 0.175},
	"gpt-5.2-pro":         {displayName: "GPT-5.2 Pro", input: 21, output: 168},
	"gpt-5.3-chat-latest": {displayName: "GPT-5.3 Chat (latest)", input: 1.75, output: 14, cacheRead: 0.175},
	"gpt-5.4":             {displayName: "GPT-5.4", input: 2.50, output: 15, cacheRead: 0.25},
	"gpt-5.4-mini":        {displayName: "GPT-5.4 mini", input: 0.75, output: 4.50, cacheRead: 0.075},
	"gpt-5.4-nano":        {displayName: "GPT-5.4 nano", input: 0.20, output: 1.25, cacheRead: 0.02},
	"gpt-5.4-pro":         {displayName: "GPT-5.4 Pro", input: 30, output: 180},
	"gpt-5.5":             {displayName: "GPT-5.5", input: 5, output: 30, cacheRead: 0.50},
	"gpt-5.5-pro":         {displayName: "GPT-5.5 Pro", input: 30, output: 180},
	"gpt-5.6-sol":         {displayName: "GPT-5.6 Sol", input: 5, output: 30, cacheRead: 0.50, cacheCreate: 6.25},
	// OpenAI Codex
	"gpt-5-codex":         {displayName: "GPT-5-Codex", input: 1.25, output: 10, cacheRead: 0.125},
	"gpt-5.1-codex":       {displayName: "GPT-5.1 Codex", input: 1.25, output: 10, cacheRead: 0.125},
	"gpt-5.1-codex-mini":  {displayName: "GPT-5.1 Codex mini", input: 0.25, output: 2, cacheRead: 0.025},
	"gpt-5.1-codex-max":   {displayName: "GPT-5.1 Codex Max", input: 1.25, output: 10, cacheRead: 0.125},
	"gpt-5.2-codex":       {displayName: "GPT-5.2 Codex", input: 1.75, output: 14, cacheRead: 0.175},
	"gpt-5.3-codex":       {displayName: "GPT-5.3 Codex", input: 1.75, output: 14, cacheRead: 0.175},
	"gpt-5.3-codex-spark": {displayName: "GPT-5.3 Codex Spark", input: 1.75, output: 14, cacheRead: 0.175},
	"codex-mini-latest":   {displayName: "Codex Mini", input: 1.50, output: 6, cacheRead: 0.375},
	// xAI Grok models
	"grok-4.20-multi-agent-0309": {displayName: "Grok 4.20 Multi-Agent", input: 1.25, output: 2.5, cacheRead: 0.20},
	"grok-4.20-0309-non-reasoning": {
		displayName: "Grok 4.20 (Non-Reasoning)",
		input:       1.25,
		output:      2.5,
		cacheRead:   0.20,
	},
	"grok-4.20-0309-reasoning": {
		displayName: "Grok 4.20 (Reasoning)",
		input:       1.25,
		output:      2.5,
		cacheRead:   0.20,
	},
	"grok-4.3":       {displayName: "Grok 4.3", input: 1.25, output: 2.5, cacheRead: 0.20},
	"grok-4.5":       {displayName: "Grok 4.5", input: 2, output: 6, cacheRead: 0.50},
	"grok-build-0.1": {displayName: "Grok Build 0.1", input: 1, output: 2, cacheRead: 0.20},
}

const (
	cursorAgentUnreportedModelID = "cursor-agent-unreported-model"
	grokAgentUnreportedModelID   = "grok-agent-unreported-model"
	codexUnreportedModelID       = "codex-unreported-model"
)

var modelDisplayNames = map[string]string{
	"grok-build":                 "Grok Build 0.1",
	"grok-composer-2.5-fast":     "Grok Composer 2.5",
	cursorAgentUnreportedModelID: "Cursor Agent",
	grokAgentUnreportedModelID:   "Grok Agent",
	codexUnreportedModelID:       "Codex",
}

func computeCost(modelID string, input, output, reasoning, cacheRead, cacheCreate int64) float64 {
	cost, _ := computeKnownCost(modelID, input, output, reasoning, cacheRead, cacheCreate)
	return cost
}

func computeKnownCost(modelID string, input, output, _ int64, cacheRead, cacheCreate int64) (float64, bool) {
	p, ok := currentPricing(modelID)
	if !ok {
		return 0, false
	}
	return float64(input)/1e6*p.input +
		float64(output)/1e6*p.output +
		float64(cacheRead)/1e6*p.cacheRead +
		float64(cacheCreate)/1e6*p.cacheCreate, true
}

func displayName(modelID string) string {
	if name := modelDisplayNames[modelID]; name != "" {
		return name
	}
	if p, ok := currentPricing(modelID); ok {
		return p.displayName
	}
	if strings.HasPrefix(modelID, "grok-") {
		parts := strings.Split(modelID, "-")
		for i, part := range parts {
			if part != "" {
				parts[i] = strings.ToUpper(part[:1]) + part[1:]
			}
		}
		return strings.Join(parts, " ")
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
	costUnknown  bool
	sessions     int
}

type projectAggEntry struct {
	totalTokens           int64
	inputTokens           int64
	outputTokens          int64
	reasoning             int64
	cacheRead             int64
	cacheCreate           int64
	cost                  float64
	costUnknown           bool
	totalTokensUnknown    bool
	tokenBreakdownUnknown bool
	sessions              int
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

	// Collect range-scoped agent usage from timestamped local data files.
	agentByDate := c.collectClaudeSessionStats(home)
	mergeDateAgg(agentByDate, c.collectGrokStats(home))
	mergeDateAgg(agentByDate, c.collectCursorAgentStats(home))

	// Collect Codex CLI data.
	codexDaily, codexModelsByDate, codexProjectsByDate := c.collectCodexStats(home)

	dailyMap := buildDailySessions(agentByDate, codexDaily)

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

		modelAgg := aggregateModelsByDate(agentByDate, fromDate, "9999-99-99")
		mergeModelAgg(modelAgg, aggregateCodexModelsByDate(codexModelsByDate, fromDate, "9999-99-99"))
		rd.Models = buildModelStats(modelAgg)
		rd.Tools = buildToolStats(aggregateToolsByDate(agentByDate, fromDate, "9999-99-99"))
		rd.Skills = buildSkillStats(aggregateSkillsByDate(agentByDate, fromDate, "9999-99-99"))
		rd.Projects = buildProjectStats(
			aggregateProjectsByDate(agentByDate, fromDate, "9999-99-99"),
			aggregateCodexProjectsByDate(codexProjectsByDate, fromDate, "9999-99-99"),
		)
		attachRangeTotals(rd)

		// Build per-day activity cells for this range.
		rd.Days = buildDayCells(agentByDate, codexModelsByDate, fromDate, "9999-99-99")
	}

	// Publish local history before the bounded network lookup so an unavailable
	// Codex endpoint never delays or removes the existing Stats experience.
	c.mu.Lock()
	c.cached = &StatsResponse{Type: "stats_data", Ranges: ranges}
	c.mu.Unlock()

	subscription := c.collectCodexSubscription(home)
	opencodeGoSubscription := c.collectOpenCodeGoSubscription(home)
	c.mu.Lock()
	c.cached = &StatsResponse{
		Type:                   "stats_data",
		Ranges:                 ranges,
		CodexSubscription:      subscription,
		OpenCodeGoSubscription: opencodeGoSubscription,
	}
	c.mu.Unlock()

	log.Printf("[stats] refresh complete: %d days of data", len(dailyMap))
}

// ── dailyEntry holds per-date aggregated data ──────────────

type dailyEntry struct {
	sessions int
}

type modelAggEntry struct {
	totalTokens           int64
	inputTokens           int64
	outputTokens          int64
	reasoning             int64
	cacheRead             int64
	cacheCreate           int64
	costUnknown           bool
	totalTokensUnknown    bool
	tokenBreakdownUnknown bool
	sessions              int
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
		cost, known := computeKnownCost(rec.modelID, rec.inputTokens, rec.outputTokens, rec.reasoning, rec.cacheRead, rec.cacheCreate)
		p.cost += cost
		if !known {
			p.costUnknown = true
		}
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

// ── Grok and Cursor Agent collection ───────────────────────

type grokStatsSummary struct {
	Info struct {
		ID             string `json:"id"`
		CWD            string `json:"cwd"`
		CurrentModelID string `json:"current_model_id"`
	} `json:"info"`
	CurrentModelID string `json:"current_model_id"`
	UpdatedAt      string `json:"updated_at"`
	CreatedAt      string `json:"created_at"`
}

func (c *Collector) collectGrokStats(home string) map[string]*dateAgg {
	byDate := make(map[string]*dateAgg)

	sessionsRoot := filepath.Join(home, ".grok", "sessions")
	cwdEntries, err := os.ReadDir(sessionsRoot)
	if err != nil {
		return byDate
	}

	for _, cwdEntry := range cwdEntries {
		if !cwdEntry.IsDir() {
			continue
		}
		cwdDir := filepath.Join(sessionsRoot, cwdEntry.Name())
		sessionEntries, err := os.ReadDir(cwdDir)
		if err != nil {
			continue
		}
		for _, sessionEntry := range sessionEntries {
			if !sessionEntry.IsDir() {
				continue
			}
			sessionDir := filepath.Join(cwdDir, sessionEntry.Name())
			summary, _ := readGrokStatsSummary(sessionDir)
			projectName := grokProjectName(summary, cwdEntry.Name())
			modelID := firstNonEmptyString(
				strings.TrimSpace(summary.CurrentModelID),
				strings.TrimSpace(summary.Info.CurrentModelID),
				readGrokHistoryModel(filepath.Join(sessionDir, "chat_history.jsonl")),
			)

			if scanGrokUpdates(filepath.Join(sessionDir, "updates.jsonl"), projectName, modelID, byDate) {
				continue
			}
			if date := grokFallbackSessionDate(summary, sessionDir); date != "" {
				agg := ensureDateAgg(byDate, date)
				sourceModelID := firstNonEmptyString(modelID, grokAgentUnreportedModelID)
				model := agg.models[sourceModelID]
				model.costUnknown = true
				model.totalTokensUnknown = true
				model.tokenBreakdownUnknown = true
				model.sessions++
				agg.models[sourceModelID] = model
				if projectName != "" {
					incrementProjectSession(agg, projectName)
					project := agg.projects[projectName]
					project.costUnknown = true
					project.totalTokensUnknown = true
					project.tokenBreakdownUnknown = true
				}
			}
		}
	}

	return byDate
}

func readGrokStatsSummary(sessionDir string) (grokStatsSummary, error) {
	data, err := os.ReadFile(filepath.Join(sessionDir, "summary.json"))
	if err != nil {
		return grokStatsSummary{}, err
	}
	var summary grokStatsSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return grokStatsSummary{}, err
	}
	return summary, nil
}

func grokProjectName(summary grokStatsSummary, encodedCWD string) string {
	if cwd := strings.TrimSpace(summary.Info.CWD); cwd != "" {
		if name := filepath.Base(filepath.Clean(cwd)); name != "." && name != string(filepath.Separator) {
			return name
		}
	}
	return decodeGrokProjectDir(encodedCWD)
}

func decodeGrokProjectDir(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(name); err == nil {
		name = decoded
	}
	if base := filepath.Base(filepath.Clean(name)); base != "." && base != string(filepath.Separator) {
		return base
	}
	return name
}

func readGrokHistoryModel(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var latest string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var record struct {
			ModelID string `json:"model_id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err == nil && strings.TrimSpace(record.ModelID) != "" {
			latest = strings.TrimSpace(record.ModelID)
		}
	}
	return latest
}

func scanGrokUpdates(path, projectName, fallbackModelID string, byDate map[string]*dateAgg) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	type updateMeta struct {
		TotalTokens      json.RawMessage `json:"totalTokens"`
		ModelID          string          `json:"modelId"`
		ModelIDSnake     string          `json:"model_id"`
		AgentTimestampMs json.RawMessage `json:"agentTimestampMs"`
	}
	type updateLine struct {
		Timestamp json.RawMessage `json:"timestamp"`
		Params    struct {
			Meta   updateMeta `json:"_meta"`
			Update struct {
				Meta updateMeta `json:"_meta"`
			} `json:"update"`
		} `json:"params"`
	}

	lastModelID := strings.TrimSpace(fallbackModelID)
	var previousTotal int64
	hasPrevious := false
	hasActivity := false
	seenModelDate := make(map[string]bool)
	seenProjectDate := make(map[string]bool)
	seenSlotDate := make(map[string]bool)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var line updateLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}

		if modelID := firstNonEmptyString(
			line.Params.Meta.ModelID,
			line.Params.Meta.ModelIDSnake,
			line.Params.Update.Meta.ModelID,
			line.Params.Update.Meta.ModelIDSnake,
		); modelID != "" {
			lastModelID = modelID
		}

		total, ok := firstRawInt64(line.Params.Meta.TotalTokens, line.Params.Update.Meta.TotalTokens)
		if !ok || total <= 0 {
			continue
		}
		timestamp, ok := unixTimestampFromRaw(line.Timestamp)
		if !ok {
			timestamp, ok = unixTimestampFromRaw(line.Params.Meta.AgentTimestampMs)
		}
		if !ok {
			continue
		}

		delta := total
		if hasPrevious {
			if total >= previousTotal {
				delta = total - previousTotal
			} else {
				delta = total
			}
		}
		previousTotal = total
		hasPrevious = true
		if delta <= 0 {
			continue
		}

		local := time.Unix(timestamp, 0).In(time.Local)
		date := local.Format("2006-01-02")
		slot := local.Hour() / 6
		if slot > 3 {
			slot = 3
		}
		modelID := firstNonEmptyString(lastModelID, fallbackModelID, grokAgentUnreportedModelID)

		agg := ensureDateAgg(byDate, date)
		m := agg.models[modelID]
		m.totalTokens += delta
		m.costUnknown = true
		m.tokenBreakdownUnknown = true
		modelDateKey := date + "\x00" + modelID
		if !seenModelDate[modelDateKey] {
			m.sessions++
			seenModelDate[modelDateKey] = true
		}
		agg.models[modelID] = m

		if projectName != "" {
			p := ensureProjectAgg(agg, projectName)
			p.totalTokens += delta
			p.costUnknown = true
			p.tokenBreakdownUnknown = true
			projectDateKey := date + "\x00" + projectName
			if !seenProjectDate[projectDateKey] {
				p.sessions++
				seenProjectDate[projectDateKey] = true
			}
		}

		agg.slots[slot].totalTokens += delta
		agg.slots[slot].costUnknown = true
		slotDateKey := fmt.Sprintf("%s\x00%d", date, slot)
		if !seenSlotDate[slotDateKey] {
			agg.slots[slot].sessions++
			seenSlotDate[slotDateKey] = true
		}
		hasActivity = true
	}
	return hasActivity
}

func grokFallbackSessionDate(summary grokStatsSummary, sessionDir string) string {
	for _, value := range []string{summary.UpdatedAt, summary.CreatedAt} {
		if date := dateFromTimestamp(value); date != "" {
			return date
		}
	}
	for _, name := range []string{"updates.jsonl", "chat_history.jsonl", "summary.json"} {
		if info, err := os.Stat(filepath.Join(sessionDir, name)); err == nil {
			return info.ModTime().In(time.Local).Format("2006-01-02")
		}
	}
	if info, err := os.Stat(sessionDir); err == nil {
		return info.ModTime().In(time.Local).Format("2006-01-02")
	}
	return ""
}

func (c *Collector) collectCursorAgentStats(home string) map[string]*dateAgg {
	byDate := make(map[string]*dateAgg)

	projectsRoot := filepath.Join(home, ".cursor", "projects")
	err := filepath.WalkDir(projectsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		rel, err := filepath.Rel(projectsRoot, path)
		if err != nil {
			return nil
		}
		parts := strings.Split(rel, string(os.PathSeparator))
		if len(parts) != 4 || parts[1] != "agent-transcripts" {
			return nil
		}
		sessionID := parts[2]
		if sessionID == "" || d.Name() != sessionID+".jsonl" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}

		projectName := decodeCursorProjectDir(parts[0])
		if projectName == "" {
			return nil
		}
		date := info.ModTime().In(time.Local).Format("2006-01-02")
		agg := ensureDateAgg(byDate, date)
		incrementProjectSession(agg, projectName)
		project := agg.projects[projectName]
		project.costUnknown = true
		project.totalTokensUnknown = true
		project.tokenBreakdownUnknown = true
		model := agg.models[cursorAgentUnreportedModelID]
		model.costUnknown = true
		model.totalTokensUnknown = true
		model.tokenBreakdownUnknown = true
		model.sessions++
		agg.models[cursorAgentUnreportedModelID] = model
		return nil
	})
	if err != nil {
		return byDate
	}
	return byDate
}

func decodeCursorProjectDir(name string) string {
	for _, part := range strings.Split(strings.Trim(name, "-"), "-") {
		if strings.TrimSpace(part) != "" {
			name = part
		}
	}
	return strings.TrimSpace(name)
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
		"SELECT id, cwd, model, rollout_path FROM threads WHERE tokens_used > 0").Output()
	if err != nil {
		log.Printf("[stats] sqlite3 query failed: %v", err)
		return daily, modelsByDate, projectsByDate
	}

	var threads []struct {
		ID          string `json:"id"`
		Cwd         string `json:"cwd"`
		Model       string `json:"model"`
		RolloutPath string `json:"rollout_path"`
	}
	if err := json.Unmarshal(out, &threads); err != nil {
		log.Printf("[stats] failed to parse sqlite3 output: %v", err)
		return daily, modelsByDate, projectsByDate
	}

	skipped := 0
	var skippedExamples []string
	for _, t := range threads {
		modelID := t.Model
		if modelID == "" {
			modelID = codexUnreportedModelID
		}
		projectName := filepath.Base(t.Cwd)
		if projectName == "." || projectName == "/" {
			projectName = ""
		}

		usageByDate, err := readCodexUsageByDate(t.RolloutPath, time.Local)
		if err != nil || len(usageByDate) == 0 {
			skipped++
			if len(skippedExamples) < 3 {
				reason := "no usage by date"
				if err != nil {
					reason = err.Error()
				}
				threadID := t.ID
				if threadID == "" {
					threadID = t.RolloutPath
				}
				skippedExamples = append(skippedExamples, fmt.Sprintf("%s (%s)", threadID, reason))
			}
			continue
		}

		for date, usage := range usageByDate {
			if !usage.hasTokens() {
				continue
			}

			d := daily[date]
			d.sessions++
			d.totalTokens += usage.totalTokens
			d.inputTokens += usage.inputTokens
			d.outputTokens += usage.outputTokens
			d.reasoningTokens += usage.reasoningTokens
			d.cacheRead += usage.cacheRead
			daily[date] = d

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

			if projectName == "" {
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
			cost, known := computeKnownCost(modelID, usage.inputTokens, usage.outputTokens, usage.reasoningTokens, usage.cacheRead, 0)
			p.cost += cost
			if !known {
				p.costUnknown = true
			}
			p.sessions++
		}
	}
	if skipped > 0 {
		detail := ""
		if len(skippedExamples) > 0 {
			detail = ": " + strings.Join(skippedExamples, "; ")
		}
		log.Printf("[stats] skipped %d Codex threads without parsable token_count rollout%s", skipped, detail)
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

func (u codexUsage) hasTokens() bool {
	return u.totalTokens > 0 ||
		u.inputTokens > 0 ||
		u.outputTokens > 0 ||
		u.reasoningTokens > 0 ||
		u.cacheRead > 0
}

func readCodexUsage(path string) (codexUsage, error) {
	byDate, err := readCodexUsageByDate(path, time.Local)
	if err != nil {
		return codexUsage{}, err
	}

	var usage codexUsage
	for _, bucket := range byDate {
		usage = addCodexUsage(usage, bucket)
	}
	if !usage.hasTokens() {
		return codexUsage{}, fmt.Errorf("no token_count event found")
	}
	return usage, nil
}

func readCodexUsageByDate(path string, loc *time.Location) (map[string]codexUsage, error) {
	if path == "" {
		return nil, fmt.Errorf("empty rollout path")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	type tokenCountLine struct {
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Payload   struct {
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

	byDate := make(map[string]codexUsage)
	var previous *codexUsage
	found := false

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
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

		date, _, ok := localDateHourFromTimestamp(line.Timestamp, loc)
		if !ok || date == "" {
			continue
		}

		current := codexUsageFromTotals(line.Payload.Info.TotalTokenUsage)
		if !current.hasTokens() {
			continue
		}

		delta := current
		if previous != nil {
			delta = diffCodexUsage(current, *previous)
		}
		if !delta.hasTokens() {
			previous = &current
			continue
		}

		byDate[date] = addCodexUsage(byDate[date], delta)
		previous = &current
		found = true
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("no token_count event found")
	}
	return byDate, nil
}

func codexUsageFromTotals(total *struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
}) codexUsage {
	if total == nil {
		return codexUsage{}
	}

	uncachedInput := total.InputTokens - total.CachedInputTokens
	if uncachedInput < 0 {
		uncachedInput = total.InputTokens
	}

	totalTokens := total.TotalTokens
	if totalTokens <= 0 {
		totalTokens = total.InputTokens + total.OutputTokens
	}

	return codexUsage{
		totalTokens:     totalTokens,
		inputTokens:     uncachedInput,
		outputTokens:    total.OutputTokens,
		reasoningTokens: total.ReasoningOutputTokens,
		cacheRead:       total.CachedInputTokens,
	}
}

func addCodexUsage(dst, src codexUsage) codexUsage {
	dst.totalTokens += src.totalTokens
	dst.inputTokens += src.inputTokens
	dst.outputTokens += src.outputTokens
	dst.reasoningTokens += src.reasoningTokens
	dst.cacheRead += src.cacheRead
	return dst
}

func diffCodexUsage(current, previous codexUsage) codexUsage {
	// Token totals should normally be monotonic. If they reset, treat the new
	// snapshot as a fresh baseline instead of producing negative deltas.
	if current.totalTokens < previous.totalTokens ||
		current.inputTokens < previous.inputTokens ||
		current.outputTokens < previous.outputTokens ||
		current.reasoningTokens < previous.reasoningTokens ||
		current.cacheRead < previous.cacheRead {
		return current
	}

	return codexUsage{
		totalTokens:     current.totalTokens - previous.totalTokens,
		inputTokens:     current.inputTokens - previous.inputTokens,
		outputTokens:    current.outputTokens - previous.outputTokens,
		reasoningTokens: current.reasoningTokens - previous.reasoningTokens,
		cacheRead:       current.cacheRead - previous.cacheRead,
	}
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
		cost, known := computeKnownCost(modelID, m.inputTokens, m.outputTokens, m.reasoning, m.cacheRead, m.cacheCreate)
		costKnown := known && !m.costUnknown
		result = append(result, ModelStat{
			Name:                displayName(modelID),
			TotalTokens:         m.totalTokens,
			TotalTokensKnown:    !m.totalTokensUnknown,
			InputTokens:         m.inputTokens,
			OutputTokens:        m.outputTokens,
			ReasoningTokens:     m.reasoning,
			CacheRead:           m.cacheRead,
			CacheCreate:         m.cacheCreate,
			TokenBreakdownKnown: !m.tokenBreakdownUnknown,
			Cost:                cost,
			CostKnown:           costKnown,
			Sessions:            m.sessions,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CostKnown != result[j].CostKnown {
			return result[i].CostKnown
		}
		if result[i].Cost == result[j].Cost {
			if result[i].TotalTokens == result[j].TotalTokens {
				return result[i].Sessions > result[j].Sessions
			}
			return result[i].TotalTokens > result[j].TotalTokens
		}
		return result[i].Cost > result[j].Cost
	})
	return result
}

func buildProjectStats(projectAgg map[string]*projectAggEntry, extra ...map[string]*projectAggEntry) []ProjectStat {
	for _, more := range extra {
		mergeProjectAgg(projectAgg, more)
	}

	var result []ProjectStat
	for name, p := range projectAgg {
		result = append(result, ProjectStat{
			Name:                name,
			TotalTokens:         p.totalTokens,
			TotalTokensKnown:    !p.totalTokensUnknown,
			InputTokens:         p.inputTokens,
			OutputTokens:        p.outputTokens,
			ReasoningTokens:     p.reasoning,
			CacheRead:           p.cacheRead,
			CacheCreate:         p.cacheCreate,
			TokenBreakdownKnown: !p.tokenBreakdownUnknown,
			Cost:                p.cost,
			CostKnown:           !p.costUnknown,
			Sessions:            p.sessions,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CostKnown != result[j].CostKnown {
			return result[i].CostKnown
		}
		if result[i].Cost == result[j].Cost {
			if result[i].TotalTokens == result[j].TotalTokens {
				return result[i].Sessions > result[j].Sessions
			}
			return result[i].TotalTokens > result[j].TotalTokens
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
	costKnown := true
	totalTokensKnown := true
	tokenBreakdownKnown := true
	for _, m := range rd.Models {
		totalCost += m.Cost
		totalTokens += m.TotalTokens
		totalInput += m.InputTokens
		totalOutput += m.OutputTokens
		totalReasoning += m.ReasoningTokens
		totalCacheRead += m.CacheRead
		totalCacheCreate += m.CacheCreate
		if !m.CostKnown {
			costKnown = false
		}
		if !m.TotalTokensKnown {
			totalTokensKnown = false
		}
		if !m.TokenBreakdownKnown {
			tokenBreakdownKnown = false
		}
	}
	for _, p := range rd.Projects {
		if !p.CostKnown {
			costKnown = false
		}
		if !p.TotalTokensKnown {
			totalTokensKnown = false
		}
		if !p.TokenBreakdownKnown {
			tokenBreakdownKnown = false
		}
	}
	rd.Cost = totalCost
	rd.CostKnown = costKnown
	rd.TotalTokens = totalTokens
	rd.TotalTokensKnown = totalTokensKnown
	rd.InputTokens = totalInput
	rd.OutputTokens = totalOutput
	rd.ReasoningTokens = totalReasoning
	rd.CacheRead = totalCacheRead
	rd.CacheCreate = totalCacheCreate
	rd.TokenBreakdownKnown = tokenBreakdownKnown
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
			dc = &DayCell{Date: date, CostKnown: true, TotalTokensKnown: true, TokenBreakdownKnown: true}
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
			cost, known := computeKnownCost(modelID, m.inputTokens, m.outputTokens, m.reasoning, m.cacheRead, m.cacheCreate)
			dc.Cost += cost
			if !known || m.costUnknown {
				dc.CostKnown = false
			}
			if m.totalTokensUnknown {
				dc.TotalTokensKnown = false
			}
			if m.tokenBreakdownUnknown {
				dc.TokenBreakdownKnown = false
			}
		}
		if projectSessions := projectSessionCount(agg.projects); projectSessions > dc.Sessions {
			dc.Sessions = projectSessions
		}
		for _, project := range agg.projects {
			if project != nil {
				if project.costUnknown {
					dc.CostKnown = false
				}
				if project.totalTokensUnknown {
					dc.TotalTokensKnown = false
				}
				if project.tokenBreakdownUnknown {
					dc.TokenBreakdownKnown = false
				}
			}
		}
	}

	// Codex data
	for date, models := range codexModelsByDate {
		if date < fromDate || date > toDate {
			continue
		}
		dc := dayCosts[date]
		if dc == nil {
			dc = &DayCell{Date: date, CostKnown: true, TotalTokensKnown: true, TokenBreakdownKnown: true}
			dayCosts[date] = dc
		}
		for modelID, m := range models {
			dc.TotalTokens += m.totalTokens
			dc.InputTokens += m.inputTokens
			dc.OutputTokens += m.outputTokens
			dc.ReasoningTokens += m.reasoning
			dc.CacheRead += m.cacheRead
			dc.Sessions += m.sessions
			cost, known := computeKnownCost(modelID, m.inputTokens, m.outputTokens, m.reasoning, m.cacheRead, m.cacheCreate)
			dc.Cost += cost
			if !known || m.costUnknown {
				dc.CostKnown = false
			}
			if m.totalTokensUnknown {
				dc.TotalTokensKnown = false
			}
			if m.tokenBreakdownUnknown {
				dc.TokenBreakdownKnown = false
			}
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
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
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

func ensureProjectAgg(agg *dateAgg, projectName string) *projectAggEntry {
	if agg.projects == nil {
		agg.projects = make(map[string]*projectAggEntry)
	}
	p := agg.projects[projectName]
	if p == nil {
		p = &projectAggEntry{}
		agg.projects[projectName] = p
	}
	return p
}

func incrementProjectSession(agg *dateAgg, projectName string) {
	if projectName == "" {
		return
	}
	ensureProjectAgg(agg, projectName).sessions++
}

// hourFromTimestamp extracts the local hour (0-23) from an ISO 8601 timestamp.
// Returns 12 as a safe default if parsing fails.
func hourFromTimestamp(ts string) int {
	_, hour, ok := localDateHourFromTimestamp(ts, time.Local)
	if ok {
		return hour
	}
	return fallbackHourFromTimestamp(ts)
}

func dateFromTimestamp(ts string) string {
	date, _, ok := localDateHourFromTimestamp(ts, time.Local)
	if ok {
		return date
	}
	return fallbackDateFromTimestamp(ts)
}

func dateFromUnixTimestamp(sec int64) string {
	return localDateFromUnixTimestamp(sec, time.Local)
}

func localDateHourFromTimestamp(ts string, loc *time.Location) (string, int, bool) {
	if loc == nil {
		loc = time.Local
	}

	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(ts))
	if err != nil {
		return "", 12, false
	}

	local := parsed.In(loc)
	return local.Format("2006-01-02"), local.Hour(), true
}

func localDateFromUnixTimestamp(sec int64, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	return time.Unix(sec, 0).In(loc).Format("2006-01-02")
}

func unixTimestampFromRaw(raw json.RawMessage) (int64, bool) {
	value, ok := rawFloat64(raw)
	if !ok {
		return 0, false
	}
	if value > 1e14 {
		value = value / 1e9
	} else if value > 1e11 {
		value = value / 1e3
	}
	if value <= 0 {
		return 0, false
	}
	return int64(value), true
}

func firstRawInt64(values ...json.RawMessage) (int64, bool) {
	for _, raw := range values {
		value, ok := rawFloat64(raw)
		if !ok {
			continue
		}
		return int64(value), true
	}
	return 0, false
}

func rawFloat64(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 || strings.EqualFold(strings.TrimSpace(string(raw)), "null") {
		return 0, false
	}
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil {
		return number, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return 0, false
	}
	if parsed, err := strconv.ParseFloat(text, 64); err == nil {
		return parsed, true
	}
	if parsed, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return float64(parsed.Unix()), true
	}
	return 0, false
}

func fallbackHourFromTimestamp(ts string) int {
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

func fallbackDateFromTimestamp(ts string) string {
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
		current.costUnknown = current.costUnknown || item.costUnknown
		current.totalTokensUnknown = current.totalTokensUnknown || item.totalTokensUnknown
		current.tokenBreakdownUnknown = current.tokenBreakdownUnknown || item.tokenBreakdownUnknown
		current.sessions += item.sessions
		dst[modelID] = current
	}
}

func mergeDateAgg(dst, src map[string]*dateAgg) {
	for date, srcAgg := range src {
		if srcAgg == nil {
			continue
		}
		dstAgg := ensureDateAgg(dst, date)
		mergeModelAgg(dstAgg.models, srcAgg.models)
		for name, calls := range srcAgg.tools {
			dstAgg.tools[name] += calls
		}
		for name, skill := range srcAgg.skills {
			current := dstAgg.skills[name]
			if current == nil {
				current = &skillEntry{projects: make(map[string]bool)}
				dstAgg.skills[name] = current
			}
			current.calls += skill.calls
			for project := range skill.projects {
				current.projects[project] = true
			}
		}
		mergeProjectAgg(dstAgg.projects, srcAgg.projects)
		for i := range dstAgg.slots {
			dstAgg.slots[i].totalTokens += srcAgg.slots[i].totalTokens
			dstAgg.slots[i].inputTokens += srcAgg.slots[i].inputTokens
			dstAgg.slots[i].outputTokens += srcAgg.slots[i].outputTokens
			dstAgg.slots[i].reasoning += srcAgg.slots[i].reasoning
			dstAgg.slots[i].cacheRead += srcAgg.slots[i].cacheRead
			dstAgg.slots[i].cacheCreate += srcAgg.slots[i].cacheCreate
			dstAgg.slots[i].costUnknown = dstAgg.slots[i].costUnknown || srcAgg.slots[i].costUnknown
			dstAgg.slots[i].sessions += srcAgg.slots[i].sessions
		}
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
		current.costUnknown = current.costUnknown || item.costUnknown
		current.totalTokensUnknown = current.totalTokensUnknown || item.totalTokensUnknown
		current.tokenBreakdownUnknown = current.tokenBreakdownUnknown || item.tokenBreakdownUnknown
		current.sessions += item.sessions
	}
}

func projectSessionCount(projects map[string]*projectAggEntry) int {
	total := 0
	for _, project := range projects {
		if project != nil {
			total += project.sessions
		}
	}
	return total
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
