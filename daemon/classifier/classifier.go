package classifier

import (
	"regexp"
	"strings"
	"time"
)

// AgentState represents the classified state of a tmux-managed agent.
type AgentState string

const (
	StateRunning AgentState = "running"
	StateBlocked AgentState = "blocked"
	StateDone    AgentState = "done"
	StateFailed  AgentState = "failed"
	StateUnknown AgentState = "unknown"
)

// Agent holds the current state and metadata for a single agent session.
type Agent struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	State         AgentState `json:"status"`
	Summary       string     `json:"summary"`
	LastLines     []string   `json:"last_output_lines"`
	UpdatedAt     time.Time  `json:"updated_at"`
	StateVersion  int64      `json:"state_version"` // increments on every state change
	PaneAlive     bool       `json:"-"`
	LastOutputLen int        `json:"-"`
	StaleCount    int        `json:"-"` // consecutive polls with no new output
}

// blockedPatterns match output that indicates the agent is waiting for user input.
var blockedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\(Y/n\)\s*$`),
	regexp.MustCompile(`(?i)\(y/N\)\s*$`),
	regexp.MustCompile(`(?i)\?\s*$`),
	regexp.MustCompile(`(?i)Do you want to proceed`),
	regexp.MustCompile(`(?i)Should I continue`),
	regexp.MustCompile(`(?i)approve|reject`),
	regexp.MustCompile(`(?i)Press enter to continue`),
	regexp.MustCompile(`(?i)Would you like`),
	regexp.MustCompile(`(?i)Is this ok`),
	regexp.MustCompile(`(?i)Shall I`),
	// Claude Code specific
	regexp.MustCompile(`(?i)Do you want to create`),
	regexp.MustCompile(`(?i)Do you want to run`),
	regexp.MustCompile(`(?i)Do you want to edit`),
	regexp.MustCompile(`(?i)Do you want to delete`),
	regexp.MustCompile(`(?i)Allow .+ to`),
}

// failedPatterns match output that indicates the agent has encountered an error.
var failedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^error:`),
	regexp.MustCompile(`(?i)^fatal:`),
	regexp.MustCompile(`(?i)^panic:`),
	regexp.MustCompile(`(?i)traceback \(most recent call last\)`),
	regexp.MustCompile(`(?i)unhandled exception`),
	regexp.MustCompile(`(?i)FAILED`),
	regexp.MustCompile(`(?i)command not found`),
	regexp.MustCompile(`(?i)permission denied`),
	regexp.MustCompile(`(?i)segmentation fault`),
}

// Classify determines the state of an agent based on its tmux pane output and liveness.
//
//	tmux pane alive? ──no──→ check last lines ──failed patterns?──→ FAILED
//	                                           └──otherwise──→ DONE
//	        │yes
//	        ▼
//	last N lines match blocked pattern? ──yes──→ BLOCKED
//	        │no
//	        ▼
//	last N lines match failed pattern? ──yes──→ FAILED
//	        │no
//	        ▼
//	output changed recently? ──yes──→ RUNNING
//	        │no (stale > 30 polls = ~15s at 500ms)
//	        ▼
//	check last lines for blocked ──yes──→ BLOCKED
//	        │no
//	        ▼
//	UNKNOWN
func Classify(paneAlive bool, lines []string, staleCount int) (AgentState, string) {
	if len(lines) == 0 {
		if !paneAlive {
			return StateDone, "Session ended (no output)"
		}
		return StateUnknown, "No output yet"
	}

	// Get the last few meaningful lines for pattern matching.
	tail := lastNonEmpty(lines, 10)
	lastLine := ""
	if len(tail) > 0 {
		lastLine = tail[len(tail)-1]
	}

	if !paneAlive {
		// Pane is dead. Check if it failed or completed normally.
		for _, p := range failedPatterns {
			for _, line := range tail {
				if p.MatchString(strings.TrimSpace(line)) {
					return StateFailed, truncate(line, 100)
				}
			}
		}
		return StateDone, summarize(tail)
	}

	// Pane is alive. Check for blocked state first (highest priority after dead).
	for _, p := range blockedPatterns {
		if p.MatchString(strings.TrimSpace(lastLine)) {
			return StateBlocked, truncate(lastLine, 100)
		}
	}

	// Check for failed patterns in recent output.
	for _, p := range failedPatterns {
		for _, line := range tail {
			if p.MatchString(strings.TrimSpace(line)) {
				return StateFailed, truncate(line, 100)
			}
		}
	}

	// If output is actively changing, the agent is running.
	if staleCount < 30 { // ~15 seconds at 500ms polling
		return StateRunning, summarize(tail)
	}

	// Output hasn't changed for a while. Re-check blocked patterns more broadly.
	for _, p := range blockedPatterns {
		for _, line := range tail {
			if p.MatchString(strings.TrimSpace(line)) {
				return StateBlocked, truncate(line, 100)
			}
		}
	}

	return StateUnknown, "No new output for " + time.Duration(time.Duration(staleCount)*500*time.Millisecond).String()
}

func lastNonEmpty(lines []string, n int) []string {
	var result []string
	for i := len(lines) - 1; i >= 0 && len(result) < n; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed != "" {
			result = append([]string{lines[i]}, result...)
		}
	}
	return result
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func summarize(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	return truncate(last, 100)
}
