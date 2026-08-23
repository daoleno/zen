package server

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrainSchedulerHasOneRuntimeOwner(t *testing.T) {
	files := []string{
		"server.go",
		filepath.Join("..", "brain", "service.go"),
		filepath.Join("..", "brain", "lifecycle.go"),
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
	if strings.Contains(string(serverSource), "MigrateDelegatedSessionsV1") ||
		strings.Contains(string(serverSource), "MigrateSignalSystemV1") ||
		strings.Contains(string(serverSource), "MigrateTurnLedgerV1") {
		t.Fatal("server runtime still drives legacy scheduler migration")
	}
	brainSource, err := os.ReadFile(filepath.Join("..", "brain", "lifecycle.go"))
	if err != nil {
		t.Fatal(err)
	}
	turnLedgerSource, err := os.ReadFile(filepath.Join("..", "brain", "turn_ledger.go"))
	if err != nil {
		t.Fatal(err)
	}
	brainSource = append(brainSource, turnLedgerSource...)
	for _, required := range []string{
		"ConsumeReviewDelivery",
		"ClaimNextReviewAction",
		"PrepareInputAdmission",
		"AcceptReviewFollowUp",
		"isTurnScopedSessionDedupeKey",
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
	for _, required := range []string{
		"SendInputWithReceipt",
		"InputReceiptResult",
		"marshalDirectWorkEventInput",
		"ConsumeReviewDelivery",
	} {
		if !strings.Contains(string(brainServiceSource), required) {
			t.Fatalf("Brain delivery is missing durable Session receipt boundary %q", required)
		}
	}
	for _, forbidden := range []string{"CapturePaneContent", "eventClaimRecoveryDelay"} {
		if strings.Contains(string(brainServiceSource), forbidden) {
			t.Fatalf("Brain delivery still uses volatile replay authority %q", forbidden)
		}
	}
	for _, forbidden := range []string{
		"dispatchContinuation" + "Due",
		"launchContinuation" + "Session",
		"continuation" + "Prompt",
		"Continue the " + "Work until",
		"AdmissionPurpose" + "Continuation",
	} {
		if strings.Contains(string(brainServiceSource), forbidden) {
			t.Fatalf("Brain service still contains daemon continuation path %q", forbidden)
		}
	}
	assertFunctionsDoNotCall(t, filepath.Join("..", "brain", "service.go"),
		[]string{"RunLifecycleScheduler", "ReconcileDelegatedSessions"}, "CreateSession")

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

func assertFunctionsDoNotCall(t *testing.T, path string, functions []string, selector string) {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	wanted := make(map[string]bool, len(functions))
	for _, name := range functions {
		wanted[name] = true
	}
	for _, declaration := range parsed.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || !wanted[fn.Name.Name] {
			continue
		}
		delete(wanted, fn.Name.Name)
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selected, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selected.Sel.Name == selector {
				t.Errorf("%s calls forbidden lifecycle side effect %s", fn.Name.Name, selector)
			}
			return true
		})
	}
	for name := range wanted {
		t.Errorf("function %s not found in %s", name, path)
	}
}
