package main

import (
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/control"
)

func TestWorkerPromptKeepsPayloadAndOneExactTurn(t *testing.T) {
	const user = "Fix the parser.\nPreserve this spacing:  a  b"
	prompt, err := spawnPrompt(control.Request{Prompt: user, Profile: "implementation"})
	if err != nil {
		t.Fatal(err)
	}
	result := delegatedLifecyclePayload(prompt, "turn:exact-current")
	if !strings.HasPrefix(result, user+"\n\n") {
		t.Fatal("user bytes changed")
	}
	if assertDelegatedLifecyclePayload(t, result, prompt) != "turn:exact-current" {
		t.Fatal("wrong turn identity")
	}
	if strings.Count(result, "Zen lifecycle protocol:") != 1 || strings.Count(result, "Zen delegated turn contract:") != 1 {
		t.Fatal("duplicate protocol")
	}
	if len(result)-len(user) > 2800 {
		t.Fatalf("Worker overhead=%d bytes", len(result)-len(user))
	}
	for _, required := range []string{"required repository gates", "risk-proportionate", "--phase", "--attention", "never in the project repository", "report resource limits rather than bypassing them"} {
		if !strings.Contains(result, required) {
			t.Fatalf("missing %q", required)
		}
	}
}

func TestGeneratedWorkerPromptDirectRepositoryAndDelivery(t *testing.T) {
	for _, profile := range []string{"quick", "research", "implementation", "long_running"} {
		t.Run(profile, func(t *testing.T) {
			prompt, err := spawnPrompt(control.Request{Prompt: "Integrate the pending changes.", Profile: profile})
			if err != nil {
				t.Fatal(err)
			}
			result := delegatedLifecyclePayload(prompt, "turn:direct-repo")
			for _, required := range []string{
				"Edit the supplied repository and cwd directly by default",
				"preserve unrelated changes",
				"explicit user request, concrete conflicting edits, or a justified necessary isolation reason",
				"Briefly explain the actual reason",
				"concurrent Workers do not necessarily conflict",
				"integration into the owning target repository and requested delivery remain part of completion",
				"A candidate branch or passing tests alone are not a delivered outcome",
			} {
				if !strings.Contains(result, required) {
					t.Errorf("generated prompt missing %q", required)
				}
			}
			if strings.Contains(result, "only for required concurrent-write isolation") {
				t.Fatal("generated prompt retains superseded worktree restriction")
			}
		})
	}
}
