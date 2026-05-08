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
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

var ErrNoAgentDigestProvider = errors.New("no agent digest provider available")

// AgentDigest is the AI-produced reading of one agent session snapshot.
type AgentDigest struct {
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Progress []string `json:"progress"`
	Next     string   `json:"next"`
	Provider string   `json:"provider,omitempty"`
}

type AgentDigestInput struct {
	Agent           classifier.Agent
	Status          string
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
	execs   *ExecutorConfig
	timeout time.Duration
}

func NewAgentCLIDigestProvider(execs *ExecutorConfig) *AgentCLIDigestProvider {
	return &AgentCLIDigestProvider{
		execs:   execs,
		timeout: 45 * time.Second,
	}
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

	prompt := buildAgentDigestPrompt(input)
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
	if strings.Contains(command, "claude") {
		preferred = append(preferred, "claude")
	}
	if strings.Contains(command, "codex") {
		preferred = append(preferred, "codex")
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
	return strings.TrimSpace(fmt.Sprintf(`
You are turning a coding-agent terminal snapshot into a readable mobile work log.

Return ONLY valid JSON with this exact shape:
{"title":"short title","summary":"one useful paragraph","progress":["short bullet"],"next":"next useful action"}

Rules:
- Do not include markdown, code fences, tmux ids, ANSI noise, or generic terminal mechanics.
- Describe the actual engineering work in human terms.
- Prefer the language used by the user's task or the terminal output.
- If the evidence is weak, say what can be inferred and keep it honest.
- title <= 60 characters; summary <= 180 characters; each progress item <= 90 characters; max 4 progress items; next <= 120 characters.

Session:
- Status: %s
- Agent name: %s
- Command: %s
- Project: %s
- CWD: %s
- Classifier summary: %s
- Previous title: %s
- Previous summary: %s
- Previous next: %s
- Updated: %s

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
		tail,
	))
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
		strings.TrimSpace(d.Summary) != "" ||
		len(d.Progress) > 0 ||
		strings.TrimSpace(d.Next) != ""
}

func sanitizeDigest(d AgentDigest) AgentDigest {
	out := AgentDigest{
		Title:    truncateRunes(collapseSpaces(d.Title), 60),
		Summary:  truncateRunes(collapseSpaces(d.Summary), 180),
		Next:     truncateRunes(collapseSpaces(d.Next), 120),
		Provider: strings.TrimSpace(d.Provider),
	}
	for _, item := range d.Progress {
		item = truncateRunes(collapseSpaces(item), 90)
		if item == "" {
			continue
		}
		out.Progress = append(out.Progress, item)
		if len(out.Progress) >= 4 {
			break
		}
	}
	return out
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
