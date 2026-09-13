package work

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/classifier"
)

func TestAmpCustomExecutorContract(t *testing.T) {
	const command = "amp --visibility private --no-ide --no-remote-control-terminal"
	executor := NewWorkerExecutor("amp", Executor{Name: "amp", Command: command})
	if executor.Provider != WorkerProviderCustom || executor.Runtime != WorkerRuntimeTmux || executor.Command != command {
		t.Fatalf("Amp must retain the custom tmux contract: %#v", executor)
	}
	if executor.Capabilities != (WorkerCapabilities{InteractiveTTY: true}) {
		t.Fatalf("Amp gained unimplemented native/structured capabilities: %#v", executor.Capabilities)
	}
	if executor.ProfileClientExecutor() != "" || ProfileClientExecutor(command, "amp") != "" {
		t.Fatal("Amp must not enter a model-profile credential route")
	}
	if _, err := WithProviderResumeToken(executor.Provider, command, "amp-thread"); !errors.Is(err, ErrLaunchUnparseable) {
		t.Fatalf("Amp resume must fail closed, got %v", err)
	}
	conversation, err := NewProviderConversationReader().Load(classifier.Worker{
		ID: "amp-fixture", Command: command, Cwd: t.TempDir(),
	}, executor.Provider, time.Now())
	if err != nil || conversation.Available || conversation.Reason != "not_structured_agent" || len(conversation.Events) != 0 {
		t.Fatalf("Amp must not synthesize conversation events: %#v, %v", conversation, err)
	}
}

func TestAmpCatalogIsExplicitAdditiveAndDoesNotChangeDefaults(t *testing.T) {
	t.Setenv("ZEN_DELEGATED_EXECUTOR", "")
	path := filepath.Join(t.TempDir(), "executors.toml")
	before, err := LoadExecutors(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := before.ByName["amp"]; exists {
		t.Fatal("Amp must not be implicitly registered")
	}
	config := []byte("delegated_executor = \"pi\"\n[[executors]]\nname = \"amp\"\ncommand = \"amp --visibility private --no-ide --no-remote-control-terminal\"\n")
	if err := os.WriteFile(path, config, 0o600); err != nil {
		t.Fatal(err)
	}
	after, err := LoadExecutors(path)
	if err != nil {
		t.Fatal(err)
	}
	for name, executor := range before.ByName {
		if !reflect.DeepEqual(after.ByName[name], executor) {
			t.Fatalf("Amp catalog addition changed peer %s", name)
		}
	}
	if after.GetDelegatedExecutor() != "pi" || len(after.ByName) != len(before.ByName)+1 {
		t.Fatal("Amp addition changed the explicit delegated selection or replaced the catalog")
	}
	persisted, err := os.ReadFile(path)
	if err != nil || string(persisted) != string(config) {
		t.Fatal("catalog load must not mutate configuration")
	}
}
