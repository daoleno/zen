package classifier

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	codexChromeRe         = regexp.MustCompile(`(?i)\bopenai\s+codex\b`)
	codexWorkingRe        = regexp.MustCompile(`(?im)\bworking\b`)
	codexInterruptRe      = regexp.MustCompile(`(?i)esc\s+to\s+interrupt`)
	codexApprovalPromptRe = regexp.MustCompile(`(?i)(press enter to (continue|confirm)|do you want to|approve|reject|allow .+ to)`)
)

// CodexActivityAdapter detects Codex turn activity via rollout JSONL lifecycle
// events (task_started vs task_complete/turn_aborted), with pane Working+esc as
// an auxiliary Running signal and mtime freshness against stale opens.
type CodexActivityAdapter struct {
	transcript *JSONLTurnProbe
}

func NewCodexActivityAdapter() *CodexActivityAdapter {
	return &CodexActivityAdapter{transcript: NewCodexTranscriptProbe()}
}

func NewCodexActivityAdapterWithProbe(probe *JSONLTurnProbe) *CodexActivityAdapter {
	return &CodexActivityAdapter{transcript: probe}
}

func (a *CodexActivityAdapter) Name() string { return "codex" }

func (a *CodexActivityAdapter) Match(in ActivityInput) bool {
	base := commandBaseName(in.Agent.Command)
	if base == "codex" || strings.Contains(base, "codex") {
		return true
	}
	return codexChromeRe.MatchString(in.PaneContent)
}

func (a *CodexActivityAdapter) Infer(in ActivityInput) ActivitySignal {
	pane := latestProviderPaneWindow(in.PaneContent, "openai codex")
	if codexApprovalPromptRe.MatchString(pane) {
		return ActivitySignal{
			State:    StateBlocked,
			Summary:  "Waiting for Codex approval",
			Source:   "codex_pane_blocked",
			Provider: a.Name(),
		}
	}

	if a.transcript != nil {
		res := a.transcript.Probe(in.Agent)
		if res.OK {
			if res.Failed && !res.Active {
				return ActivitySignal{State: StateFailed, Summary: "Codex turn failed", Source: "codex_transcript_failed", Provider: a.Name()}
			}
			if res.Blocked {
				return ActivitySignal{State: StateBlocked, Summary: "Codex needs input", Source: "codex_transcript_blocked", Provider: a.Name()}
			}
			if res.Active {
				return ActivitySignal{State: StateRunning, Summary: "Codex turn in progress", Source: "codex_transcript_active", Provider: a.Name()}
			}
		}
	}

	if codexWorkingRe.MatchString(pane) && codexInterruptRe.MatchString(pane) {
		return ActivitySignal{
			State:    StateRunning,
			Summary:  "Codex working",
			Source:   "codex_pane_working",
			Provider: a.Name(),
		}
	}

	return ActivitySignal{State: StateUnknown, Source: "codex_idle", Provider: a.Name()}
}

func NewCodexTranscriptProbe() *JSONLTurnProbe {
	return NewJSONLTurnProbe(resolveCodexRolloutPath, inspectCodexLifecycleLine)
}

func inspectCodexLifecycleLine(line []byte, lineStart int64, state *JSONLTurnState) {
	trimmed := bytesTrimSpace(line)
	if len(trimmed) == 0 {
		return
	}
	var envelope struct {
		Type    string `json:"type"`
		Payload struct {
			Type           string          `json:"type"`
			Reason         string          `json:"reason"`
			CodexErrorInfo json.RawMessage `json:"codex_error_info"`
		} `json:"payload"`
	}
	if json.Unmarshal(trimmed, &envelope) != nil {
		return
	}
	eventType := strings.ToLower(strings.TrimSpace(envelope.Payload.Type))
	if eventType == "" {
		eventType = strings.ToLower(strings.TrimSpace(envelope.Type))
	}
	switch eventType {
	case "task_started", "turn_started", "stream_error":
		state.OpenOff = lineStart
		state.Failed = false
	case "task_complete", "turn_complete", "turn_aborted":
		state.CloseOff = lineStart
		if eventType == "turn_aborted" {
			state.Failed = true
		}
	case "error":
		if len(bytesTrimSpace(envelope.Payload.CodexErrorInfo)) > 0 || strings.TrimSpace(envelope.Payload.Reason) != "" {
			state.CloseOff = lineStart
			state.Failed = true
		}
	}
}

func resolveCodexRolloutPath(home string, agent Agent, now time.Time) (string, bool) {
	if home == "" {
		return "", false
	}
	resumeID := strings.ToLower(strings.TrimSpace(resumeSessionIDFromCommand(agent.Command)))
	sessionsRoot := filepath.Join(home, ".codex", "sessions")

	type candidate struct {
		path    string
		modTime time.Time
	}
	var candidates []candidate

	// Prefer recent date buckets (Codex layout: sessions/YYYY/MM/DD/rollout-*.jsonl)
	// instead of walking the entire sessions tree every miss.
	for dayOffset := 0; dayOffset < 14; dayOffset++ {
		day := now.UTC().AddDate(0, 0, -dayOffset)
		dir := filepath.Join(sessionsRoot, day.Format("2006"), day.Format("01"), day.Format("02"))
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasPrefix(name, "rollout-") || !strings.HasSuffix(name, ".jsonl") {
				continue
			}
			if resumeID != "" && !strings.Contains(strings.ToLower(name), resumeID) {
				continue
			}
			info, err := entry.Info()
			if err != nil || !transcriptFresh(info.ModTime(), now, defaultTranscriptMaxAge) {
				continue
			}
			candidates = append(candidates, candidate{
				path:    filepath.Join(dir, name),
				modTime: info.ModTime(),
			})
		}
		// With an explicit resume ID, the newest matching day hit is enough.
		if resumeID != "" && len(candidates) > 0 {
			break
		}
	}

	if len(candidates) == 0 {
		return "", false
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modTime.After(candidates[j].modTime)
	})

	if resumeID != "" {
		return candidates[0].path, true
	}

	for _, candidate := range candidates {
		cwd := readCodexRolloutCWD(candidate.path)
		if cwd == "" || cwdMatchesAgent(cwd, agent.Cwd) {
			return candidate.path, true
		}
	}
	return "", false
}

func readCodexRolloutCWD(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for i := 0; i < 12; i++ {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var envelope struct {
				Type    string `json:"type"`
				Payload struct {
					CWD string `json:"cwd"`
				} `json:"payload"`
				CWD string `json:"cwd"`
			}
			if json.Unmarshal(bytesTrimSpace(line), &envelope) == nil {
				if cwd := strings.TrimSpace(firstNonEmpty(envelope.Payload.CWD, envelope.CWD)); cwd != "" {
					return filepath.Clean(cwd)
				}
			}
		}
		if err != nil {
			break
		}
	}
	return ""
}

func cwdMatchesAgent(metaCWD, agentCWD string) bool {
	metaCWD = filepath.Clean(strings.TrimSpace(metaCWD))
	for _, candidate := range cwdPathCandidates(agentCWD) {
		if metaCWD == candidate {
			return true
		}
	}
	return false
}

func latestProviderPaneWindow(content, marker string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lower := strings.ToLower(normalized)
	marker = strings.ToLower(strings.TrimSpace(marker))
	if marker != "" {
		if idx := strings.LastIndex(lower, marker); idx >= 0 {
			return normalized[idx:]
		}
	}
	lines := strings.Split(normalized, "\n")
	if len(lines) > 80 {
		return strings.Join(lines[len(lines)-80:], "\n")
	}
	return normalized
}
