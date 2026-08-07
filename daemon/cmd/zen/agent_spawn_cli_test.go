package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestAgentSpawnHelpExposesModelProfileFlag(t *testing.T) {
	var help bytes.Buffer
	err := runAgentSpawn([]string{"-help"}, &help)
	if err == nil {
		t.Fatal("expected help sentinel error")
	}
	out := help.String()
	for _, want := range []string{
		"-model-profile",
		"Model Profile id override",
		"-profile",
		"lifecycle profile",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("help missing %q:\n%s", want, out)
		}
	}
}

func TestAgentSpawnParsesModelProfileOverrideAndLifecycleProfile(t *testing.T) {
	_, req, err := parseAgentSpawnArgs([]string{
		"-name", "Franklin",
		"-executor", "codex",
		"-cwd", "/repo",
		"-prompt", "hi",
		"-profile", "research",
		"-model-profile", "codex-main",
		"-hidden",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if req.Type != "agent_spawn" {
		t.Fatalf("type=%q", req.Type)
	}
	if req.Profile != "research" {
		t.Fatalf("lifecycle profile=%q", req.Profile)
	}
	if req.ProfileID != "codex-main" {
		t.Fatalf("model profile=%q", req.ProfileID)
	}
	if req.Executor != "codex" || req.Cwd != "/repo" || req.Command != "" {
		t.Fatalf("req=%+v", req)
	}
}

func TestAgentSpawnOmitsModelProfileForExecutorDefault(t *testing.T) {
	_, req, err := parseAgentSpawnArgs([]string{
		"-name", "Franklin",
		"-executor", "codex",
		"-cwd", "/repo",
		"-prompt", "hi",
		"-hidden",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if req.ProfileID != "" {
		t.Fatalf("omitted model-profile must leave ProfileID empty, got %q", req.ProfileID)
	}
	if req.Profile != "implementation" {
		t.Fatalf("lifecycle default=%q", req.Profile)
	}
}

func TestAgentSpawnPreservesExplicitCommandSemantics(t *testing.T) {
	_, req, err := parseAgentSpawnArgs([]string{
		"-name", "Raw",
		"-command", "my-custom-agent --flag",
		"-cwd", "/repo",
		"-prompt", "hi",
		"-hidden",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if req.Command != "my-custom-agent --flag" {
		t.Fatalf("command=%q", req.Command)
	}
	if req.ProfileID != "" {
		t.Fatalf("ProfileID=%q", req.ProfileID)
	}
}
