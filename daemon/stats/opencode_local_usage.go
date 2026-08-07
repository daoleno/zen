package stats

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// OpenCode model usage is collected entirely from the OpenCode CLI's local
// structured database ($XDG_DATA_HOME/opencode/opencode.db, or the platform
// equivalent). There is no subscription probing, no credential read, and no
// upstream API access: the collector reads only the observed usage fields the
// CLI already records per assistant message (model, token counts, cost,
// timestamp, working directory) and aggregates exactly those facts. Rows
// without usage are ignored, unavailable metrics are omitted, and quota or
// subscription state is never inferred.

// openCodeDBPath resolves the OpenCode CLI data directory: $XDG_DATA_HOME on
// Linux, the Library Application Support dir on macOS, and LOCALAPPDATA on
// Windows, mirroring the official CLI's platform layout.
func openCodeDBPath(home string) string {
	var dataDir string
	switch {
	case strings.TrimSpace(os.Getenv("XDG_DATA_HOME")) != "":
		dataDir = filepath.Join(os.Getenv("XDG_DATA_HOME"), "opencode")
	case runtime.GOOS == "darwin":
		dataDir = filepath.Join(home, "Library", "Application Support", "opencode")
	case runtime.GOOS == "windows":
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			dataDir = filepath.Join(local, "opencode")
		} else {
			dataDir = filepath.Join(home, "AppData", "Local", "opencode")
		}
	default:
		dataDir = filepath.Join(home, ".local", "share", "opencode")
	}
	return filepath.Join(dataDir, "opencode.db")
}

// openCodeUsageQuery projects the observed usage fields from the OpenCode
// message table. The message data column is JSON metadata (the conversation
// text lives in the part table and is never read); json_extract keeps the
// read bounded to exactly the aggregated fields. json_valid guards keep one
// malformed row from failing the whole read.
const openCodeUsageQuery = `SELECT
	CASE WHEN json_valid(data) THEN json_extract(data, '$.role') END AS role,
	CASE WHEN json_valid(data) THEN json_extract(data, '$.modelID') END AS model,
	CASE WHEN json_valid(data) THEN json_extract(data, '$.cost') END AS cost,
	CASE WHEN json_valid(data) THEN json_extract(data, '$.tokens.input') END AS tin,
	CASE WHEN json_valid(data) THEN json_extract(data, '$.tokens.output') END AS tout,
	CASE WHEN json_valid(data) THEN json_extract(data, '$.tokens.reasoning') END AS trea,
	CASE WHEN json_valid(data) THEN json_extract(data, '$.tokens.cache.read') END AS tcr,
	CASE WHEN json_valid(data) THEN json_extract(data, '$.tokens.cache.write') END AS tcw,
	CASE WHEN json_valid(data) THEN json_extract(data, '$.time.created') END AS created,
	CASE WHEN json_valid(data) THEN json_extract(data, '$.path.cwd') END AS cwd
FROM message;`

// openCodeMessageUsageRow mirrors one projected message row.
type openCodeMessageUsageRow struct {
	Role    json.RawMessage `json:"role"`
	Model   json.RawMessage `json:"model"`
	Cost    json.RawMessage `json:"cost"`
	Tin     json.RawMessage `json:"tin"`
	Tout    json.RawMessage `json:"tout"`
	Trea    json.RawMessage `json:"trea"`
	Tcr     json.RawMessage `json:"tcr"`
	Tcw     json.RawMessage `json:"tcw"`
	Created json.RawMessage `json:"created"`
	Cwd     json.RawMessage `json:"cwd"`
}

