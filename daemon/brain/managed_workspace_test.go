package brain

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewStoreCreatesExactlyOneCanonicalManagedBlock(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, spec := range store.managedMarkdownSpecs() {
		raw := mustReadFile(t, spec.path)
		want := append(append([]byte{}, canonicalManagedBlock(spec)...), '\n')
		if !bytes.Equal(raw, want) {
			t.Fatalf("%s bytes differ from the canonical block:\n%s", spec.relativePath, raw)
		}
		if strings.Count(string(raw), managedStartMarker(spec.managedID)) != 1 ||
			strings.Count(string(raw), managedEndMarker(spec.managedID)) != 1 {
			t.Fatalf("%s does not contain exactly one marker pair:\n%s", spec.relativePath, raw)
		}
		assertFileMode(t, spec.path, 0o600)
	}
	if got := mustReadFile(t, store.profileNotesPath()); string(got) != defaultProfileNotes {
		t.Fatalf("profile.md = %q, want %q", got, defaultProfileNotes)
	}
}

func TestNewStorePreservesUnmarkedDocumentAndAppendsCanonicalBlock(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "AGENTS.md")
	custom := []byte("# Existing Workspace Notes\n\nKeep every old and user-authored byte.  \n")
	if err := os.WriteFile(path, custom, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	spec := store.managedMarkdownSpecs()[0]
	want := appendManagedBlock(custom, canonicalManagedBlock(spec))
	if got := mustReadFile(t, path); !bytes.Equal(got, want) {
		t.Fatalf("unmarked AGENTS.md was not preserved\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestNewStoreUpgradesMarkedBlockInPlaceAndSecondRunIsExact(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "AGENTS.md")
	prefix := []byte("# User Before\n\n")
	suffix := []byte("\n\n# User After\n\nKeep this exact suffix.  \n")
	oldBlock := []byte(managedStartMarker(brainAgentsManagedID) + "\n# Old Product Block\n" + managedEndMarker(brainAgentsManagedID))
	input := bytes.Join([][]byte{prefix, oldBlock, suffix}, nil)
	if err := os.WriteFile(path, input, 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	spec := store.managedMarkdownSpecs()[0]
	want := bytes.Join([][]byte{prefix, canonicalManagedBlock(spec), suffix}, nil)
	first := mustReadFile(t, path)
	if !bytes.Equal(first, want) {
		t.Fatalf("marked upgrade changed outer bytes\ngot:\n%s\nwant:\n%s", first, want)
	}
	assertFileMode(t, path, 0o600)

	fixed := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := os.Chtimes(path, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, path); !bytes.Equal(got, first) {
		t.Fatal("second NewStore changed canonical AGENTS.md bytes")
	}
	assertFileMtime(t, path, fixed)
}

func TestNewStoreConsolidatesMarkedBlocksAndPreservesAllExteriorBytes(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "AGENTS.md")
	oldBlock := []byte(managedStartMarker(brainAgentsManagedID) + "\nold\n" + managedEndMarker(brainAgentsManagedID))
	prefix := []byte("USER BEFORE\n")
	between := []byte("\nUSER BETWEEN BLOCKS\n")
	suffix := []byte("\nUSER AFTER\n")
	input := bytes.Join([][]byte{prefix, oldBlock, between, oldBlock, suffix}, nil)
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Join([][]byte{prefix, canonicalManagedBlock(store.managedMarkdownSpecs()[0]), between, suffix}, nil)
	if got := mustReadFile(t, path); !bytes.Equal(got, want) {
		t.Fatalf("consolidation changed exterior bytes\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestManagedMarkerValidationRejectsCorruptionAndForeignIDs(t *testing.T) {
	tests := map[string]string{
		"missing end": managedStartMarker(brainAgentsManagedID) + "\nbody\n",
		"stray end":   managedEndMarker(brainAgentsManagedID) + "\n",
		"nested": managedStartMarker(brainAgentsManagedID) + "\n" +
			managedStartMarker(brainAgentsManagedID) + "\n" +
			managedEndMarker(brainAgentsManagedID) + "\n",
		"malformed line":  managedStartMarker(brainAgentsManagedID) + " trailing text\n",
		"foreign id":      managedStartMarker(handoffManagedID) + "\nforeign\n" + managedEndMarker(handoffManagedID) + "\n",
		"embedded prefix": "User prose mentions " + managedMarkerPrefix + "agents:start --> inline.\n",
	}
	for name, malformed := range tests {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "workspace")
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(workspace, "AGENTS.md")
			original := []byte(malformed)
			if err := os.WriteFile(path, original, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := NewStore(root); err == nil {
				t.Fatal("NewStore accepted an invalid managed marker")
			}
			if got := mustReadFile(t, path); !bytes.Equal(got, original) {
				t.Fatalf("invalid document changed:\n%s", got)
			}
		})
	}
}

func TestManagedWorkspaceLateFailureNeverWritesEarlierDocument(t *testing.T) {
	t.Run("construction", func(t *testing.T) {
		root, agentsPath, handoffPath, original, fixed := lateFailureFixture(t)
		if _, err := NewStore(root); err == nil {
			t.Fatal("NewStore accepted a foreign handoff marker")
		}
		assertBytesAndMtime(t, agentsPath, original, fixed)
		if !bytes.Contains(mustReadFile(t, handoffPath), []byte(managedStartMarker(brainAgentsManagedID))) {
			t.Fatal("foreign handoff marker unexpectedly changed")
		}
	})

	t.Run("housekeeping", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		agentsPath := store.workspaceInstructionsPath()
		original := staleManagedAgents()
		if err := os.WriteFile(agentsPath, original, 0o600); err != nil {
			t.Fatal(err)
		}
		foreign := []byte(managedStartMarker(brainAgentsManagedID) + "\nforeign\n" + managedEndMarker(brainAgentsManagedID) + "\n")
		if err := os.WriteFile(store.policyPath("handoff.md"), foreign, 0o600); err != nil {
			t.Fatal(err)
		}
		fixed := time.Date(2002, 3, 4, 5, 6, 7, 0, time.UTC)
		if err := os.Chtimes(agentsPath, fixed, fixed); err != nil {
			t.Fatal(err)
		}
		if _, err := NewService(store, nil, nil).Housekeeping(); err == nil {
			t.Fatal("Housekeeping accepted a foreign handoff marker")
		}
		assertBytesAndMtime(t, agentsPath, original, fixed)
	})
}

func TestNewStorePreservesEveryExistingNonEmptyProfile(t *testing.T) {
	profiles := map[string][]byte{
		"minimal heading":  []byte("# Brain Profile\n\n"),
		"whitespace":       []byte(" \n\t\n"),
		"old looking text": []byte("# Brain Profile\n\n## Voice\n\nKnown working preferences: keep this.  \n"),
	}
	for name, profile := range profiles {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			workspace := filepath.Join(root, "workspace")
			if err := os.MkdirAll(workspace, 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(workspace, "profile.md")
			if err := os.WriteFile(path, profile, 0o600); err != nil {
				t.Fatal(err)
			}
			fixed := time.Date(2003, 4, 5, 6, 7, 8, 0, time.UTC)
			if err := os.Chtimes(path, fixed, fixed); err != nil {
				t.Fatal(err)
			}
			if _, err := NewStore(root); err != nil {
				t.Fatal(err)
			}
			assertBytesAndMtime(t, path, profile, fixed)
		})
	}
}

