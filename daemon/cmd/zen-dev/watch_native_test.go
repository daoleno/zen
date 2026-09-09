package main

import (
	"path/filepath"
	"runtime"
	"testing"
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
	_ = filepath.Separator
}
