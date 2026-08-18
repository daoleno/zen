package skills

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fakePluginRuntime struct {
	installed map[PluginHost][]managerInstalledPlugin
	available map[PluginHost][]AvailablePlugin
	listErr   map[PluginHost]error
	executed  []fakePluginExecution
	failure   string
}

type fakePluginExecution struct {
	host PluginHost
	args []string
}

type concurrentPluginRuntime struct {
	started chan PluginHost
	release chan struct{}
}

func newFakePluginRuntime() *fakePluginRuntime {
	return &fakePluginRuntime{
		installed: map[PluginHost][]managerInstalledPlugin{},
		available: map[PluginHost][]AvailablePlugin{},
		listErr:   map[PluginHost]error{},
	}
}

func (runtime *concurrentPluginRuntime) List(_ context.Context, host PluginHost, _ InventoryOptions, _ bool) ([]byte, error) {
	runtime.started <- host
	<-runtime.release
	return []byte(`{"installed":[],"available":[]}`), nil
}

func (runtime *concurrentPluginRuntime) Execute(context.Context, PluginHost, []string, MutationExecutionOptions) (MutationExecution, error) {
	return MutationExecution{}, errors.New("unexpected execution")
}

func (runtime *fakePluginRuntime) List(_ context.Context, host PluginHost, _ InventoryOptions, includeAvailable bool) ([]byte, error) {
	if err := runtime.listErr[host]; err != nil {
		return nil, err
	}
	if host == PluginHostClaude {
		envelope := map[string]any{"installed": []any{}, "available": []any{}}
		installed := envelope["installed"].([]any)
		for _, entry := range runtime.installed[host] {
			installed = append(installed, map[string]any{
				"id": entry.pluginID, "version": entry.version, "scope": "user",
				"enabled": entry.enabled, "installPath": entry.rootPath,
			})
		}
		envelope["installed"] = installed
		if includeAvailable {
			available := envelope["available"].([]any)
			for _, entry := range runtime.available[host] {
				available = append(available, map[string]any{
					"pluginId": entry.PluginID, "name": entry.Name,
					"marketplaceName": entry.MarketplaceName,
					"description":     entry.Description,
				})
			}
			envelope["available"] = available
		}
		return json.Marshal(envelope)
	}
	envelope := map[string]any{"installed": []any{}, "available": []any{}}
	installed := envelope["installed"].([]any)
	for _, entry := range runtime.installed[host] {
		installed = append(installed, map[string]any{
			"pluginId": entry.pluginID, "name": entry.name,
			"marketplaceName": entry.marketplace, "version": entry.version,
			"installed": true, "enabled": entry.enabled,
		})
	}
	envelope["installed"] = installed
	if includeAvailable {
		available := envelope["available"].([]any)
		for _, entry := range runtime.available[host] {
			available = append(available, map[string]any{
				"pluginId": entry.PluginID, "name": entry.Name,
				"marketplaceName": entry.MarketplaceName, "version": entry.Version,
				"installed": false, "enabled": false,
			})
		}
		envelope["available"] = available
	}
	return json.Marshal(envelope)
}

