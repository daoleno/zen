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
	StateRemoved AgentState = "removed"
	StateUnknown AgentState = "unknown"
)

// Agent holds the current state and metadata for a single agent session.
type Agent struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Project             string     `json:"project,omitempty"`
	Cwd                 string     `json:"cwd,omitempty"`
	Command             string     `json:"command,omitempty"`
	State               AgentState `json:"status"`
	Summary             string     `json:"summary"`
	Phase               string     `json:"phase,omitempty"`
	Attention           string     `json:"attention,omitempty"`
	TaskClass           string     `json:"task_class,omitempty"`
	EventKind           string     `json:"event_kind,omitempty"`
	DetailsJSON         string     `json:"details_json,omitempty"`
	NeedsAttention      bool       `json:"needs_attention,omitempty"`
	LastProgressAt      *time.Time `json:"last_progress_at,omitempty"`
	ExpectedNextCheckAt *time.Time `json:"expected_next_check_at,omitempty"`
	LeaseSeconds        int        `json:"lease_seconds,omitempty"`
	LastLines           []string   `json:"last_output_lines"`
	StartedAt           time.Time  `json:"started_at,omitempty"`
	UpdatedAt           time.Time  `json:"updated_at"`
	LastSeenAt          time.Time  `json:"last_seen_at,omitempty"`
	ProcessID           int        `json:"process_id,omitempty"`
	Hidden              bool       `json:"hidden,omitempty"`
	Delegated           bool       `json:"delegated,omitempty"`
	PaneAlive           bool       `json:"-"`
}

// blockedPatterns match output that indicates the agent is waiting for user input.
//
// Approval/rejection patterns intentionally require interactive phrasing.
// Static agent mode chrome such as Grok's "always-approve" footer must not match:
// bare "approve|reject" previously false-positive blocked every Grok session in
// YOLO/always-approve mode.
var blockedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\(Y/n\)\s*$`),
	regexp.MustCompile(`(?i)\(y/N\)\s*$`),
	regexp.MustCompile(`(?i)\?\s*$`),
	regexp.MustCompile(`(?i)Do you want to proceed`),
	regexp.MustCompile(`(?i)Should I continue`),
	// Interactive approval gates only (not "always-approve" mode chrome).
	regexp.MustCompile(`(?i)please\s+approve`),
	regexp.MustCompile(`(?i)approve\s+or\s+reject`),
	regexp.MustCompile(`(?i)\breject\s+(this|the|and)\b`),
	regexp.MustCompile(`(?i)Press enter to continue`),
	regexp.MustCompile(`(?i)Press enter to confirm or esc to cancel`),
	regexp.MustCompile(`(?i)Action Required`),
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

var immediateBlockedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)Press enter to confirm or esc to cancel`),
	regexp.MustCompile(`(?i)Press enter to continue`),
	regexp.MustCompile(`(?i)Action Required`),
}

var readyComposerLineRe = regexp.MustCompile(`^\s*(?:[›❯]\s*|→\s*Add a follow-up.*)$`)

// Grok paints provider-native choice menus only in the live TUI. Canonical
// conversation sources (updates.jsonl / chat_history.jsonl) do not carry the
// ordered options, so Interface must treat the current pane lines as the sole
// live fact. Match structure, not one English billing string.
// Optional ›/> prefix is Grok's selection caret; plain capture-pane has no reverse-video.
// Capture-pane may keep bare glyphs or parenthesize them as (○)/(●)/(O).
var grokChoiceOptionRe = regexp.MustCompile(`(?i)^\s*[›>]?\s*[1-9]\s*(?:[○●◯◉◎]|\([○●◯◉◎Oo\s]*\))\s+\S`)
var grokChoiceFooterRe = regexp.MustCompile(`(?i)(?:↑\s*/\s*↓|↑|↓).{0,24}navigate|(?i)\bnavigate\b.{0,40}\benter\b|(?i)\benter\s*:\s*submit\b|(?i)\benter\s+confirm\b`)

// failedPatterns match output that indicates the agent has encountered an error.
var failedPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^error:`),
	regexp.MustCompile(`(?i)^fatal:`),
	regexp.MustCompile(`(?i)^panic:`),
	regexp.MustCompile(`(?i)traceback \(most recent call last\)`),
	regexp.MustCompile(`(?i)unhandled exception`),
	regexp.MustCompile(`\bFAILED\b`),
	regexp.MustCompile(`(?i)command not found`),
	regexp.MustCompile(`(?i)permission denied`),
	regexp.MustCompile(`(?i)segmentation fault`),
}

var nonFatalDiagnosticStartPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bwork transcript lookup failed for (codex|claude)\b`),
}

