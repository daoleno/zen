package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
	"github.com/daoleno/zen/daemon/work"
)

func TestAgentSessionWireAdvertisesKnownStructuredProvider(t *testing.T) {
	srv := &Server{}
	for _, command := range []string{"codex", "pi --session /tmp/x.jsonl", "opencode --auto"} {
		wire := srv.agentSessionWire(&classifier.Agent{ID: command + "-1", Command: command})
		if wire == nil || !wire.Capabilities.StructuredEvents {
			t.Fatalf("%s capabilities = %#v", command, wire)
		}
	}
}

func TestAgentSessionWireUsesConfiguredExecutorCapabilityForGenericCommand(t *testing.T) {
	srv := &Server{execs: &work.ExecutorConfig{ByName: map[string]work.Executor{
		"future": {
			Name:    "future",
			Command: "future-agent --structured",
			Kind:    work.AgentProviderGrok,
		},
	}}}
	agent := &classifier.Agent{ID: "future-1", Command: "/opt/bin/future-agent"}
	wire := srv.agentSessionWire(agent)
	if wire == nil || !wire.Capabilities.StructuredEvents {
		t.Fatalf("configured generic capabilities = %#v", wire)
	}
	provider := srv.structuredProviderForAgent(agent)
	if provider != work.AgentProviderGrok {
		t.Fatalf("configured provider = %q, want grok", provider)
	}
	conversation, err := work.NewProviderConversationReader().Load(*agent, provider, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Reason != "missing_cwd" {
		t.Fatalf("configured wrapper did not reach grok loader: %#v", conversation)
	}

	payload, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"id":"future-1"`) ||
		!strings.Contains(string(payload), `"capabilities":{"structured_events":true}`) {
		t.Fatalf("agent wire payload = %s", payload)
	}
}

func TestAgentSessionWireDoesNotAdvertisePlainShell(t *testing.T) {
	srv := &Server{execs: &work.ExecutorConfig{ByName: map[string]work.Executor{
		"future": {
			Name:    "future",
			Command: "future-agent --structured",
			Kind:    work.AgentProviderGrok,
		},
	}}}
	wire := srv.agentSessionWire(&classifier.Agent{ID: "shell-1", Command: "zsh"})
	if wire == nil || wire.Capabilities.StructuredEvents {
		t.Fatalf("plain shell capabilities = %#v", wire)
	}
}

func TestAgentSessionWireDoesNotInferStructuredProviderFromShellTitle(t *testing.T) {
	srv := &Server{execs: &work.ExecutorConfig{ByName: map[string]work.Executor{
		"codex": {
			Name:    "codex",
			Command: "codex",
			Kind:    work.AgentProviderCodex,
		},
	}}}
	for _, name := range []string{"Codex notes", "Claude research", "Cursor Agent scratch", "Grok shell"} {
		wire := srv.agentSessionWire(&classifier.Agent{
			ID:      "shell-title",
			Name:    name,
			Command: "zsh",
		})
		if wire == nil || wire.Capabilities.StructuredEvents {
			t.Fatalf("plain shell titled %q capabilities = %#v", name, wire)
		}
	}
}
