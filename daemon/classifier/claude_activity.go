package classifier

import (
	"regexp"
	"strings"
)

var (
	claudeChromeRe     = regexp.MustCompile(`(?i)\bclaude\s+code\b`)
	claudePermissionRe = regexp.MustCompile(`(?i)(do you want to (create|run|edit|delete)|allow .+ to|permission required|askuserquestion)`)
)

// ClaudeActivityAdapter recognizes only visible permission/approval evidence.
// Canonical provider transcripts remain owned by daemon/work.
type ClaudeActivityAdapter struct{}

func NewClaudeActivityAdapter() *ClaudeActivityAdapter {
	return &ClaudeActivityAdapter{}
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

	return ActivitySignal{State: StateUnknown, Source: "claude_idle", Provider: a.Name()}
}