var timestampedLogLineRe = regexp.MustCompile(`^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}\b`)

// Classify determines pane-derived state from tmux output and liveness.
//
// It never returns Running. Pane liveness and recent output churn only prove the
// session is alive; active-turn Running comes from lifecycle progress leases or
// provider activity adapters via ResolveSessionStatus.
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
//	UNKNOWN (idle / no durable activity signal)
//
// command is the live pane process command line. Grok radio/footer menus are
// only classified when that command is Grok; the same painted lines under
// Codex/Claude/Cursor/shell must not phantom-block another provider.
func Classify(paneAlive bool, lines []string, command string) (AgentState, string) {
	if len(lines) == 0 {
		if !paneAlive {
			return StateDone, "Session ended (no output)"
		}
		return StateUnknown, "No output yet"
	}

	// Get the last few meaningful lines for pattern matching.
	tail := lastNonEmpty(lines, 10)
	interactiveTail := linesAfterLastReadyComposer(tail)
	lastLine := ""
	if len(interactiveTail) > 0 {
		lastLine = interactiveTail[len(interactiveTail)-1]
	}

	if !paneAlive {
		// Pane is dead. Check if it failed or completed normally.
		if line := matchingFailedLine(tail); line != "" {
			return StateFailed, truncate(line, 100)
		}
		return StateDone, summarize(tail)
	}

	if line := matchingImmediateBlockedLine(interactiveTail); line != "" {
		return StateBlocked, truncate(line, 100)
	}
	if isGrokCommand(command) {
		if line := matchingGrokChoiceMenu(tail); line != "" {
			return StateBlocked, truncate(line, 100)
		}
	}

	// Pane is alive. Check for blocked state first (highest priority after dead).
	for _, p := range blockedPatterns {
		if p.MatchString(strings.TrimSpace(lastLine)) {
			return StateBlocked, truncate(lastLine, 100)
		}
	}

	// Check for failed patterns in recent output.
	if line := matchingFailedLine(tail); line != "" {
		return StateFailed, truncate(line, 100)
	}

	// Broader blocked scan (prompt may not be the final line).
	for _, p := range blockedPatterns {
		for _, line := range interactiveTail {
			if p.MatchString(strings.TrimSpace(line)) {
				return StateBlocked, truncate(line, 100)
			}
		}
	}

	if summarize(tail) != "" {
		return StateUnknown, summarize(tail)
	}
	return StateUnknown, "Session idle"
}

func linesAfterLastReadyComposer(lines []string) []string {
	lastComposer := -1
	for index, line := range lines {
		if readyComposerLineRe.MatchString(strings.TrimSpace(line)) {
			lastComposer = index
		}
	}
	if lastComposer < 0 {
		return lines
	}
	return lines[lastComposer+1:]
}

func matchingImmediateBlockedLine(lines []string) string {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		for _, p := range immediateBlockedPatterns {
			if p.MatchString(trimmed) {
				return trimmed
			}
		}
	}
	return ""
}

func matchingGrokChoiceMenu(lines []string) string {
	optionLine := ""
	footerLine := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if optionLine == "" && grokChoiceOptionRe.MatchString(trimmed) {
			optionLine = trimmed
		}
		if footerLine == "" && grokChoiceFooterRe.MatchString(trimmed) {
			footerLine = trimmed
		}
	}
	if optionLine == "" || footerLine == "" {
		return ""
	}
	return footerLine
}

func isGrokCommand(command string) bool {
	base := commandBaseName(command)
	return base == "grok" || strings.HasPrefix(base, "grok-")
}

func matchingFailedLine(lines []string) string {
	inNonFatalBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if timestampedLogLineRe.MatchString(trimmed) {
			inNonFatalBlock = false
		}
		if startsNonFatalDiagnosticBlock(trimmed) {
			inNonFatalBlock = true
			continue
		}
		if inNonFatalBlock {
			continue
		}
		for _, p := range failedPatterns {
			if p.MatchString(trimmed) {
				return trimmed
			}
		}
	}
	return ""
}

func startsNonFatalDiagnosticBlock(line string) bool {
	for _, p := range nonFatalDiagnosticStartPatterns {
		if p.MatchString(line) {
			return true
		}
	}
	return false
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
