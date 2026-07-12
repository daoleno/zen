package classifier

import (
	"regexp"
	"strings"
)

// Cursor activity signals (evidence from live sessions 2026-07-12):
//
//	RUNNING pane: "→ Add a follow-up" WITH "ctrl+c to stop"; often a "Running"
//	              spinner line; transcript last_user > last turn_ended.
//	IDLE pane:    "→ Add a follow-up" WITHOUT "ctrl+c to stop"; transcript ends
//	              with {"type":"turn_ended",...}; MCP children still present.
//	SHELL pane:   no Cursor chrome / no stop marker → never upgrade via this adapter.
//
// Process tree: long-lived MCP (playwright/context7) exists while idle, so MCP
// alone must not imply Running. Non-MCP worker children (shell tool sandboxes)
// reinforce Running when present.
//
// State machine (after Classify + progress merge, base == unknown only):
//
//	trust/permission prompt  → blocked
//	ctrl+c to stop           → running  (generating or tools)
//	transcript turn active   → running
//	non-MCP tool child       → running
//	else                     → unknown (idle composer / ordinary shell)

var (
	cursorStopMarkerRe         = regexp.MustCompile(`(?i)ctrl\+c\s+to\s+stop`)
	cursorAgentChromeRe        = regexp.MustCompile(`(?i)\bcursor\s+agent\b`)
	cursorWorkspaceTrustRe     = regexp.MustCompile(`(?i)\bworkspace\s+trust\s+required\b`)
	cursorTrustThisWorkspaceRe = regexp.MustCompile(`(?i)\btrust\s+this\s+workspace\b`)
	cursorPermissionPromptRe   = regexp.MustCompile(`(?i)\b(allow\s+this\s+action|permission\s+required|waiting\s+for\s+approval)\b`)
)

// CursorActivityAdapter is the Cursor provider ActivityAdapter.
// It owns an optional cheap transcript probe; Watcher never talks to Cursor APIs
// directly.
type CursorActivityAdapter struct {
	transcript CursorTranscriptActiver
}

// NewCursorActivityAdapter builds the default Cursor adapter with an embedded
// mtime/offset transcript probe.
func NewCursorActivityAdapter() *CursorActivityAdapter {
	return &CursorActivityAdapter{transcript: NewCursorTranscriptActiveProbe()}
}

// NewCursorActivityAdapterWithTranscript injects a transcript probe (tests /
// custom homes).
func NewCursorActivityAdapterWithTranscript(transcript CursorTranscriptActiver) *CursorActivityAdapter {
	return &CursorActivityAdapter{transcript: transcript}
}

func (a *CursorActivityAdapter) Name() string { return "cursor" }

func (a *CursorActivityAdapter) Match(in ActivityInput) bool {
	return isCursorAgentCommandLine(in.Agent.Command) ||
		cursorAgentChromeRe.MatchString(latestCursorPaneWindow(in.PaneContent))
}

func (a *CursorActivityAdapter) Infer(in ActivityInput) ActivitySignal {
	pane := latestCursorPaneWindow(in.PaneContent)

	lower := strings.ToLower(pane)
	if cursorWorkspaceTrustRe.MatchString(pane) && cursorTrustThisWorkspaceRe.MatchString(lower) {
		return ActivitySignal{
			State:    StateBlocked,
			Summary:  "Workspace trust required",
			Source:   "cursor_pane_trust",
			Provider: a.Name(),
		}
	}
	if cursorPermissionPromptRe.MatchString(pane) {
		return ActivitySignal{
			State:    StateBlocked,
			Summary:  "Waiting for permission",
			Source:   "cursor_pane_permission",
			Provider: a.Name(),
		}
	}

	if cursorStopMarkerRe.MatchString(pane) {
		return ActivitySignal{
			State:    StateRunning,
			Summary:  "Cursor agent generating",
			Source:   "cursor_pane_stop_marker",
			Provider: a.Name(),
		}
	}

	// Pane already idle-shaped: only then consult the cheap transcript probe.
	if a.transcript != nil {
		if active, ok := a.transcript.Active(in.Agent); ok && active {
			return ActivitySignal{
				State:    StateRunning,
				Summary:  "Cursor turn in progress",
				Source:   "cursor_transcript_active",
				Provider: a.Name(),
			}
		}
	}

	if in.ToolChildActive {
		return ActivitySignal{
			State:    StateRunning,
			Summary:  "Cursor tool executing",
			Source:   "cursor_tool_child",
			Provider: a.Name(),
		}
	}

	return ActivitySignal{
		State:    StateUnknown,
		Summary:  "",
		Source:   "cursor_idle",
		Provider: a.Name(),
	}
}

// InferCursorActivity is a test/helper entry that runs Cursor pane/tool logic
// without filesystem transcript I/O. Prefer ActivityProbe / CursorActivityAdapter
// in production paths.
func InferCursorActivity(in CursorActivityInput) ActivitySignal {
	command := in.Command
	if in.LooksLikeCursorUI && !isCursorAgentCommandLine(command) &&
		!cursorAgentChromeRe.MatchString(latestCursorPaneWindow(in.PaneContent)) {
		command = "cursor-agent"
	}
	adapter := &CursorActivityAdapter{} // nil transcript: no filesystem probe
	input := ActivityInput{
		Agent:           Agent{Command: command},
		PaneContent:     in.PaneContent,
		ToolChildActive: in.ToolChildActive,
	}
	if !adapter.Match(input) {
		return ActivitySignal{}
	}
	signal := adapter.Infer(input)
	if signal.State == StateRunning || signal.State == StateBlocked {
		return signal
	}
	if in.TranscriptActive != nil && *in.TranscriptActive {
		return ActivitySignal{
			State:    StateRunning,
			Summary:  "Cursor turn in progress",
			Source:   "cursor_transcript_active",
			Provider: adapter.Name(),
		}
	}
	return signal
}

// CursorActivityInput is retained for focused Cursor unit tests.
type CursorActivityInput struct {
	Command           string
	PaneContent       string
	TranscriptActive  *bool
	ToolChildActive   bool
	LooksLikeCursorUI bool
}

func isCursorAgentCommandLine(command string) bool {
	base := commandBaseName(command)
	return base == "cursor-agent" || base == "agent"
}

func latestCursorPaneWindow(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	lower := strings.ToLower(normalized)
	if idx := strings.LastIndex(lower, "cursor agent"); idx >= 0 {
		return normalized[idx:]
	}
	lines := strings.Split(normalized, "\n")
	if len(lines) > 80 {
		return strings.Join(lines[len(lines)-80:], "\n")
	}
	return normalized
}
