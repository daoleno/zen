package server

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrainSchedulerHasOneRuntimeOwner(t *testing.T) {
	files := []string{
		"server.go",
		filepath.Join("..", "brain", "service.go"),
		filepath.Join("..", "brain", "orchestration.go"),
		filepath.Join("..", "work", "codex_conversation.go"),
	}
	forbidden := []string{
		"maybeWake" + "Brain",
		"brain" + "Sent",
		"delegated" + "LifecycleManager",
		"Heartbeat " + "wake:",
		".Heart" + "beat(",
		"thread_goal_" + "updated",
		"Goal " + "updated",
		"<codex_internal_context source=\"goal\">",
	}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range forbidden {
			if strings.Contains(string(raw), value) {
				t.Fatalf("%s still contains superseded scheduler path %q", path, value)
			}
		}
	}

	serverSource, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"RouteSessionEvent(ev)",
		"RouteCalendarEvent(event)",
		"ReconcileDelegatedSessions(allAgentSessions)",
		"SanitizeConversationProjection(conversation)",
	} {
		if !strings.Contains(string(serverSource), required) {
			t.Fatalf("server runtime is missing sole-owner boundary %q", required)
		}
	}
	if count := strings.Count(string(serverSource), "MigrateDelegatedSessionsV1(agentSessions)"); count != 1 {
		t.Fatalf("one-way delegated Session migration calls = %d, want 1 source path", count)
	}
	brainSource, err := os.ReadFile(filepath.Join("..", "brain", "orchestration.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"DeliveryHostSessionID",
		"DeliveryAcknowledgedAt",
		"RecoverWorkEventClaim",
		"AttachWorkOwner",
		"ReconcileMissingWorkOwner",
	} {
		if !strings.Contains(string(brainSource), required) {
			t.Fatalf("Brain store is missing bounded correction primitive %q", required)
		}
	}
	if strings.Contains(string(brainSource), "ReleaseWorkEvent") {
		t.Fatal("Brain store still permits eager unacknowledged claim release")
	}
	brainServiceSource, err := os.ReadFile(filepath.Join("..", "brain", "service.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"SendInputWithReceipt", "HasInputReceipt"} {
		if !strings.Contains(string(brainServiceSource), required) {
			t.Fatalf("Brain delivery is missing durable Session receipt boundary %q", required)
		}
	}
	for _, forbidden := range []string{"CapturePaneContent", "eventClaimRecoveryDelay"} {
		if strings.Contains(string(brainServiceSource), forbidden) {
			t.Fatalf("Brain delivery still uses volatile replay authority %q", forbidden)
		}
	}

	for _, removed := range []string{
		"brain_attention_test.go",
		"delegated_lifecycle.go",
		"delegated_lifecycle_test.go",
	} {
		if _, err := os.Stat(removed); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("superseded scheduler file still exists: %s (err=%v)", removed, err)
		}
	}
}
