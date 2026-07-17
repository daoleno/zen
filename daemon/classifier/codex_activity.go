package classifier

import (
	"regexp"
	"strings"
)

var (
	codexChromeRe    = regexp.MustCompile(`(?i)\bopenai\s+codex\b`)
	codexWorkingRe   = regexp.MustCompile(`(?im)\bworking\b`)
	codexInterruptRe = regexp.MustCompile(`(?i)esc\s+to\s+interrupt`)
	// Interactive approval phrasing only — exclude static mode chrome like "always-approve".
	codexApprovalPromptRe = regexp.MustCompile(`(?i)(press enter to (continue|confirm)|do you want to|please\s+approve|approve\s+or\s+reject|allow .+ to)`)
)

// CodexActivityAdapter detects only visible approval and Working/interrupt pane
// evidence. Canonical provider transcripts remain owned by daemon/work.
type CodexActivityAdapter struct{}

func NewCodexActivityAdapter() *CodexActivityAdapter {
	return &CodexActivityAdapter{}
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
