package classifier

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var grokChromeRe = regexp.MustCompile(`(?i)\bgrok\b`)

// GrokActivityAdapter uses updates.jsonl durable lifecycle markers.
// summary.updated_at is intentionally ignored for activity.
type GrokActivityAdapter struct {
	transcript *JSONLTurnProbe
}

func NewGrokActivityAdapter() *GrokActivityAdapter {
	return &GrokActivityAdapter{transcript: NewGrokTranscriptProbe()}
}

func NewGrokActivityAdapterWithProbe(probe *JSONLTurnProbe) *GrokActivityAdapter {
	return &GrokActivityAdapter{transcript: probe}
}

func (a *GrokActivityAdapter) Name() string { return "grok" }

func (a *GrokActivityAdapter) Match(in ActivityInput) bool {
	base := commandBaseName(in.Agent.Command)
	if base == "grok" || strings.Contains(base, "grok") {
		return true
	}
	lower := strings.ToLower(in.PaneContent)
	return strings.Contains(lower, "grok") && (strings.Contains(lower, "xai") || grokChromeRe.MatchString(in.PaneContent))
}

func (a *GrokActivityAdapter) Infer(in ActivityInput) ActivitySignal {
	if a.transcript != nil {
		res := a.transcript.Probe(in.Agent)
		if res.OK {
			if res.Failed {
				return ActivitySignal{State: StateFailed, Summary: "Grok turn failed", Source: "grok_transcript_failed", Provider: a.Name()}
			}
			if res.Active {
				return ActivitySignal{State: StateRunning, Summary: "Grok turn in progress", Source: "grok_updates_active", Provider: a.Name()}
			}
			return ActivitySignal{State: StateUnknown, Source: "grok_idle", Provider: a.Name()}
		}
	}
	return ActivitySignal{State: StateUnknown, Source: "grok_idle", Provider: a.Name()}
}

func NewGrokTranscriptProbe() *JSONLTurnProbe {
	return NewJSONLTurnProbe(resolveGrokUpdatesPath, inspectGrokUpdatesLine)
}

func inspectGrokUpdatesLine(line []byte, lineStart int64, state *JSONLTurnState) {
	trimmed := bytesTrimSpace(line)
	if len(trimmed) == 0 {
		return
	}
	var envelope struct {
		Params struct {
			Update struct {
				SessionUpdate string `json:"sessionUpdate"`
				Status        string `json:"status"`
			} `json:"update"`
		} `json:"params"`
	}
	if json.Unmarshal(trimmed, &envelope) != nil {
		return
	}
	update := strings.ToLower(strings.TrimSpace(envelope.Params.Update.SessionUpdate))
	status := strings.ToLower(strings.TrimSpace(envelope.Params.Update.Status))
	switch update {
	case "user_message_chunk", "agent_thought_chunk", "agent_message_chunk", "tool_call":
		state.OpenOff = lineStart
		state.Failed = false
	case "tool_call_update":
		switch status {
		case "failed":
			state.CloseOff = lineStart
			state.Failed = true
		case "completed":
			// Tool finished; turn may still be open until turn_completed.
		default:
			// in_progress / empty → reinforce open turn.
			state.OpenOff = lineStart
		}
	case "turn_completed", "task_backgrounded", "task_completed":
		state.CloseOff = lineStart
	case "error":
		state.CloseOff = lineStart
		state.Failed = true
	}
}

func resolveGrokUpdatesPath(home string, agent Agent, now time.Time) (string, bool) {
	if home == "" {
		return "", false
	}
	resumeID := resumeSessionIDFromCommand(agent.Command)
	var bestPath string
	var bestUpdated time.Time
	for _, candidateCWD := range cwdPathCandidates(agent.Cwd) {
		baseDir := filepath.Join(home, ".grok", "sessions", encodeGrokSessionCWDName(candidateCWD))
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			sessionID := entry.Name()
			if resumeID != "" && !strings.EqualFold(sessionID, resumeID) {
				continue
			}
			updatesPath := filepath.Join(baseDir, sessionID, "updates.jsonl")
			info, err := os.Stat(updatesPath)
			if err != nil || info.IsDir() || !transcriptFresh(info.ModTime(), now, defaultTranscriptMaxAge) {
				continue
			}
			// Prefer updates.jsonl mtime; never use summary.updated_at.
			if bestPath == "" || info.ModTime().After(bestUpdated) {
				bestPath = updatesPath
				bestUpdated = info.ModTime()
			}
		}
	}
	if bestPath == "" {
		return "", false
	}
	return bestPath, true
}

func encodeGrokSessionCWDName(cwd string) string {
	cwd = filepath.Clean(strings.TrimSpace(cwd))
	if cwd == "" || cwd == "." {
		return ""
	}
	if !strings.HasPrefix(cwd, "/") {
		cwd = "/" + cwd
	}
	return url.PathEscape(cwd)
}