func (runtime *fakePluginRuntime) Execute(_ context.Context, host PluginHost, args []string, options MutationExecutionOptions) (MutationExecution, error) {
	runtime.executed = append(runtime.executed, fakePluginExecution{host: host, args: append([]string{}, args...)})
	if runtime.failure == "nonzero" {
		return MutationExecution{Success: false, ExitCode: 1, Output: "manager rejected removal"}, nil
	}
	pluginID := ""
	operation := ""
	if len(args) >= 3 {
		operation = args[1]
		pluginID = args[2]
	}
	if operation == "install" || operation == "add" {
		for _, entry := range runtime.available[host] {
			if entry.PluginID != pluginID {
				continue
			}
			name, marketplace, _ := splitPluginID(pluginID)
			root := filepath.Join(pluginCacheRoot(options.InventoryOptions, host), marketplace, name, entry.Version)
			if host == PluginHostClaude {
				root = filepath.Join(pluginCacheRoot(options.InventoryOptions, host), marketplace, name, entry.Version)
			}
			if err := writePluginFixture(root, host, name, entry.Version); err != nil {
				return MutationExecution{}, err
			}
			runtime.installed[host] = append(runtime.installed[host], managerInstalledPlugin{
				pluginID: pluginID, name: name, marketplace: marketplace,
				version: entry.Version, enabled: true, host: host, rootPath: root,
			})
			return MutationExecution{Success: true, ExitCode: 0}, nil
		}
		return MutationExecution{Success: false, ExitCode: 1, Output: "not available"}, nil
	}
	before := append([]managerInstalledPlugin{}, runtime.installed[host]...)
	for index, entry := range runtime.installed[host] {
		if entry.pluginID != pluginID {
			continue
		}
		root := entry.rootPath
		if root == "" {
			root = filepath.Join(pluginCacheRoot(options.InventoryOptions, host), entry.marketplace, entry.name, entry.version)
		}
		if runtime.failure == "rollback" {
			runtime.installed[host] = append(runtime.installed[host][:index], runtime.installed[host][index+1:]...)
			runtime.installed[host] = before
			return MutationExecution{}, errors.New("manager rolled back failed removal")
		}
		if runtime.failure == "partial" {
			if err := os.RemoveAll(root); err != nil {
				return MutationExecution{}, err
			}
			return MutationExecution{Success: false, ExitCode: 1, Output: "manager failed after deleting files"}, nil
		}
		if err := os.RemoveAll(root); err != nil {
			return MutationExecution{}, err
		}
		runtime.installed[host] = append(runtime.installed[host][:index], runtime.installed[host][index+1:]...)
		if runtime.failure == "neighbor" && len(runtime.installed[host]) > 0 {
			neighbor := runtime.installed[host][0]
			neighborRoot := neighbor.rootPath
			if neighborRoot == "" {
				neighborRoot = filepath.Join(pluginCacheRoot(options.InventoryOptions, host), neighbor.marketplace, neighbor.name, neighbor.version)
			}
			_ = os.RemoveAll(neighborRoot)
			runtime.installed[host] = runtime.installed[host][1:]
		}
		return MutationExecution{Success: true, ExitCode: 0}, nil
	}
	return MutationExecution{Success: false, ExitCode: 1, Output: "not installed"}, nil
}

func pluginTestOptions(t *testing.T) InventoryOptions {
	t.Helper()
	home := t.TempDir()
	return InventoryOptions{
		Context:     context.Background(),
		Home:        home,
		CodexHome:   filepath.Join(home, ".codex"),
		ClaudeHome:  filepath.Join(home, ".claude"),
		ZenStateDir: filepath.Join(home, ".zen"),
		Now:         func() time.Time { return time.Date(2026, 8, 18, 1, 2, 3, 0, time.UTC) },
	}
}

func addManagedPlugin(t *testing.T, runtime *fakePluginRuntime, options InventoryOptions, host PluginHost, name, marketplace, version string) managerInstalledPlugin {
	t.Helper()
	root := filepath.Join(pluginCacheRoot(options, host), marketplace, name, version)
	if err := writePluginFixture(root, host, name, version); err != nil {
		t.Fatal(err)
	}
	entry := managerInstalledPlugin{
		pluginID: name + "@" + marketplace, name: name, marketplace: marketplace,
		version: version, enabled: true, host: host, rootPath: root,
	}
	runtime.installed[host] = append(runtime.installed[host], entry)
	return entry
}

