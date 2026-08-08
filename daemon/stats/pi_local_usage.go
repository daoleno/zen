package stats

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Pi model usage is collected entirely from Pi's durable local session
// ledgers: Pi's shared per-CWD history
// (~/.pi/agent/sessions/<encoded-cwd>/<timestamp>_<uuid>.jsonl) and Zen's
// explicitly bound session files (~/.zen/provider-sessions/pi/<uuid>.jsonl).
// Pi records the authoritative per-turn Usage on assistant messages (input,
// output, cache-read, cache-write, totalTokens, an optional reasoning
// subset of output, and the exact observed cost when the provider is
// priced), plus optional Usage on compaction and branch_summary entries
// (summary-generation LLM calls) and toolResult messages (nested LLM work).
// This collector reads only those structured metadata fields: prompt,
// response, reasoning and tool bodies are never read, logged or persisted.
//
// Counting follows Pi's own durable-session model: the JSONL file is a
// billing ledger, so every distinct entry that carries usage counts once,
// including abandoned branches, retries and compacted-away history (each
// was billed). Duplicate snapshots of the same entry (same entry id written
// twice by an interrupted append) count once. Model identity is the
// recorded message.model, never rewritten through an allowlist or a generic
// label; compaction/branch_summary/toolResult usage has no recorded model,
// so it is attributed to the model in effect at that ledger position
// (tracked from assistant messages and model_change entries), and skipped
// when the ledger never recorded a model. Missing directories, malformed or
// partially written lines, and unknown models fail soft without affecting
// other collectors.

// piUsage mirrors Pi's authoritative Usage record. reasoning is a subset of
// output reported by some providers; cost is exact observed cost and stays
// all-zero when the provider is not priced.
type piUsage struct {
	Input       int64      `json:"input"`
	Output      int64      `json:"output"`
	CacheRead   int64      `json:"cacheRead"`
	CacheWrite  int64      `json:"cacheWrite"`
	Reasoning   *int64     `json:"reasoning"`
	TotalTokens int64      `json:"totalTokens"`
	Cost        piCostInfo `json:"cost"`
}

type piCostInfo struct {
	Total float64 `json:"total"`
}

// piSessionMessage is the message payload of a "message" session entry. Only
// the aggregated usage fields are decoded; content bodies are not.
type piSessionMessage struct {
	Role  string   `json:"role"`
	Model string   `json:"model"`
	Usage *piUsage `json:"usage"`
}

// piSessionLine decodes exactly the structured metadata of one Pi session
// file line. The "session" header carries the authoritative cwd; "message"
// entries carry an AgentMessage in "message"; "compaction" and
// "branch_summary" entries carry "usage" at top level; "model_change"
// entries carry provider/modelId at top level.
type piSessionLine struct {
	Type      string            `json:"type"`
	ID        string            `json:"id"`
	Timestamp string            `json:"timestamp"`
	Cwd       string            `json:"cwd"`
	ModelID   string            `json:"modelId"`
	Message   *piSessionMessage `json:"message"`
	Usage     *piUsage          `json:"usage"`
}

// piLedgerRecord is one counted, deduplicated billed usage record.
type piLedgerRecord struct {
	date         string
	hour         int // 0-23, for heatmap slot bucketing
	modelID      string
	totalTokens  int64
	inputTokens  int64
	outputTokens int64
	reasoning    int64
	cacheRead    int64
	cacheWrite   int64
	cost         float64
	costObserved bool
}

func (u *piUsage) hasTokens() bool {
	return u != nil &&
		(u.Input > 0 || u.Output > 0 || u.CacheRead > 0 || u.CacheWrite > 0 || u.TotalTokens > 0)
}

func (u *piUsage) reasoningTokens() int64 {
	if u.Reasoning != nil && *u.Reasoning > 0 {
		return *u.Reasoning
	}
	return 0
}

