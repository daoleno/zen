package brain

import (
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/work"
)

func TestHandoffReferencesContextAndOnlyOwnedWorkers(t *testing.T) {
	workers := []WorkerRef{
		{ID: "owned:@1", Status: "running", Delegated: true, Summary: strings.Repeat("private-history", 1000)},
		{ID: "external:@2", Status: "running"},
	}
	prompt := formatHostHandoffPrompt("thread-one", "codex", "grok", "codex", workers)
	for _, required := range []string{"current.md", "AGENTS.md", "policies/handoff.md", "thread-one", "owned:@1", "pending Event identities"} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("handoff missing %q", required)
		}
	}
	for _, excluded := range []string{"private-history", "external:@2", brainWorkerRoleContract} {
		if strings.Contains(prompt, excluded) {
			t.Fatalf("handoff duplicated or leaked %q", excluded)
		}
	}
	if len(prompt) > 1000 {
		t.Fatalf("handoff expanded to %d bytes", len(prompt))
	}
	if !work.IsPrivateHostPrompt(prompt) {
		t.Fatal("compact handoff lost transcript privacy")
	}
	if formatHostHandoffPrompt(" ", "codex", "grok", "codex", workers) != "" {
		t.Fatal("blank thread produced a handoff")
	}
}

func TestPromptOwnersCoverAutonomyWaitingAndVerification(t *testing.T) {
	for _, required := range []string{
		"complete authorized work", "finish independent authorized preparation first",
		"User instructions override skill guidelines within platform constraints",
		"completion or failure event", "Do not repeatedly capture progress",
		"If a lifecycle command fails, report the actual failure once",
		"provider Goals and current.md are not alternate schedulers",
	} {
		if !strings.Contains(productWorkspaceInstructions, required) {
			t.Fatalf("workspace missing %q", required)
		}
	}
	for _, required := range []string{"Scale verification to risk", "required repository gates", "Do not replace a full-task requirement with a passing subset", "Receipt acceptance alone is not active Attempt ownership", "Ambiguous or unknown delivery is no-replay"} {
		if !strings.Contains(productDelegationPolicy, required) {
			t.Fatalf("delegation missing %q", required)
		}
	}
}
