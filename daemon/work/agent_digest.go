package work

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/classifier"
)

var ErrNoAgentDigestProvider = errors.New("no agent digest provider available")

// AgentDigest is the AI-produced reading of one evidence slice for the daily work log.
type AgentDigest struct {
	Title    string   `json:"title"`
	Outcome  string   `json:"outcome,omitempty"`
	Readout  string   `json:"readout,omitempty"`
	Summary  string   `json:"summary,omitempty"`
	Signals  []string `json:"signals,omitempty"`
	Progress []string `json:"progress,omitempty"`
	Friction string   `json:"friction,omitempty"`
	Cause    string   `json:"cause,omitempty"`
	Insight  string   `json:"insight,omitempty"`
	Next     string   `json:"next"`
	Provider string   `json:"provider,omitempty"`
}

type AgentDigestInput struct {
	Agent           classifier.Agent
	Status          string
	Transcript      ToolTranscript
	PreviousTitle   string
	PreviousSummary string
	PreviousNext    string
	Now             time.Time
}

type AgentDigestProvider interface {
	Digest(ctx context.Context, input AgentDigestInput) (AgentDigest, error)
}

type AgentDigestProviderFunc func(context.Context, AgentDigestInput) (AgentDigest, error)

func (f AgentDigestProviderFunc) Digest(ctx context.Context, input AgentDigestInput) (AgentDigest, error) {
	return f(ctx, input)
}

type AgentCLIDigestProvider struct {
	execs     *ExecutorConfig
	timeout   time.Duration
	mu        sync.RWMutex
	preferred string
}

func NewAgentCLIDigestProvider(execs *ExecutorConfig) *AgentCLIDigestProvider {
	return &AgentCLIDigestProvider{
		execs:   execs,
		timeout: 45 * time.Second,
	}
}

func (p *AgentCLIDigestProvider) PreferredProvider() string {
	if p == nil {
		return ""
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.preferred
}

func (p *AgentCLIDigestProvider) SetPreferredProvider(provider string) (string, bool) {
	if p == nil {
		return "", false
	}
	normalized, ok := normalizeDigestProvider(provider)
	if !ok {
		return "", false
	}
	p.mu.Lock()
	p.preferred = normalized
	p.mu.Unlock()
	if normalized == "" {
		return "auto", true
	}
	return normalized, true
}

func (p *AgentCLIDigestProvider) Digest(ctx context.Context, input AgentDigestInput) (AgentDigest, error) {
	if p == nil {
		return AgentDigest{}, ErrNoAgentDigestProvider
	}
	provider, ok := p.selectProvider(input.Agent.Command)
	if !ok {
		return AgentDigest{}, ErrNoAgentDigestProvider
	}

	timeout := p.timeout
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	prompt := sanitizePromptUTF8(buildAgentDigestPrompt(input))
	var (
		digest AgentDigest
		err    error
	)
	switch provider {
	case "claude":
		digest, err = p.digestWithClaude(ctx, input.Agent.Cwd, prompt)
	default:
		digest, err = p.digestWithCodex(ctx, input.Agent.Cwd, prompt)
	}
	if err != nil {
		return AgentDigest{}, err
	}
	digest.Provider = provider
	return sanitizeDigest(digest), nil
}

func (p *AgentCLIDigestProvider) selectProvider(command string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) > 0 {
		command = strings.ToLower(filepath.Base(fields[0]))
	} else {
		command = ""
	}
	preferred := make([]string, 0, 4)
	if selected := p.PreferredProvider(); selected != "" {
		preferred = append(preferred, selected)
	} else {
		if strings.Contains(command, "claude") {
			preferred = append(preferred, "claude")
		}
		if strings.Contains(command, "codex") {
			preferred = append(preferred, "codex")
		}
	}
	if p.execs != nil {
		if name := strings.ToLower(strings.TrimSpace(p.execs.Default)); name == "claude" || name == "codex" {
			preferred = append(preferred, name)
		}
	}
	preferred = append(preferred, "codex", "claude")

	seen := map[string]bool{}
	for _, name := range preferred {
		if seen[name] {
			continue
		}
		seen[name] = true
		if _, err := exec.LookPath(name); err == nil {
			return name, true
		}
	}
	return "", false
}