// collectPiStats scans Pi's local session ledger and aggregates the recorded
// usage per date. The ledger is only ever read; live concurrent appends show
// up as a partial final line that is skipped until completed.
func (c *Collector) collectPiStats(home string) map[string]*dateAgg {
	byDate := make(map[string]*dateAgg)

	sessionsRoot := filepath.Join(home, ".pi", "agent", "sessions")
	projectDirs, err := os.ReadDir(sessionsRoot)
	if err == nil {
		for _, projectDir := range projectDirs {
			if !projectDir.IsDir() {
				continue
			}
			dirPath := filepath.Join(sessionsRoot, projectDir.Name())
			scanPiSessionDir(dirPath, decodePiProjectDir(projectDir.Name()), byDate)
		}
	}

	// Zen launches Pi with an absolute --session path so each Session has one
	// stable transcript owner. Those files are deliberately outside Pi's
	// shared per-CWD history and use the same durable JSONL schema.
	ownedSessionsRoot := filepath.Join(home, ".zen", "provider-sessions", "pi")
	scanPiSessionDir(ownedSessionsRoot, "", byDate)

	return byDate
}

func scanPiSessionDir(dirPath, fallbackProject string, byDate map[string]*dateAgg) {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
			continue
		}
		scanPiSessionFile(filepath.Join(dirPath, file.Name()), fallbackProject, byDate)
	}
}

// scanPiSessionFile reads one Pi session JSONL ledger. Every distinct entry
// with usage is counted once (dedupe by entry id); malformed or partially
// written lines are skipped without aborting the scan. Usage is attributed
// to the entry's own local date/hour, the file's project, and the recorded
// model (or the model in effect for entries that carry usage but no model).
func scanPiSessionFile(path, fallbackProject string, byDate map[string]*dateAgg) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	headerCwd := ""
	inEffectModel := ""
	seenIDs := make(map[string]bool)
	var records []piLedgerRecord

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		var line piSessionLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			// Malformed or partially written line (live append, interrupted
			// write): skip it and keep scanning.
			continue
		}

		switch line.Type {
		case "session":
			headerCwd = line.Cwd
		case "model_change":
			if modelID := strings.TrimSpace(line.ModelID); modelID != "" {
				inEffectModel = modelID
			}
		case "message":
			msg := line.Message
			if msg == nil || msg.Usage == nil || !msg.Usage.hasTokens() {
				continue
			}
			modelID := ""
			switch msg.Role {
			case "assistant":
				// The authoritative record: the billed assistant turn carries
				// its own recorded model identity.
				modelID = strings.TrimSpace(msg.Model)
				if modelID != "" {
					inEffectModel = modelID
				}
			case "toolResult":
				// Nested LLM work performed by the tool, when recorded; Pi
				// totals include it, so the ledger model in effect is used.
				modelID = inEffectModel
			default:
				continue
			}
			if modelID == "" {
				continue
			}
			if line.ID != "" {
				if seenIDs[line.ID] {
					continue // duplicate snapshot of the same entry
				}
				seenIDs[line.ID] = true
			}
			record, ok := piRecordFromUsage(line.Timestamp, modelID, msg.Usage)
			if ok {
				records = append(records, record)
			}
		case "compaction", "branch_summary":
			// Summary-generation LLM usage has no recorded model; it is
			// attributed to the model in effect at this ledger position. A
			// ledger that never recorded a model cannot attribute it.
			if line.Usage == nil || !line.Usage.hasTokens() || inEffectModel == "" {
				continue
			}
			if line.ID != "" {
				if seenIDs[line.ID] {
					continue
				}
				seenIDs[line.ID] = true
			}
			record, ok := piRecordFromUsage(line.Timestamp, inEffectModel, line.Usage)
			if ok {
				records = append(records, record)
			}
		}
	}

	if len(records) == 0 {
		return
	}

	projectName := piProjectName(headerCwd, fallbackProject)
	seenModelDate := make(map[string]bool)
	seenProjectDate := make(map[string]bool)

	for _, rec := range records {
		agg := ensureDateAgg(byDate, rec.date)

		// Per-model aggregation. The exact observed cost travels as the
		// recorded component with its own token bucket, so price estimates
		// cover exactly the tokens of unrecorded sources when a model is
		// shared (OpenCode contract).
		model := agg.models[rec.modelID]
		model.totalTokens += rec.totalTokens
		model.inputTokens += rec.inputTokens
		model.outputTokens += rec.outputTokens
		model.reasoning += rec.reasoning
		model.cacheRead += rec.cacheRead
		model.cacheCreate += rec.cacheWrite
		if model.recorded == nil {
			model.recorded = &modelRecordedCost{}
		}
		model.recorded.cost += rec.cost
		model.recorded.input += rec.inputTokens
		model.recorded.output += rec.outputTokens
		model.recorded.reasoning += rec.reasoning
		model.recorded.cacheRead += rec.cacheRead
		model.recorded.cacheCreate += rec.cacheWrite
		if !rec.costObserved {
			model.costUnknown = true
		}
		modelDateKey := rec.date + "\x00" + rec.modelID
		if !seenModelDate[modelDateKey] {
			model.sessions++
			seenModelDate[modelDateKey] = true
		}
		agg.models[rec.modelID] = model

		// Time-of-day slot aggregation.
		slot := rec.hour / 6
		if slot > 3 {
			slot = 3
		}
		agg.slots[slot].totalTokens += rec.totalTokens
		agg.slots[slot].inputTokens += rec.inputTokens
		agg.slots[slot].outputTokens += rec.outputTokens
		agg.slots[slot].reasoning += rec.reasoning
		agg.slots[slot].cacheRead += rec.cacheRead
		agg.slots[slot].cacheCreate += rec.cacheWrite
		agg.slots[slot].sessions++

		// Per-project aggregation from the session header cwd.
		if projectName == "" {
			continue
		}
		project := ensureProjectAgg(agg, projectName)
		project.totalTokens += rec.totalTokens
		project.inputTokens += rec.inputTokens
		project.outputTokens += rec.outputTokens
		project.reasoning += rec.reasoning
		project.cacheRead += rec.cacheRead
		project.cacheCreate += rec.cacheWrite
		project.cost += rec.cost
		if !rec.costObserved {
			project.costUnknown = true
		}
		projectDateKey := rec.date + "\x00" + projectName
		if !seenProjectDate[projectDateKey] {
			project.sessions++
			seenProjectDate[projectDateKey] = true
		}
	}
}

