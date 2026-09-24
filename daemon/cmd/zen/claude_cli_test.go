package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScrubClaudeEnvRemovesInheritedProviderOverrides(t *testing.T) {
	got := scrubClaudeEnv([]string{
		"PATH=/bin",
		"ANTHROPIC_API_KEY=stale",
		"ANTHROPIC_BASE_URL=http://old",
		"CLAUDE_CODE_USE_VERTEX=1",
		"ZEN_CLAUDE_WRAPPER=/tmp/wrapper",
		"HOME=/home/test",
	})
	want := []string{"PATH=/bin", "HOME=/home/test"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("scrubbed env = %q, want %q", got, want)
	}
}

func TestFindNativeClaudeSkipsZenWrapper(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "claude")
	nativeDir := t.TempDir()
	native := filepath.Join(nativeDir, "claude")
	for _, path := range []string{wrapper, native} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+nativeDir)
	t.Setenv("ZEN_CLAUDE_WRAPPER", wrapper)
	got, err := findNativeClaude()
	if err != nil || got != native {
		t.Fatalf("native Claude = %q, err=%v, want %q", got, err, native)
	}
}

func TestShellQuoteProtectsClaudeArguments(t *testing.T) {
	if got := shellQuote("a'b"); got != "'a'\\''b'" {
		t.Fatalf("shell quote = %q", got)
	}
}
