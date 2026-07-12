package classifier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	claudeChromeRe     = regexp.MustCompile(`(?i)\bclaude\s+code\b`)
	claudePermissionRe = regexp.MustCompile(`(?i)(do you want to (create|run|edit|delete)|allow .+ to|permission required|askuserquestion)`)
)

// ClaudeActivityAdapter uses a cheap transcript probe for open tools / turns,
// AskUserQuestion→blocked, and stays Idle when evidence is missing.
type ClaudeActivityAdapter struct {
	transcript *JSONLTurnProbe
}

func NewClaudeActivityAdapter() *ClaudeActivityAdapter {
	return &ClaudeActivityAdapter{transcript: NewClaudeTranscriptProbe()}
}

func NewClaudeActivityAdapterWithProbe(probe *JSONLTurnProbe) *ClaudeActivityAdapter {
	return &ClaudeActivityAdapter{transcript: probe}
}

func (a *ClaudeActivityAdapter) Name() string { return "claude" }

func (a *ClaudeActivityAdapter) Match(in ActivityInput) bool {
	base := commandBaseName(in.Agent.Command)
	if base == "claude" || base == "cc" || strings.Contains(base, "claude") {
		return true
	}
	return claudeChromeRe.MatchString(in.PaneContent)
}

func (a *ClaudeActivityAdapter) Infer(in ActivityInput) ActivitySignal {
	pane := latestProviderPaneWindow(in.PaneContent, "claude code")
	if claudePermissionRe.MatchString(pane) {
		return ActivitySignal{
			State:    StateBlocked,
			Summary:  "Waiting for Claude permission",
			Source:   "claude_pane_blocked",
			Provider: a.Name(),
		}
	}

	if a.transcript != nil {
		res := a.transcript.Probe(in.Agent)
		if res.OK {
			if res.Blocked {
				return ActivitySignal{
					State:    StateBlocked,
					Summary:  "Claude needs a decision",
					Source:   "claude_ask_user",
					Provider: a.Name(),
				}
			}
			if res.Active {
				return ActivitySignal{
					State:    StateRunning,
					Summary:  "Claude turn in progress",
					Source:   "claude_transcript_active",
					Provider: a.Name(),
				}
			}
			// Explicit miss: transcript found but idle.
			return ActivitySignal{State: StateUnknown, Source: "claude_idle", Provider: a.Name()}
		}
	}

	// No live transcript sample → conservative Idle (unknown).
	return ActivitySignal{State: StateUnknown, Source: "claude_idle", Provider: a.Name()}
}

func NewClaudeTranscriptProbe() *JSONLTurnProbe {
	return NewJSONLTurnProbe(resolveClaudeTranscriptPath, inspectClaudeActivityLine)
}

func inspectClaudeActivityLine(line []byte, lineStart int64, state *JSONLTurnState) {
	trimmed := bytesTrimSpace(line)
	if len(trimmed) == 0 {
		return
	}
	var record struct {
		Type    string `json:"type"`
		Message struct {
			Role       string          `json:"role"`
			StopReason string          `json:"stop_reason"`
			Content    json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(trimmed, &record) != nil {
		return
	}
	typeName := strings.ToLower(strings.TrimSpace(record.Type))
	role := strings.ToLower(strings.TrimSpace(record.Message.Role))
	if role == "" {
		role = typeName
	}

	switch typeName {
	case "user", "assistant":
		// continue
	default:
		if role != "user" && role != "assistant" {
			return
		}
	}

	openTools, askPending, sawToolResult := inspectClaudeContent(record.Message.Content)
	if askPending {
		state.Blocked = true
		state.OpenOff = lineStart
	}
	if sawToolResult {
		// A tool_result answers prior AskUserQuestion / running tools.
		state.Blocked = false
	}
	if openTools {
		state.OpenOff = lineStart
		return
	}

	if role == "user" && !sawToolResult {
		state.OpenOff = lineStart
		return
	}

	stop := strings.ToLower(strings.TrimSpace(record.Message.StopReason))
	switch stop {
	case "end_turn", "stop_sequence", "max_tokens":
		if !openTools {
			state.CloseOff = lineStart
		}
	case "tool_use":
		state.OpenOff = lineStart
	}
}

func inspectClaudeContent(raw json.RawMessage) (openTools bool, askPending bool, sawToolResult bool) {
	if len(bytesTrimSpace(raw)) == 0 {
		return false, false, false
	}
	var blocks []struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return false, false, false
	}
	for _, block := range blocks {
		switch strings.ToLower(strings.TrimSpace(block.Type)) {
		case "tool_use":
			openTools = true
			if strings.EqualFold(strings.TrimSpace(block.Name), "AskUserQuestion") {
				askPending = true
			}
		case "tool_result":
			sawToolResult = true
		}
	}
	return openTools, askPending, sawToolResult
}

func resolveClaudeTranscriptPath(home string, agent Agent, now time.Time) (string, bool) {
	if home == "" {
		return "", false
	}
	resumeID := resumeSessionIDFromCommand(agent.Command)
	var bestPath string
	var bestUpdated time.Time
	for _, candidateCWD := range cwdPathCandidates(agent.Cwd) {
		projectDir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDirName(candidateCWD))
		entries, err := os.ReadDir(projectDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
				continue
			}
			sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
			if resumeID != "" && !strings.EqualFold(sessionID, resumeID) {
				continue
			}
			path := filepath.Join(projectDir, entry.Name())
			info, err := entry.Info()
			if err != nil || !transcriptFresh(info.ModTime(), now, defaultTranscriptMaxAge) {
				continue
			}
			if bestPath == "" || info.ModTime().After(bestUpdated) {
				bestPath = path
				bestUpdated = info.ModTime()
			}
		}
	}
	if bestPath == "" {
		return "", false
	}
	return bestPath, true
}

func encodeClaudeProjectDirName(cwd string) string {
	return strings.ReplaceAll(filepath.Clean(strings.TrimSpace(cwd)), string(filepath.Separator), "-")
}
