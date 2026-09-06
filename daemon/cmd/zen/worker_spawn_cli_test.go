package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/control"
)

func TestWorkerSpawnHelpExposesModelProfileFlag(t *testing.T) {
	var help bytes.Buffer
	err := runWorkerSpawn([]string{"-help"}, &help)
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

func TestWorkerSpawnParsesModelProfileOverrideAndLifecycleProfile(t *testing.T) {
	_, req, err := parseWorkerSpawnArgs([]string{
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
	if req.Type != "worker_spawn" {
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

func TestWorkerSpawnOmitsModelProfileForExecutorDefault(t *testing.T) {
	_, req, err := parseWorkerSpawnArgs([]string{
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

func TestWorkerSpawnPreservesExplicitCommandSemantics(t *testing.T) {
	_, req, err := parseWorkerSpawnArgs([]string{
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

func TestWorkerSpawnPendingAdmissionResponseIsSuccessfulCLIJSON(t *testing.T) {
	resp := control.Response{
		OK:           true,
		Confirmation: spawnAdmissionConfirmation(true),
		Worker: &control.Worker{
			ID: "zen-worker-pending:@1", Status: "running", Delegated: true,
		},
	}
	var output bytes.Buffer
	if err := writeControlResponse(&output, resp, true); err != nil {
		t.Fatalf("pending response returned CLI failure: %v", err)
	}
	var decoded control.Response
	if err := json.Unmarshal(output.Bytes(), &decoded); err != nil {
		t.Fatalf("decode pending response: %v\n%s", err, output.String())
	}
	if !decoded.OK || decoded.Error != nil || decoded.Worker == nil || decoded.Worker.Status != "running" ||
		!strings.Contains(decoded.Confirmation, "awaiting exact turn-scoped admission") {
		t.Fatalf("pending CLI/API response = %#v", decoded)
	}
}

func TestWorkerSpawnControlTimeoutContainsBoundedAdmission(t *testing.T) {
	if workerSpawnControlTimeout <= time.Minute {
		t.Fatalf("agent spawn control timeout = %s, must contain startup plus provider admission", workerSpawnControlTimeout)
	}
}