// collectOpenCodeStats reads OpenCode usage from the local structured
// database and aggregates the observed facts per date: assistant-message
// (request) counts, input/output/reasoning/cache tokens, and cost when those
// exact fields exist. Each usage row counts as one request; rows without any
// token usage contribute nothing; a row without a cost field keeps the
// model's cost unknown rather than inventing one. Rows are aggregated by the
// (provider, model) pair recorded in the message; the shared model map is
// keyed by model ID exactly like every other collector, so a model served by
// multiple providers sums its observed facts. The database is opened
// read-only so the live CLI is never disturbed.
func (c *Collector) collectOpenCodeStats(home string) map[string]*dateAgg {
	byDate := make(map[string]*dateAgg)

	dbPath := openCodeDBPath(home)
	if _, err := os.Stat(dbPath); err != nil {
		return byDate
	}

	sqlite3, err := exec.LookPath("sqlite3")
	if err != nil {
		log.Printf("[stats] sqlite3 not found, skipping OpenCode stats")
		return byDate
	}

	out, err := exec.Command(sqlite3, "-readonly", "-json", dbPath, openCodeUsageQuery).Output()
	if err != nil {
		log.Printf("[stats] OpenCode stats query failed: %v", err)
		return byDate
	}

	var rows []openCodeMessageUsageRow
	if err := json.Unmarshal(out, &rows); err != nil {
		log.Printf("[stats] failed to parse OpenCode stats rows: %v", err)
		return byDate
	}

	for _, row := range rows {
		if !openCodeRowIsAssistant(row.Role) {
			continue
		}
		modelID := strings.TrimSpace(firstRawString(row.Model))
		if modelID == "" {
			continue
		}
		seconds, ok := unixTimestampFromRaw(row.Created)
		if !ok {
			continue
		}
		local := time.Unix(seconds, 0).In(time.Local)
		date := local.Format("2006-01-02")
		hour := local.Hour() / 6
		if hour > 3 {
			hour = 3
		}

		input, _ := rawFloat64(row.Tin)
		output, _ := rawFloat64(row.Tout)
		reasoning, _ := rawFloat64(row.Trea)
		cacheRead, _ := rawFloat64(row.Tcr)
		cacheWrite, _ := rawFloat64(row.Tcw)
		if input+output+reasoning+cacheRead+cacheWrite <= 0 {
			// No observed usage: the row is not a usage row.
			continue
		}
		cost, costOK := rawFloat64(row.Cost)

		agg := ensureDateAgg(byDate, date)

		// Per-model aggregation.
		model := agg.models[modelID]
		model.totalTokens += int64(input + output + reasoning + cacheRead + cacheWrite)
		model.inputTokens += int64(input)
		model.outputTokens += int64(output)
		model.reasoning += int64(reasoning)
		model.cacheRead += int64(cacheRead)
		model.cacheCreate += int64(cacheWrite)
		model.sessions++
		model.cost += cost
		model.costRecorded = true
		if !costOK {
			model.costUnknown = true
		}
		agg.models[modelID] = model

		// Per-project aggregation from the message working directory.
		if projectName := openCodeProjectName(row.Cwd); projectName != "" {
			project := ensureProjectAgg(agg, projectName)
			project.totalTokens += int64(input + output + reasoning + cacheRead + cacheWrite)
			project.inputTokens += int64(input)
			project.outputTokens += int64(output)
			project.reasoning += int64(reasoning)
			project.cacheRead += int64(cacheRead)
			project.cacheCreate += int64(cacheWrite)
			project.cost += cost
			project.sessions++
			if !costOK {
				project.costUnknown = true
			}
		}

		// Time-of-day slot aggregation.
		slot := agg.slots[hour]
		slot.totalTokens += int64(input + output + reasoning + cacheRead + cacheWrite)
		slot.inputTokens += int64(input)
		slot.outputTokens += int64(output)
		slot.reasoning += int64(reasoning)
		slot.cacheRead += int64(cacheRead)
		slot.cacheCreate += int64(cacheWrite)
		slot.sessions++
		agg.slots[hour] = slot
	}

	return byDate
}

func openCodeRowIsAssistant(role json.RawMessage) bool {
	return strings.EqualFold(strings.TrimSpace(firstRawString(role)), "assistant")
}

func openCodeProjectName(cwd json.RawMessage) string {
	raw := strings.TrimSpace(firstRawString(cwd))
	if raw == "" {
		return ""
	}
	name := filepath.Base(filepath.Clean(raw))
	if name == "." || name == string(filepath.Separator) {
		return ""
	}
	return name
}

func firstRawString(raw json.RawMessage) string {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return ""
}
