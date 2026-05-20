package work

import "testing"

func TestIsAgentCommandRecognizesNativeAgentCommands(t *testing.T) {
	for _, command := range []string{
		"codex",
		"/usr/local/bin/codex exec",
		"claude",
		"claude-code",
		"cc",
	} {
		if !IsAgentCommand(command) {
			t.Fatalf("IsAgentCommand(%q) = false", command)
		}
	}
}

func TestIsAgentWorkItemRequiresNativeSource(t *testing.T) {
	items := []*Item{
		{Frontmatter: Frontmatter{ID: "old", Kind: brainLogKind}},
		{Frontmatter: Frontmatter{ID: "codex", Kind: brainLogKind, AgentSource: "codex"}},
		{Frontmatter: Frontmatter{ID: "claude", Kind: brainLogKind, AgentSource: "claude"}},
		{Frontmatter: Frontmatter{ID: "task", Kind: "task", AgentSource: "codex"}},
	}

	filtered := FilterAgentWorkItems(items)
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
	if filtered[0].Frontmatter.ID != "codex" || filtered[1].Frontmatter.ID != "claude" {
		t.Fatalf("filtered = %#v", filtered)
	}
}
