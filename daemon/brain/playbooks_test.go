package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStoreEnsuresSeedPlaybooks(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	for _, name := range seedPlaybookFilenames() {
		path := store.playbookPath(name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read playbook %s: %v", name, err)
		}
		if strings.TrimSpace(string(raw)) == "" {
			t.Fatalf("playbook %s is empty", name)
		}
	}

	readme, err := os.ReadFile(store.playbooksReadmePath())
	if err != nil {
		t.Fatalf("read playbooks README: %v", err)
	}
	for _, marker := range []string{"Provider-neutral operating playbooks", "zen brain playbooks --json", "Progressive disclosure"} {
		if !strings.Contains(string(readme), marker) {
			t.Fatalf("playbooks README missing %q:\n%s", marker, readme)
		}
	}

	align, err := os.ReadFile(store.playbookPath("align.md"))
	if err != nil {
		t.Fatalf("read align playbook: %v", err)
	}
	for _, marker := range []string{"one question at a time", "Grill before you delegate"} {
		if !strings.Contains(string(align), marker) {
			t.Fatalf("align playbook missing %q:\n%s", marker, align)
		}
	}
}

func TestNewStorePreservesExistingPlaybookContent(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	playbooks := filepath.Join(workspace, "playbooks")
	if err := os.MkdirAll(playbooks, 0o700); err != nil {
		t.Fatalf("create playbooks dir: %v", err)
	}
	customAlign := "---\ndescription: Custom align playbook.\n---\n\n# Custom Align\n\nKeep my custom note.\n"
	if err := os.WriteFile(filepath.Join(playbooks, "align.md"), []byte(customAlign), 0o600); err != nil {
		t.Fatalf("write custom align playbook: %v", err)
	}

	if _, err := NewStore(root); err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(playbooks, "align.md"))
	if err != nil {
		t.Fatalf("read align playbook: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "Keep my custom note.") {
		t.Fatalf("custom align playbook was overwritten:\n%s", content)
	}
	if !strings.Contains(content, "Custom align playbook.") {
		t.Fatalf("custom align frontmatter was lost:\n%s", content)
	}
}

func TestPlaybookCatalogListsSeedPlaybooks(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}

	catalog, err := store.PlaybookCatalog()
	if err != nil {
		t.Fatalf("PlaybookCatalog() error = %v", err)
	}
	if len(catalog.Playbooks) != 5 {
		t.Fatalf("catalog playbooks = %d, want 5: %#v", len(catalog.Playbooks), catalog.Playbooks)
	}

	byName := map[string]PlaybookEntry{}
	for _, entry := range catalog.Playbooks {
		byName[entry.Name] = entry
	}
	for _, name := range []string{"align", "brain-flows", "delegate-brief", "slice-work", "wayfind"} {
		entry, ok := byName[name]
		if !ok {
			t.Fatalf("catalog missing playbook %q: %#v", name, catalog.Playbooks)
		}
		if entry.Path != "playbooks/"+name+".md" {
			t.Fatalf("playbook %q path = %q, want playbooks/%s.md", name, entry.Path, name)
		}
		if strings.TrimSpace(entry.Description) == "" {
			t.Fatalf("playbook %q missing description", name)
		}
	}
	if !strings.Contains(byName["brain-flows"].Description, "router") {
		t.Fatalf("brain-flows description = %q", byName["brain-flows"].Description)
	}
	if !strings.Contains(byName["wayfind"].Description, "Fog-of-war") {
		t.Fatalf("wayfind description = %q", byName["wayfind"].Description)
	}
}

func TestParsePlaybookDescription(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "frontmatter description",
			content: `---
description: One question at a time.
---

# Align
`,
			want: "One question at a time.",
		},
		{
			name: "blockquote fallback",
			content: `# Title

> Short summary line.
`,
			want: "Short summary line.",
		},
		{
			name:    "empty",
			content: "",
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parsePlaybookDescription(tc.content); got != tc.want {
				t.Fatalf("parsePlaybookDescription() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHousekeepingCreatesMissingPlaybooks(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	if err := os.RemoveAll(store.playbooksPath()); err != nil {
		t.Fatalf("remove playbooks dir: %v", err)
	}

	service := NewService(store, nil, nil)
	report, err := service.Housekeeping()
	if err != nil {
		t.Fatalf("Housekeeping() error = %v", err)
	}
	if len(report.ChangedPaths) == 0 {
		t.Fatalf("expected repaired workspace report: %+v", report)
	}
	for _, path := range seedPlaybookPaths() {
		if !containsString(report.ChangedPaths, path) {
			t.Fatalf("changed paths %v missing %q", report.ChangedPaths, path)
		}
	}
	if len(report.PlaybookPaths) != 6 {
		t.Fatalf("playbook paths = %#v", report.PlaybookPaths)
	}
	if _, err := os.Stat(store.playbookPath("delegate-brief.md")); err != nil {
		t.Fatalf("delegate-brief playbook not backfilled: %v", err)
	}
}
