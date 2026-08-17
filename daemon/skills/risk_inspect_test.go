package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInspectExactCopyListsFilesAndReturnsDefaultPreview(t *testing.T) {
	f := newFixture(t)
	path := f.writeSkill(f.agentGlobalDir(AgentCodex), "reader", "# Reader\n")
	if err := os.MkdirAll(filepath.Join(path, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "docs", "guide.md"), []byte("# Guide\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, _ := DiscoverInventory(f.options(""))
	copy := findCopy(t, inventory, "reader", path)
	detail, err := InspectPackageCopy(f.options(""), copy.Name, copy.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.CopyID != copy.ID || detail.RootPath != copy.RootPath || detail.AllowedRoot != copy.AllowedRoot || detail.Location == "" {
		t.Fatalf("detail identity = %+v", detail)
	}
	if detail.Preview == nil || detail.Preview.Path != "SKILL.md" || !strings.Contains(detail.Preview.Content, "Reader") {
		t.Fatalf("default preview = %+v", detail.Preview)
	}
	if len(detail.Files) != 2 {
		t.Fatalf("files = %+v", detail.Files)
	}
}

func TestInspectDuplicateNameUsesOpaqueCopyID(t *testing.T) {
	f := newFixture(t)
	aPath := f.writeSkill(f.agentGlobalDir(AgentCodex), "duplicate", "codex body")
	bPath := f.writeSkill(f.agentGlobalDir(AgentPi), "duplicate", "pi body")
	inventory, _ := DiscoverInventory(f.options(""))
	a := findCopy(t, inventory, "duplicate", aPath)
	b := findCopy(t, inventory, "duplicate", bPath)
	if _, err := InspectPackage(f.options(""), "duplicate"); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("ambiguous inspection error = %v", err)
	}
	detail, err := InspectPackageCopy(f.options(""), "duplicate", b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.CopyID != b.ID || detail.CopyID == a.ID || detail.Preview == nil || !strings.Contains(detail.Preview.Content, "pi body") {
		t.Fatalf("selected detail = %+v", detail)
	}
}

func TestInspectFileRejectsTraversalAndSymlinks(t *testing.T) {
	f := newFixture(t)
	path := f.writeSkill(f.agentGlobalDir(AgentClaudeCode), "safe-reader", "body")
	outside := filepath.Join(f.Home, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(path, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	inventory, _ := DiscoverInventory(f.options(""))
	copy := findCopy(t, inventory, "safe-reader", path)
	for _, relative := range []string{"../outside.txt", "/etc/passwd", "escape.txt"} {
		if _, err := InspectPackageCopyFile(f.options(""), copy.Name, copy.ID, relative); err == nil {
			t.Fatalf("unsafe file %q was inspected", relative)
		}
	}
}

func TestInspectPreviewClassifiesBinaryAndTruncatesLargeText(t *testing.T) {
	f := newFixture(t)
	path := f.writeSkill(f.agentGlobalDir(AgentCursor), "formats", "body")
	large := strings.Repeat("x", maxInspectPreviewBytes+100)
	if err := os.WriteFile(filepath.Join(path, "large.txt"), []byte(large), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "blob.bin"), []byte{0, 1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, _ := DiscoverInventory(f.options(""))
	copy := findCopy(t, inventory, "formats", path)
	largeDetail, err := InspectPackageCopyFile(f.options(""), copy.Name, copy.ID, "large.txt")
	if err != nil {
		t.Fatal(err)
	}
	if largeDetail.Preview == nil || largeDetail.Preview.Status != "truncated" || largeDetail.Preview.BytesReturned != maxInspectPreviewBytes {
		t.Fatalf("large preview = %+v", largeDetail.Preview)
	}
	binaryDetail, err := InspectPackageCopyFile(f.options(""), copy.Name, copy.ID, "blob.bin")
	if err != nil {
		t.Fatal(err)
	}
	if binaryDetail.Preview == nil || binaryDetail.Preview.Status != "binary" || binaryDetail.Preview.Content != "" {
		t.Fatalf("binary preview = %+v", binaryDetail.Preview)
	}
}

func TestRiskSignalsRemainReadOnlyDiagnostics(t *testing.T) {
	f := newFixture(t)
	path := f.writeSkill(f.agentGlobalDir(AgentGrok), "risky", "body")
	if err := os.WriteFile(filepath.Join(path, "run.sh"), []byte("#!/bin/sh\ncurl https://example.com\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, ".env"), []byte("TOKEN=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inventory, _ := DiscoverInventory(f.options(""))
	copy := findCopy(t, inventory, "risky", path)
	if len(copy.Risk) == 0 || !copy.Capability.CanDelete {
		t.Fatalf("risk/capability = %+v / %+v", copy.Risk, copy.Capability)
	}
	if _, err := os.Stat(filepath.Join(path, "run.sh")); err != nil {
		t.Fatalf("risk scan mutated package: %v", err)
	}
}

func TestInspectRejectsStaleOrMismatchedCopy(t *testing.T) {
	f := newFixture(t)
	path := f.writeSkill(f.agentGlobalDir(AgentPi), "stale-inspect", "body")
	inventory, _ := DiscoverInventory(f.options(""))
	copy := findCopy(t, inventory, "stale-inspect", path)
	if _, err := InspectPackageCopy(f.options(""), "other", copy.ID); err == nil {
		t.Fatal("mismatched name was accepted")
	}
	if err := os.RemoveAll(path); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectPackageCopy(f.options(""), copy.Name, copy.ID); err == nil {
		t.Fatal("stale copy was accepted")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture cleanup failed: %v", err)
	}
}
