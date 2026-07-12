package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDefaultVersionMatchesAppBase(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// daemon/cmd/zen -> repo root
	root := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", ".."))
	basePath := filepath.Join(root, "app", "app.base.json")
	raw, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatalf("read app.base.json: %v", err)
	}
	var doc struct {
		Expo struct {
			Version string `json:"version"`
			Android struct {
				Package     string `json:"package"`
				VersionCode int    `json:"versionCode"`
			} `json:"android"`
		} `json:"expo"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse app.base.json: %v", err)
	}
	if doc.Expo.Version != Version {
		t.Fatalf("Version %q != app.base.json expo.version %q", Version, doc.Expo.Version)
	}
	if doc.Expo.Android.Package != "com.daoleno.zen" {
		t.Fatalf("unexpected package %q", doc.Expo.Android.Package)
	}
	if doc.Expo.Android.VersionCode != 2 {
		t.Fatalf("unexpected versionCode %d", doc.Expo.Android.VersionCode)
	}
}

func TestStartupBannerIncludesVersion(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	printStartupBanner(&b, "127.0.0.1:9876", "0123456789abcdefghijklmnop")
	out := b.String()
	if !strings.Contains(out, "zen v"+Version) {
		t.Fatalf("banner missing version: %q", out)
	}
	if !strings.Contains(out, "127.0.0.1:9876") {
		t.Fatalf("banner missing listen addr: %q", out)
	}
}
