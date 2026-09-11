package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNativeBuildInputTokenHashesContentNotMtime(t *testing.T) {
	dir := t.TempDir()
	writeNativeInputs(t, dir, "int zen_fixture_a = 1;\n")
	first, err := NativeBuildInputToken(dir)
	if err != nil || first == "" {
		t.Fatalf("token=%q err=%v", first, err)
	}
	now := time.Now().Add(2 * time.Hour)
	for _, name := range []string{"linux.c", "encoder.h"} {
		if err := os.Chtimes(filepath.Join(dir, name), now, now); err != nil {
			t.Fatal(err)
		}
	}
	second, err := NativeBuildInputToken(dir)
	if err != nil || second != first {
		t.Fatalf("mtime must not change token: %q vs %q err=%v", first, second, err)
	}
	writeNativeInputs(t, dir, "int zen_fixture_a = 2;\n")
	third, err := NativeBuildInputToken(dir)
	if err != nil || third == first {
		t.Fatalf("content change must change token: %q vs %q err=%v", first, third, err)
	}
}

func TestNativeBuildInputTokenIgnoresStandaloneTestC(t *testing.T) {
	dir := t.TempDir()
	writeNativeInputs(t, dir, "int zen_fixture_a = 1;\n")
	first, err := NativeBuildInputToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test-grant-click.c"), []byte("int ignored = 1;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	second, err := NativeBuildInputToken(dir)
	if err != nil || second != first {
		t.Fatalf("standalone test C must not be a cgo input: %q vs %q err=%v", first, second, err)
	}
}

func TestWithNativeBuildInputPreservesCallerCflags(t *testing.T) {
	env := []string{"PATH=/usr/bin", "CGO_CFLAGS=-O1 -std=c11", "HOME=/tmp"}
	got := WithNativeBuildInput(env, "abcd1234")
	var cflags, path, home string
	cflagsCount := 0
	for _, item := range got {
		switch {
		case strings.HasPrefix(item, "CGO_CFLAGS="):
			cflagsCount++
			cflags = strings.TrimPrefix(item, "CGO_CFLAGS=")
		case strings.HasPrefix(item, "PATH="):
			path = item
		case strings.HasPrefix(item, "HOME="):
			home = item
		}
	}
	if cflagsCount != 1 {
		t.Fatalf("CGO_CFLAGS entries=%d env=%v", cflagsCount, got)
	}
	if path != "PATH=/usr/bin" || home != "HOME=/tmp" {
		t.Fatalf("unrelated env mutated: %v", got)
	}
	if !strings.Contains(cflags, "-O1") || !strings.Contains(cflags, "-std=c11") {
		t.Fatalf("caller CFLAGS lost: %q", cflags)
	}
	if !strings.Contains(cflags, NativeBuildInputFlag("abcd1234")) {
		t.Fatalf("missing structured define: %q", cflags)
	}
	if strings.Contains(cflags, "CGO_CFLAGS=") {
		t.Fatalf("nested CGO_CFLAGS: %q", cflags)
	}
}

func writeNativeInputs(t *testing.T, dir, linuxBody string) {
	t.Helper()
	files := map[string]string{
		"linux.c":          linuxBody,
		"host-agent.c":     "int zen_agent = 0;\n",
		"encoder.c":        "int zen_encoder = 0;\n",
		"encoder.h":        "#pragma once\n",
		"portal.c":         "int zen_portal = 0;\n",
		"portal.h":         "#pragma once\n",
		"portal_capture.c": "int zen_portal_capture = 0;\n",
		"portal_capture.h": "#pragma once\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
}