func writePluginFixture(root string, host PluginHost, name, version string) error {
	manifestDir := filepath.Join(root, ".codex-plugin")
	if host == PluginHostClaude {
		manifestDir = filepath.Join(root, ".claude-plugin")
	}
	if err := os.MkdirAll(filepath.Join(root, "skills", "helper"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(manifestDir, 0o755); err != nil {
		return err
	}
	manifest, _ := json.Marshal(map[string]any{
		"name": name, "version": version, "description": name + " description",
		"interface": map[string]any{"displayName": strings.ToUpper(name[:1]) + name[1:]},
	})
	if err := os.WriteFile(filepath.Join(manifestDir, "plugin.json"), manifest, 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "skills", "helper", "SKILL.md"), []byte("# Helper\n"), 0o644)
}

func requestFor(copy InstalledPluginCopy) PluginMutationRequest {
	return PluginMutationRequest{
		Operation: PluginOperationUninstall, PluginID: copy.PluginID,
		Host: copy.Host, Source: copy.Source, Scope: copy.Scope,
		CopyID: copy.CopyID, Name: copy.Name, Version: copy.Version,
		RootPath: copy.RootPath, CanonicalPath: copy.CanonicalPath,
		AllowedRoot: copy.AllowedRoot, Revision: copy.Revision,
		Agents: append([]Agent{}, copy.Agents...),
	}
}

func TestDiscoverPluginInventoryEmitsExactManagedAndReadonlyCopies(t *testing.T) {
	options := pluginTestOptions(t)
	runtime := newFakePluginRuntime()
	addManagedPlugin(t, runtime, options, PluginHostClaude, "shared", "official", "1.0.0")
	addManagedPlugin(t, runtime, options, PluginHostCodex, "shared", "personal", "2.0.0")
	remoteRoot := filepath.Join(options.CodexHome, "plugins", "cache", "openai-curated-remote", "plugin-management")
	if err := os.MkdirAll(remoteRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(remoteRoot, ".codex-remote-plugin-install.json"), []byte(`{"schema_version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePluginFixture(filepath.Join(remoteRoot, "0.1.0"), PluginHostCodex, "plugin-management", "0.1.0"); err != nil {
		t.Fatal(err)
	}

	inventory, err := DiscoverPluginInventory(options, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Installed) != 3 {
		t.Fatalf("installed = %#v", inventory.Installed)
	}
	ids := map[string]bool{}
	for _, copy := range inventory.Installed {
		if ids[copy.CopyID] || len(copy.CopyID) != 24 || len(copy.Revision) != 64 {
			t.Fatalf("bad exact identity: %#v", copy)
		}
		ids[copy.CopyID] = true
		if copy.Name == "shared" {
			if !copy.Capability.CanUninstall || len(copy.Components) != 1 || copy.Description == "" {
				t.Fatalf("managed copy = %#v", copy)
			}
			expectedAgent := pluginHostAgent(copy.Host)
			if !slices.Equal(copy.Agents, []Agent{expectedAgent}) {
				t.Fatalf("agents = %#v", copy.Agents)
			}
		}
		if copy.Name == "plugin-management" {
			if copy.Source != PluginSourceRemoteCache || copy.Capability.CanUninstall || !strings.Contains(copy.Capability.Reason, "cannot be removed") {
				t.Fatalf("remote copy = %#v", copy)
			}
		}
	}
}

func TestDiscoverPluginInventoryReadsAgentManagersConcurrently(t *testing.T) {
	options := pluginTestOptions(t)
	runtime := &concurrentPluginRuntime{
		started: make(chan PluginHost, 2),
		release: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		_, err := DiscoverPluginInventory(options, runtime)
		done <- err
	}()

	started := map[PluginHost]bool{}
	for range 2 {
		select {
		case host := <-runtime.started:
			started[host] = true
		case <-time.After(time.Second):
			t.Fatal("Plugin managers were read sequentially")
		}
	}
	close(runtime.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !started[PluginHostClaude] || !started[PluginHostCodex] {
		t.Fatalf("started = %#v", started)
	}
}

func TestPluginManagerGapLeavesCacheReadonly(t *testing.T) {
	options := pluginTestOptions(t)
	runtime := newFakePluginRuntime()
	runtime.listErr[PluginHostCodex] = ErrPluginCLIUnavailable
	root := filepath.Join(options.CodexHome, "plugins", "cache", "personal", "orphan", "1.0.0")
	if err := writePluginFixture(root, PluginHostCodex, "orphan", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	inventory, err := DiscoverPluginInventory(options, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Installed) != 1 || inventory.Installed[0].Source != PluginSourceCache || inventory.Installed[0].Capability.CanUninstall {
		t.Fatalf("inventory = %#v", inventory)
	}
	if len(inventory.Warnings) == 0 || !strings.Contains(inventory.Warnings[0], "read-only") {
		t.Fatalf("warnings = %#v", inventory.Warnings)
	}
}

func TestBuildPluginMutationCommandRequiresExactCurrentCopy(t *testing.T) {
	options := pluginTestOptions(t)
	runtime := newFakePluginRuntime()
	addManagedPlugin(t, runtime, options, PluginHostCodex, "alpha", "personal", "1.0.0")
	inventory, err := DiscoverPluginInventory(options, runtime)
	if err != nil {
		t.Fatal(err)
	}
	copy := inventory.Installed[0]
	command, err := BuildPluginMutationCommand(options, requestFor(copy), runtime)
	if err != nil {
		t.Fatal(err)
	}
	if command.CopyID != copy.CopyID || command.Revision != copy.Revision || !command.Destructive || command.Host != PluginHostCodex {
		t.Fatalf("command = %#v", command)
	}

	mutations := []struct {
		name string
		edit func(*PluginMutationRequest)
	}{
		{"copy", func(request *PluginMutationRequest) { request.CopyID = strings.Repeat("f", 24) }},
		{"name", func(request *PluginMutationRequest) { request.Name = "beta" }},
		{"root", func(request *PluginMutationRequest) { request.RootPath = filepath.Join(copy.AllowedRoot, "2.0.0") }},
		{"canonical", func(request *PluginMutationRequest) { request.CanonicalPath = filepath.Join(copy.AllowedRoot, "2.0.0") }},
		{"allowed", func(request *PluginMutationRequest) { request.AllowedRoot = filepath.Dir(copy.AllowedRoot) }},
		{"host", func(request *PluginMutationRequest) { request.Host = PluginHostClaude }},
		{"source", func(request *PluginMutationRequest) { request.Source = PluginSourceCache }},
		{"version", func(request *PluginMutationRequest) { request.Version = "2.0.0" }},
		{"agent", func(request *PluginMutationRequest) { request.Agents = []Agent{AgentClaudeCode} }},
		{"revision", func(request *PluginMutationRequest) { request.Revision = strings.Repeat("a", 64) }},
	}
	for _, scenario := range mutations {
		t.Run(scenario.name, func(t *testing.T) {
			request := requestFor(copy)
			scenario.edit(&request)
			if _, err := BuildPluginMutationCommand(options, request, runtime); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestBuildPluginMutationRejectsSymlinkAndRootEscape(t *testing.T) {
	options := pluginTestOptions(t)
	runtime := newFakePluginRuntime()
	outside := filepath.Join(options.Home, "outside")
	if err := writePluginFixture(outside, PluginHostClaude, "linked", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	allowed := filepath.Join(options.ClaudeHome, "plugins", "cache", "official", "linked")
	if err := os.MkdirAll(allowed, 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(allowed, "1.0.0")
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	runtime.installed[PluginHostClaude] = []managerInstalledPlugin{{
		pluginID: "linked@official", name: "linked", marketplace: "official",
		version: "1.0.0", enabled: true, host: PluginHostClaude, rootPath: root,
	}}
	inventory, err := DiscoverPluginInventory(options, runtime)
	if err != nil {
		t.Fatal(err)
	}
	copy := inventory.Installed[0]
	if copy.Capability.CanUninstall {
		t.Fatalf("symlink capability = %#v", copy.Capability)
	}
	if _, err := BuildPluginMutationCommand(options, requestFor(copy), runtime); err == nil {
		t.Fatal("expected symlink rejection")
	}

	escapedRoot := filepath.Join(options.Home, "elsewhere", "1.0.0")
	if err := writePluginFixture(escapedRoot, PluginHostClaude, "escaped", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	runtime.installed[PluginHostClaude] = []managerInstalledPlugin{{
		pluginID: "escaped@official", name: "escaped", marketplace: "official",
		version: "1.0.0", enabled: true, host: PluginHostClaude, rootPath: escapedRoot,
	}}
	inventory, err = DiscoverPluginInventory(options, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Installed[0].Capability.CanUninstall {
		t.Fatal("escaped manager path must be read-only")
	}

	linkedMarketplace := filepath.Join(options.CodexHome, "plugins", "cache", "linked-market")
	linkedTarget := filepath.Join(options.Home, "linked-market-target")
	if err := os.MkdirAll(linkedTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(linkedMarketplace), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(linkedTarget, linkedMarketplace); err != nil {
		t.Fatal(err)
	}
	linkedRoot := filepath.Join(linkedMarketplace, "nested", "1.0.0")
	if err := writePluginFixture(linkedRoot, PluginHostCodex, "nested", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	runtime.installed[PluginHostClaude] = nil
	runtime.installed[PluginHostCodex] = []managerInstalledPlugin{{
		pluginID: "nested@linked-market", name: "nested", marketplace: "linked-market",
		version: "1.0.0", enabled: true, host: PluginHostCodex, rootPath: linkedRoot,
	}}
	inventory, err = DiscoverPluginInventory(options, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.Installed[0].Capability.CanUninstall || !strings.Contains(inventory.Installed[0].Capability.Reason, "symlink") {
		t.Fatalf("parent symlink capability = %#v", inventory.Installed[0].Capability)
	}
}

func TestExecutePluginMutationRemovesOnlySelectedCopyAndRefreshes(t *testing.T) {
	options := pluginTestOptions(t)
	runtime := newFakePluginRuntime()
	addManagedPlugin(t, runtime, options, PluginHostCodex, "shared", "personal", "1.0.0")
	addManagedPlugin(t, runtime, options, PluginHostCodex, "shared", "team", "1.0.0")
	inventory, err := DiscoverPluginInventory(options, runtime)
	if err != nil {
		t.Fatal(err)
	}
	target := inventory.Installed[0]
	neighbor := inventory.Installed[1]
	command, err := BuildPluginMutationCommand(options, requestFor(target), runtime)
	if err != nil {
		t.Fatal(err)
	}
	execution, err := ExecutePluginMutationCommand(context.Background(), command, MutationExecutionOptions{
		InventoryOptions: options, PluginRuntime: runtime,
	})
	if err != nil || !execution.Success {
		t.Fatalf("execution = %#v err=%v", execution, err)
	}
	if _, err := os.Stat(target.RootPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target still exists: %v", err)
	}
	if _, err := os.Stat(neighbor.RootPath); err != nil {
		t.Fatalf("neighbor changed: %v", err)
	}
	after, err := DiscoverPluginInventory(options, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Installed) != 1 || after.Installed[0].CopyID != neighbor.CopyID {
		t.Fatalf("after = %#v", after.Installed)
	}
	if len(runtime.executed) != 1 || !slices.Equal(runtime.executed[0].args, []string{"plugin", "remove", target.PluginID, "--json"}) {
		t.Fatalf("executed = %#v", runtime.executed)
	}
}

func TestExecuteClaudePluginUsesOwningManagerArgs(t *testing.T) {
	options := pluginTestOptions(t)
	runtime := newFakePluginRuntime()
	addManagedPlugin(t, runtime, options, PluginHostClaude, "alpha", "official", "1.0.0")
	inventory, _ := DiscoverPluginInventory(options, runtime)
	command, err := BuildPluginMutationCommand(options, requestFor(inventory.Installed[0]), runtime)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecutePluginMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options, PluginRuntime: runtime}); err != nil {
		t.Fatal(err)
	}
	want := []string{"plugin", "uninstall", "alpha@official", "--scope", "user", "--yes"}
	if len(runtime.executed) != 1 || !slices.Equal(runtime.executed[0].args, want) {
		t.Fatalf("executed = %#v", runtime.executed)
	}
}

func TestPluginManagerFailureIsAtomicOrRolledBack(t *testing.T) {
	for _, failure := range []string{"nonzero", "rollback"} {
		t.Run(failure, func(t *testing.T) {
			options := pluginTestOptions(t)
			runtime := newFakePluginRuntime()
			addManagedPlugin(t, runtime, options, PluginHostCodex, "alpha", "personal", "1.0.0")
			inventory, _ := DiscoverPluginInventory(options, runtime)
			copy := inventory.Installed[0]
			command, err := BuildPluginMutationCommand(options, requestFor(copy), runtime)
			if err != nil {
				t.Fatal(err)
			}
			runtime.failure = failure
			execution, execErr := ExecutePluginMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options, PluginRuntime: runtime})
			if failure == "nonzero" && (execErr != nil || execution.Success) {
				t.Fatalf("execution=%#v err=%v", execution, execErr)
			}
			if failure == "rollback" && execErr == nil {
				t.Fatal("expected manager rollback error")
			}
			after, err := DiscoverPluginInventory(options, runtime)
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Installed) != 1 || after.Installed[0].CopyID != copy.CopyID {
				t.Fatalf("registry changed: %#v", after.Installed)
			}
			if _, err := os.Stat(copy.RootPath); err != nil {
				t.Fatalf("filesystem changed: %v", err)
			}
		})
	}
}

func TestPluginManagerFailureDetectsPartialFilesystemMutation(t *testing.T) {
	options := pluginTestOptions(t)
	runtime := newFakePluginRuntime()
	addManagedPlugin(t, runtime, options, PluginHostCodex, "alpha", "personal", "1.0.0")
	inventory, _ := DiscoverPluginInventory(options, runtime)
	copy := inventory.Installed[0]
	command, err := BuildPluginMutationCommand(options, requestFor(copy), runtime)
	if err != nil {
		t.Fatal(err)
	}
	runtime.failure = "partial"
	execution, execErr := ExecutePluginMutationCommand(context.Background(), command, MutationExecutionOptions{
		InventoryOptions: options,
		PluginRuntime:    runtime,
	})
	if execution.Success || execErr == nil || !strings.Contains(execErr.Error(), "reporting failure") {
		t.Fatalf("execution=%#v err=%v", execution, execErr)
	}
}

func TestPluginCommandEnvironmentReplacesIdentityRootsExactlyOnce(t *testing.T) {
	t.Setenv("HOME", "/wrong-home")
	t.Setenv("CODEX_HOME", "/wrong-codex")
	t.Setenv("CLAUDE_CONFIG_DIR", "/wrong-claude")
	env := pluginCommandEnv(InventoryOptions{
		Home:       "/right-home",
		CodexHome:  "/right-codex",
		ClaudeHome: "/right-claude",
		Env:        map[string]string{"ZEN_PLUGIN_TEST": "present"},
	})
	for key, want := range map[string]string{
		"HOME": "/right-home", "CODEX_HOME": "/right-codex",
		"CLAUDE_CONFIG_DIR": "/right-claude", "ZEN_PLUGIN_TEST": "present",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	} {
		count := 0
		for _, entry := range env {
			if strings.HasPrefix(entry, key+"=") {
				count++
				if entry != key+"="+want {
					t.Fatalf("%s = %q", key, entry)
				}
			}
		}
		if count != 1 {
			t.Fatalf("%s count = %d", key, count)
		}
	}
}

func TestExecutePluginMutationDetectsNeighborChanges(t *testing.T) {
	options := pluginTestOptions(t)
	runtime := newFakePluginRuntime()
	addManagedPlugin(t, runtime, options, PluginHostCodex, "alpha", "personal", "1.0.0")
	addManagedPlugin(t, runtime, options, PluginHostCodex, "beta", "personal", "1.0.0")
	inventory, _ := DiscoverPluginInventory(options, runtime)
	command, err := BuildPluginMutationCommand(options, requestFor(inventory.Installed[0]), runtime)
	if err != nil {
		t.Fatal(err)
	}
	runtime.failure = "neighbor"
	if _, err := ExecutePluginMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options, PluginRuntime: runtime}); err == nil || !strings.Contains(err.Error(), "another installed Plugin") {
		t.Fatalf("err = %v", err)
	}
}

func TestPluginInstallCapabilityRemainsManagerOwned(t *testing.T) {
	options := pluginTestOptions(t)
	runtime := newFakePluginRuntime()
	runtime.available[PluginHostCodex] = []AvailablePlugin{{
		PluginID: "alpha@personal", Name: "alpha", DisplayName: "Alpha",
		MarketplaceName: "personal", Version: "1.0.0", Host: PluginHostCodex, Installable: true,
	}}
	command, err := BuildPluginMutationCommand(options, PluginMutationRequest{
		Operation: PluginOperationInstall, PluginID: "alpha@personal", Host: PluginHostCodex, Scope: "user",
	}, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if command.Destructive || command.CopyID != "" || command.Summary != "Install alpha for Codex" {
		t.Fatalf("command = %#v", command)
	}
	if _, err := ExecutePluginMutationCommand(context.Background(), command, MutationExecutionOptions{InventoryOptions: options, PluginRuntime: runtime}); err != nil {
		t.Fatal(err)
	}
	after, err := DiscoverPluginInventory(options, runtime)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Installed) != 1 || after.Installed[0].PluginID != "alpha@personal" {
		t.Fatalf("after = %#v", after.Installed)
	}
}

func TestValidatePluginIDRejectsShellAndTraversalGrammars(t *testing.T) {
	for _, value := range []string{"", "plain", "../bad@market", "bad name@market", "bad@market;rm", "@market", "bad@"} {
		if ValidatePluginID(value) == nil {
			t.Fatalf("accepted %q", value)
		}
	}
	if ValidatePluginID("valid-plugin@personal") != nil {
		t.Fatal("valid identity rejected")
	}
}

func TestPluginCatalogDeadlinePrecedesAppRequestDeadlines(t *testing.T) {
	const appInventoryDeadline = 15 * time.Second
	const appCommandDeadline = 12 * time.Second
	if defaultPluginCLITimeout >= appInventoryDeadline || defaultPluginCLITimeout >= appCommandDeadline {
		t.Fatalf("daemon Plugin deadline %v must precede App request deadlines", defaultPluginCLITimeout)
	}
}
