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
	if len(result)-len(user) > 2300 {
		t.Fatalf("Worker overhead=%d bytes", len(result)-len(user))
	}
	for _, required := range []string{"required repository gates", "risk-proportionate", "--phase", "--attention", "never in the project repository", "report resource limits rather than bypassing them"} {
		if !strings.Contains(result, required) {
			t.Fatalf("missing %q", required)
		}
	}
}