// piRecordFromUsage converts a counted Pi usage object into a ledger record
// attributed to the entry's local date and hour. Records without tokens
// (aborted/error turns, zero-usage summaries) contribute nothing.
func piRecordFromUsage(ts, modelID string, u *piUsage) (piLedgerRecord, bool) {
	date := dateFromTimestamp(ts)
	if date == "" {
		return piLedgerRecord{}, false
	}
	total := u.TotalTokens
	if total <= 0 {
		total = u.Input + u.Output + u.CacheRead + u.CacheWrite
	}
	if total <= 0 {
		return piLedgerRecord{}, false
	}
	return piLedgerRecord{
		date:         date,
		hour:         hourFromTimestamp(ts),
		modelID:      modelID,
		totalTokens:  total,
		inputTokens:  u.Input,
		outputTokens: u.Output,
		reasoning:    u.reasoningTokens(),
		cacheRead:    u.CacheRead,
		cacheWrite:   u.CacheWrite,
		cost:         u.Cost.Total,
		costObserved: u.Cost.Total > 0,
	}, true
}

// piProjectName attributes a session to the base of the header cwd (Pi's
// authoritative per-session working directory). Only when the header is
// missing does it fall back to decoding the encoded session directory name.
func piProjectName(headerCwd, fallbackProject string) string {
	if cwd := strings.TrimSpace(headerCwd); cwd != "" {
		if name := filepath.Base(filepath.Clean(cwd)); name != "." && name != string(filepath.Separator) {
			return name
		}
	}
	return fallbackProject
}

// decodePiProjectDir converts the encoded session directory name
// "--home-user-workspace-zen--" (cwd with "/" replaced by "-", wrapped in
// "--") back to the last path component, mirroring Pi's documented layout
// on every platform (the replaced path uses "/" separators).
func decodePiProjectDir(name string) string {
	trimmed := strings.Trim(name, "-")
	if trimmed == "" {
		return ""
	}
	path := strings.ReplaceAll(trimmed, "-", "/")
	if base := filepath.Base(filepath.Clean(path)); base != "." && base != "/" && base != string(filepath.Separator) {
		return base
	}
	return ""
}