func normalizeDigestProvider(provider string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "auto", "automatic":
		return "", true
	case "codex":
		return "codex", true
	case "claude", "claude-code", "claude code":
		return "claude", true
	default:
		return "", false
	}
}

func (p *AgentCLIDigestProvider) digestWithCodex(ctx context.Context, cwd, prompt string) (AgentDigest, error) {
	outFile, err := os.CreateTemp("", "zen-work-digest-*.json")
	if err != nil {
		return AgentDigest{}, err
	}
	outPath := outFile.Name()
	_ = outFile.Close()
	defer os.Remove(outPath)

	args := []string{
		"exec",
		"--ephemeral",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--color", "never",
		"--output-last-message", outPath,
	}
	if cleanCwd := strings.TrimSpace(cwd); cleanCwd != "" {
		args = append(args, "-C", cleanCwd)
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, "codex", args...)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return AgentDigest{}, fmt.Errorf("codex digest: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if data, readErr := os.ReadFile(outPath); readErr == nil && strings.TrimSpace(string(data)) != "" {
		return parseAgentDigest(string(data))
	}
	return parseAgentDigest(string(stdout))
}

func (p *AgentCLIDigestProvider) digestWithClaude(ctx context.Context, cwd, prompt string) (AgentDigest, error) {
	args := []string{
		"-p",
		"--output-format", "json",
		"--no-session-persistence",
		"--permission-mode", "dontAsk",
		"--tools", "",
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Stdin = strings.NewReader(prompt)
	if cleanCwd := strings.TrimSpace(cwd); cleanCwd != "" {
		cmd.Dir = cleanCwd
	}
	cmd.Env = append(os.Environ(), "NO_COLOR=1", "TERM=dumb")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.Output()
	if err != nil {
		return AgentDigest{}, fmt.Errorf("claude digest: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return parseAgentDigest(string(stdout))
}

func buildAgentDigestPrompt(input AgentDigestInput) string {
	agent := input.Agent
	tail := recentOutput(agent.LastLines)
	if strings.TrimSpace(tail) == "" {
		tail = "(no visible terminal output)"
	}
	transcript := formatTranscriptForPrompt(input.Transcript)
	return strings.TrimSpace(fmt.Sprintf(`
You are extracting one evidence slice for a daily mobile Brain readout.

The final Brain readout is day-level. This JSON is not a user-facing item summary; it will be
merged with other evidence from the same day. The user needs leverage: what actually changed,
where the work shape was healthy or wasteful, what caused friction, and what should change next
in either the code, the prompt boundary, the environment, the model, or the agent workflow.

Return ONLY valid JSON with this exact shape:
{"title":"short evidence label","outcome":"what actually changed or was decided","readout":"why this matters for the day","signals":["evidence about work quality"],"friction":"empty if none; otherwise the main drag","cause":"likely cause of friction or empty","insight":"one reusable lesson","next":"next high-leverage action"}

Rules:
- Analyze only coding-agent work. Ignore shell mechanics, tmux ids, progress spinners, prompts, and log noise.
- Do not write a chronological status report.
- Do not call the unit a session, round, or "this agent". The user-facing unit is the day.
- You may discuss model or agent behavior as evidence, but phrase the conclusion as a daily work observation.
- Prefer native tool transcript evidence over terminal output. Terminal output is only secondary context.
- The native transcript is compressed: counters and repeated surfaces are evidence, not the answer.
- Look for deeper signals: repeated user corrections, repeated edits to the same surface, failed tool calls, tests that catch regressions, missing context, unclear boundaries, and over-broad exploration.
- Convert transcript shape into judgment: what made the day easier, what created rework, and what habit/tooling/prompt change would reduce future work.
- Focus on behavior and leverage: clean completion, rework loops, weak task boundary, context gap, tooling/env issue, model mistake, or useful decision.
- If friction exists, name the most likely cause. If it is unclear, say "unknown" briefly rather than inventing.
- Prefer concrete evidence from output over generic judgment.
- Inline Markdown is allowed for code identifiers, file names, and short labels.
- Prefer the language used by the user's task or the terminal output.
- If the evidence is weak, say what can be inferred and keep it honest.
- Be compact. Avoid filler like "visible evidence indicates" unless uncertainty changes the next action.
- Avoid generic phrases like "worked on the task", "made progress", or "ran commands".
- title <= 64 chars; outcome/readout/friction/cause/insight/next <= 180 chars each; max 3 signals; each signal <= 120 chars.

Evidence slice:
- Status: %s
- Tool name: %s
- Command: %s
- Project: %s
- CWD: %s
- Classifier summary: %s
- Previous title: %s
- Previous summary: %s
- Previous next: %s
- Updated: %s

Native tool transcript evidence:
%s

Recent terminal output:
%s
`, input.Status,
		agent.Name,
		agent.Command,
		agent.Project,
		agent.Cwd,
		agent.Summary,
		input.PreviousTitle,
		input.PreviousSummary,
		input.PreviousNext,
		input.Now.Format(time.RFC3339),
		transcript,
		tail,
	))
}

func formatTranscriptForPrompt(transcript ToolTranscript) string {
	if strings.TrimSpace(transcript.Excerpt) == "" {
		return "(no native tool transcript found)"
	}
	header := []string{
		fmt.Sprintf("- Source: %s", transcript.Source),
		fmt.Sprintf("- Path: %s", transcript.Path),
	}
	if transcript.SessionID != "" {
		header = append(header, fmt.Sprintf("- Transcript ID: %s", transcript.SessionID))
	}
	if !transcript.Updated.IsZero() {
		header = append(header, fmt.Sprintf("- Updated: %s", transcript.Updated.Format(time.RFC3339)))
	}
	header = append(header, "", transcript.Excerpt)
	return strings.Join(header, "\n")
}

func parseAgentDigest(raw string) (AgentDigest, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AgentDigest{}, fmt.Errorf("empty digest response")
	}

	var digest AgentDigest
	if err := json.Unmarshal([]byte(raw), &digest); err == nil && hasDigestContent(digest) {
		return digest, nil
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &wrapper); err == nil {
		if resultRaw, ok := wrapper["result"]; ok {
			var result string
			if err := json.Unmarshal(resultRaw, &result); err == nil {
				return parseAgentDigest(result)
			}
			if err := json.Unmarshal(resultRaw, &digest); err == nil && hasDigestContent(digest) {
				return digest, nil
			}
		}
	}

	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		return parseAgentDigest(raw[start : end+1])
	}
	return AgentDigest{}, fmt.Errorf("digest response was not JSON")
}

func hasDigestContent(d AgentDigest) bool {
	return strings.TrimSpace(d.Title) != "" ||
		strings.TrimSpace(d.Outcome) != "" ||
		strings.TrimSpace(d.Readout) != "" ||
		strings.TrimSpace(d.Summary) != "" ||
		len(d.Signals) > 0 ||
		len(d.Progress) > 0 ||
		strings.TrimSpace(d.Friction) != "" ||
		strings.TrimSpace(d.Cause) != "" ||
		strings.TrimSpace(d.Insight) != "" ||
		strings.TrimSpace(d.Next) != ""
}

func sanitizeDigest(d AgentDigest) AgentDigest {
	readout := firstNonEmpty(d.Readout, d.Summary)
	signals := d.Signals
	if len(signals) == 0 {
		signals = d.Progress
	}
	out := AgentDigest{
		Title:    truncateRunes(collapseSpaces(d.Title), 70),
		Outcome:  truncateRunes(collapseSpaces(d.Outcome), 180),
		Readout:  truncateRunes(collapseSpaces(readout), 180),
		Summary:  truncateRunes(collapseSpaces(readout), 180),
		Friction: truncateRunes(collapseSpaces(d.Friction), 180),
		Cause:    truncateRunes(collapseSpaces(d.Cause), 180),
		Insight:  truncateRunes(collapseSpaces(d.Insight), 180),
		Next:     truncateRunes(collapseSpaces(d.Next), 180),
		Provider: strings.TrimSpace(d.Provider),
	}
	for _, item := range signals {
		item = truncateRunes(collapseSpaces(item), 120)
		if item == "" {
			continue
		}
		out.Signals = append(out.Signals, item)
		out.Progress = append(out.Progress, item)
		if len(out.Signals) >= 3 {
			break
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateRunes(value string, max int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= max {
		return string(runes)
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return strings.TrimSpace(string(runes[:max-3])) + "..."
}

func sanitizePromptUTF8(value string) string {
	if utf8.ValidString(value) {
		return value
	}
	return strings.ToValidUTF8(value, "\uFFFD")
}
