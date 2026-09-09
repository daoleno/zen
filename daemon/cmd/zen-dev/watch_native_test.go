package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/desktop"
)

func TestRelevantPathIncludesNativeSources(t *testing.T) {
	tree := &watchTree{root: "/repo"}
	for _, path := range []string{
		"/repo/desktop/native/linux.c",
		"/repo/desktop/native/encoder.h",
		"/repo/desktop/native/Makefile",
		"/repo/desktop/native/host-agent.mk",
		"/repo/cmd/zen/main.go",
	} {
		rel, ok := tree.relevantPath(path)
		if !ok || rel == "" {
			t.Fatalf("ignored native/Go rebuild path %s", path)
		}
	}
	if _, ok := tree.relevantPath("/repo/tmp/zen-dev"); ok {
		t.Fatal("tmp binary must not trigger rebuild")
	}
	if _, ok := tree.relevantPath("/repo/app/App.tsx"); ok {
		t.Fatal("app TS must not trigger daemon rebuild")
	}
}

func TestDesktopNativeBuildDetectsLinuxPkgConfig(t *testing.T) {
	tags, env, ok := desktopNativeBuild()
	if runtime.GOOS != "linux" {
		if ok {
			t.Fatal("non-linux must not enable zen_desktop")
		}
		return
	}
	if !ok {
		t.Log("pkg-config desktop libraries missing; zen-dev will build daemon-only")
		return
	}
	if len(tags) != 2 || tags[1] != "zen_desktop" {
		t.Fatalf("tags=%v", tags)
	}
	found := false
	for _, item := range env {
		if item == "CGO_ENABLED=1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("env=%v", env)
	}
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	token, err := desktop.NativeBuildInputToken(filepath.Join(filepath.Dir(thisFile), "../../desktop/native"))
	if err != nil || token == "" {
		t.Fatalf("native build input token=%q err=%v", token, err)
	}
	flag := desktop.NativeBuildInputFlag(token)
	merged := desktop.WithNativeBuildInput(append([]string{}, env...), token)
	cflags := ""
	for _, item := range merged {
		if len(item) > 11 && item[:11] == "CGO_CFLAGS=" {
			cflags = item[11:]
		}
	}
	if !strings.Contains(cflags, flag) {
		t.Fatalf("structured CGO_CFLAGS=%q want define %q", cflags, flag)
	}
}

func TestNativeSourcesChanged(t *testing.T) {
	if !nativeSourcesChanged([]string{"desktop/native/linux.c"}) {
		t.Fatal("linux.c must force native rebuild")
	}
	if !nativeSourcesChanged([]string{"desktop/native/encoder.h"}) {
		t.Fatal("encoder.h must force native rebuild")
	}
	if nativeSourcesChanged([]string{"cmd/zen/main.go"}) {
		t.Fatal("Go-only edits should not force native rebuild")
	}
}

func TestCommitBuiltBinaryReplacesDest(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "zen-dev")
	built := filepath.Join(dir, "zen-dev.building")
	if err := os.WriteFile(dest, []byte("last-good"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(built, []byte("next"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := commitBuiltBinary(built, dest); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "next" {
		t.Fatalf("dest=%q err=%v", got, err)
	}
	if _, err := os.Stat(built); !os.IsNotExist(err) {
		t.Fatalf("building path should be consumed: %v", err)
	}
}

func TestRebuildLeavesLastGoodWhenBuildFails(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "zen-dev")
	if err := os.WriteFile(dest, []byte("last-good"), 0755); err != nil {
		t.Fatal(err)
	}
	runner := &devRunner{
		root:   dir,
		binary: dest,
		stderr: os.Stderr,
	}
	if err := runner.rebuild(false); err == nil {
		t.Fatal("rebuild in empty module must fail")
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "last-good" {
		t.Fatalf("last-good destroyed: %q err=%v", got, err)
	}
	if _, err := os.Stat(dest + ".building"); !os.IsNotExist(err) {
		t.Fatal("failed build must not leave dest.building as the live binary")
	}
}