func TestNewStoreInitializesExactlyEmptyProfile(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "profile.md")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(root); err != nil {
		t.Fatal(err)
	}
	if got := mustReadFile(t, path); string(got) != defaultProfileNotes {
		t.Fatalf("empty profile = %q, want %q", got, defaultProfileNotes)
	}
}

func TestHousekeepingReportsOnlySortedChangedPaths(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.workspaceInstructionsPath(), []byte("custom AGENTS\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(store.currentPath()); err != nil {
		t.Fatal(err)
	}
	service := NewService(store, nil, nil)

	first, err := service.Housekeeping()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"AGENTS.md", "current.md"}
	if !equalStrings(first.ChangedPaths, want) {
		t.Fatalf("first changed paths = %v, want %v", first.ChangedPaths, want)
	}
	second, err := service.Housekeeping()
	if err != nil {
		t.Fatal(err)
	}
	if len(second.ChangedPaths) != 0 {
		t.Fatalf("second changed paths = %v, want none", second.ChangedPaths)
	}
}

func lateFailureFixture(t *testing.T) (root, agentsPath, handoffPath string, agents []byte, fixed time.Time) {
	t.Helper()
	root = t.TempDir()
	policies := filepath.Join(root, "workspace", "policies")
	if err := os.MkdirAll(policies, 0o700); err != nil {
		t.Fatal(err)
	}
	agentsPath = filepath.Join(root, "workspace", "AGENTS.md")
	handoffPath = filepath.Join(policies, "handoff.md")
	agents = staleManagedAgents()
	if err := os.WriteFile(agentsPath, agents, 0o600); err != nil {
		t.Fatal(err)
	}
	foreign := []byte(managedStartMarker(brainAgentsManagedID) + "\nforeign\n" + managedEndMarker(brainAgentsManagedID) + "\n")
	if err := os.WriteFile(handoffPath, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	fixed = time.Date(2004, 5, 6, 7, 8, 9, 0, time.UTC)
	if err := os.Chtimes(agentsPath, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	return root, agentsPath, handoffPath, agents, fixed
}

func staleManagedAgents() []byte {
	return []byte(managedStartMarker(brainAgentsManagedID) + "\nold product bytes\n" + managedEndMarker(brainAgentsManagedID) + "\n")
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return raw
}

func assertBytesAndMtime(t *testing.T, path string, want []byte, mtime time.Time) {
	t.Helper()
	if got := mustReadFile(t, path); !bytes.Equal(got, want) {
		t.Fatalf("%s bytes changed\ngot:\n%s\nwant:\n%s", path, got, want)
	}
	assertFileMtime(t, path, mtime)
}

func assertFileMtime(t *testing.T, path string, want time.Time) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(want) {
		t.Fatalf("%s mtime = %s, want %s", path, info.ModTime(), want)
	}
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s mode = %o, want %o", path, info.Mode().Perm(), want)
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
