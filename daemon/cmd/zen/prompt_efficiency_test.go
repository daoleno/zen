package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/work"
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

// These are authored brief examples, not model-generated outputs or a reasoning
// benchmark. Exercise their delivery at the production control boundary.
func TestEngineeringScenarioBriefsReachNativeWorkers(t *testing.T) {
	raw, err := os.ReadFile("../../brain/testdata/engineering-scenarios.json")
	if err != nil {
		t.Fatal(err)
	}
	var scenarios []struct {
		ID           string   `json:"id"`
		Playbooks    []string `json:"playbooks"`
		ExampleBrief string   `json:"example_brief"`
	}
	if err := json.Unmarshal(raw, &scenarios); err != nil {
		t.Fatal(err)
	}
	if len(scenarios) != 5 {
		t.Fatalf("scenario count=%d", len(scenarios))
	}
	for _, provider := range []string{"pi", "codex"} {
		for _, scenario := range scenarios {
			t.Run(provider+"/"+scenario.ID, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("HOME", home)
				store := newControlBrainStore(t)
				catalog, err := store.PlaybookCatalog()
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range scenario.Playbooks {
					found := false
					for _, entry := range catalog.Playbooks {
						if entry.Name == name {
							file, err := store.ReadWorkspaceFile(entry.Path)
							if err != nil || file.Content == "" {
								t.Fatalf("unreadable method %s: %v", name, err)
							}
							found = true
						}
					}
					if !found {
						t.Fatalf("unknown method %s", name)
					}
				}
				if err := store.SetHostSession("brain:@scenario", provider); err != nil {
					t.Fatal(err)
				}
				fw := newFakeControlWatcher()
				fw.turnStore = store
				app := &controlApp{watcher: fw, brainStore: store, execs: work.NewExecutorConfig(provider, map[string]work.Executor{
					provider: {Name: provider, Command: provider, Kind: provider},
				})}
				cwd := filepath.Join(t.TempDir(), "unrelated-project")
				if err := os.Mkdir(cwd, 0o700); err != nil {
					t.Fatal(err)
				}
				resp := app.HandleControlRequest(control.Request{Type: "worker_spawn", WorkerID: "brain:@scenario", Name: scenario.ID, Cwd: cwd, Prompt: scenario.ExampleBrief, Profile: "implementation"})
				if !resp.OK || resp.Worker == nil || !resp.Worker.Delegated || resp.Worker.Hidden || resp.BrainWork == nil || resp.BrainWork.Status != brain.WorkRunning {
					t.Fatalf("delegation did not create visible owned Work: %+v", resp)
				}
				wantCommand := provider
				if provider == "codex" {
					wantCommand += " --dangerously-bypass-approvals-and-sandbox"
				}
				if len(fw.created) != 1 || fw.created[0].Cwd != cwd || len(fw.submitted) != 1 {
					t.Fatalf("native routing changed: created=%+v submitted=%d", fw.created, len(fw.submitted))
				}
				command := fw.created[0].Command
				if provider == "pi" {
					path := work.PiOwnedSessionPath(command)
					if filepath.Dir(path) != filepath.Join(home, ".zen", "provider-sessions", "pi") || !strings.HasPrefix(command, "pi --session ") {
						t.Fatalf("Pi lost native owned session: %s", command)
					}
				} else if command != wantCommand {
					t.Fatalf("Codex command changed: %s", command)
				}
				payload := fw.submitted[0].text
				if !strings.HasPrefix(payload, scenario.ExampleBrief+"\n\n") || len(payload)-len(scenario.ExampleBrief) > 2800 {
					t.Fatal("method-bearing brief was changed or overhead grew")
				}
				for _, excluded := range []string{"pstack", ".agents/zen-verification", "# Wayfind", "# Delegate Brief"} {
					if strings.Contains(payload, excluded) {
						t.Fatalf("brief acquired unrelated instructions %q", excluded)
					}
				}
			})
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
