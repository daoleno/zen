package work

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestAgentCLIDigestProvider_AutoClearsPreferredProvider(t *testing.T) {
	provider := NewAgentCLIDigestProvider(&ExecutorConfig{Default: "claude"})

	selected, ok := provider.SetPreferredProvider("codex")
	if !ok {
		t.Fatal("SetPreferredProvider(codex) failed")
	}
	if selected != "codex" || provider.PreferredProvider() != "codex" {
		t.Fatalf("selected = %q preferred = %q", selected, provider.PreferredProvider())
	}

	selected, ok = provider.SetPreferredProvider("auto")
	if !ok {
		t.Fatal("SetPreferredProvider(auto) failed")
	}
	if selected != "auto" {
		t.Fatalf("selected = %q, want auto", selected)
	}
	if provider.PreferredProvider() != "" {
		t.Fatalf("preferred = %q, want empty auto mode", provider.PreferredProvider())
	}
}

func TestSanitizePromptUTF8_ReplacesInvalidBytes(t *testing.T) {
	raw := string([]byte{'o', 'k', ' ', 0xff, 'd', 'o', 'n', 'e'})
	if utf8.ValidString(raw) {
		t.Fatal("test input should be invalid UTF-8")
	}

	got := sanitizePromptUTF8(raw)
	if !utf8.ValidString(got) {
		t.Fatalf("sanitized prompt is still invalid UTF-8: %q", got)
	}
	if !strings.Contains(got, "ok ") || !strings.Contains(got, "done") || !strings.Contains(got, "\uFFFD") {
		t.Fatalf("sanitized prompt = %q", got)
	}
}

func TestBuildAgentDigestPrompt_UsesDailyEvidenceLanguage(t *testing.T) {
	prompt := buildAgentDigestPrompt(AgentDigestInput{
		Agent: classifier.Agent{
			ID:        "main:@42",
			Name:      "codex",
			Project:   "zen",
			Command:   "codex",
			LastLines: []string{"implemented Brain daily readout"},
		},
		Status: "running",
		Now:    time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC),
	})

	for _, forbidden := range []string{
		"one coding-agent session",
		"Session:",
		"this round Agent",
	} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt contains %q:\n%s", forbidden, prompt)
		}
	}
	for _, want := range []string{"daily mobile Brain readout", "Evidence slice:"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}
