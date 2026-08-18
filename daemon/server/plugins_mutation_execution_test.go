package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	skillmgmt "github.com/daoleno/zen/daemon/skills"
)

type serverPluginEntry struct {
	pluginID    string
	name        string
	marketplace string
	version     string
	rootPath    string
}

type serverPluginRuntime struct {
	installed map[skillmgmt.PluginHost][]serverPluginEntry
}

func newServerPluginRuntime() *serverPluginRuntime {
	return &serverPluginRuntime{
		installed: map[skillmgmt.PluginHost][]serverPluginEntry{},
	}
}

func (runtime *serverPluginRuntime) List(_ context.Context, host skillmgmt.PluginHost, _ skillmgmt.InventoryOptions, _ bool) ([]byte, error) {
	if host == skillmgmt.PluginHostClaude {
		return json.Marshal(map[string]any{"installed": []any{}, "available": []any{}})
	}
	installed := make([]map[string]any, 0, len(runtime.installed[host]))
	for _, entry := range runtime.installed[host] {
		installed = append(installed, map[string]any{
			"pluginId": entry.pluginID, "name": entry.name,
			"marketplaceName": entry.marketplace, "version": entry.version,
			"installed": true, "enabled": true,
		})
	}
	return json.Marshal(map[string]any{"installed": installed, "available": []any{}})
}

func (runtime *serverPluginRuntime) Execute(_ context.Context, host skillmgmt.PluginHost, args []string, _ skillmgmt.MutationExecutionOptions) (skillmgmt.MutationExecution, error) {
	if host != skillmgmt.PluginHostCodex || len(args) != 4 || args[0] != "plugin" || args[1] != "remove" || args[3] != "--json" {
		return skillmgmt.MutationExecution{}, errors.New("unexpected Plugin manager command")
	}
	pluginID := args[2]
	for index, entry := range runtime.installed[host] {
		if entry.pluginID != pluginID {
			continue
		}
		if err := os.RemoveAll(entry.rootPath); err != nil {
			return skillmgmt.MutationExecution{}, err
		}
		runtime.installed[host] = append(runtime.installed[host][:index], runtime.installed[host][index+1:]...)
		return skillmgmt.MutationExecution{
			Success: true, ExitCode: 0, Output: "Uninstalled " + entry.name + ".", DurationMS: 1,
		}, nil
	}
	return skillmgmt.MutationExecution{Success: false, ExitCode: 1, Output: "not installed", DurationMS: 1}, nil
}

func (runtime *serverPluginRuntime) add(t *testing.T, home, name, marketplace, version string) string {
	t.Helper()
	root := filepath.Join(home, ".codex", "plugins", "cache", marketplace, name, version)
	if err := os.MkdirAll(filepath.Join(root, ".codex-plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"name":"` + name + `","version":"` + version + `"}`)
	if err := os.WriteFile(filepath.Join(root, ".codex-plugin", "plugin.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	runtime.installed[skillmgmt.PluginHostCodex] = append(runtime.installed[skillmgmt.PluginHostCodex], serverPluginEntry{
		pluginID: name + "@" + marketplace, name: name, marketplace: marketplace,
		version: version, rootPath: root,
	})
	return root
}

func pluginWireInput(row map[string]any) map[string]any {
	agents := make([]any, 0)
	for _, agent := range row["agents"].([]any) {
		agents = append(agents, agent)
	}
	return map[string]any{
		"operation": "uninstall", "plugin_id": row["plugin_id"],
		"plugin_host": row["host"], "plugin_source": row["source"],
		"scope": row["scope"], "plugin_copy_id": row["copy_id"],
		"plugin_name": row["name"], "plugin_version": row["version"],
		"root_path": row["root_path"], "canonical_path": row["canonical_path"],
		"allowed_root": row["allowed_root"], "plugin_revision": row["revision"],
		"agents": agents,
	}
}

func TestPluginsWebSocketUninstallUsesReviewedExactIdentityAndReconciles(t *testing.T) {
	home := configureSkillsTestHome(t)
	runtime := newServerPluginRuntime()
	selected := runtime.add(t, home, "remove-proof", "personal", "1.0.0")
	neighbor := runtime.add(t, home, "neighbor", "personal", "1.0.0")
	srv, conn, closeConn := newSkillsMutationTestServer(t, nil)
	defer closeConn()
	srv.pluginRuntime = runtime
	messages := messageSink(t, conn)
	send := func(payload map[string]any) {
		t.Helper()
		if err := conn.WriteJSON(payload); err != nil {
			t.Fatal(err)
		}
	}
	inventory := func(id string, generation int) map[string]any {
		send(map[string]any{"type": "plugins_inventory", "request_id": id, "generation": generation})
		return readUntil(t, messages, "plugins_inventory", id)
	}
	row := pluginInventoryRow(t, inventory("plugins-before", 1), "remove-proof")
	input := pluginWireInput(row)
	commandRequest := map[string]any{"type": "plugin_command", "request_id": "plugin-command"}
	for key, value := range input {
		commandRequest[key] = value
	}
	send(commandRequest)
	command := readUntil(t, messages, "plugin_command", "plugin-command")["command"].(map[string]any)
	for _, key := range []string{"copy_id", "plugin_id", "host", "source", "name", "version", "root_path", "canonical_path", "allowed_root", "revision"} {
		inputKey := map[string]string{
			"copy_id": "plugin_copy_id", "host": "plugin_host", "source": "plugin_source",
			"name": "plugin_name", "version": "plugin_version", "revision": "plugin_revision",
		}[key]
		if inputKey == "" {
			inputKey = key
		}
		if command[key] != input[inputKey] {
			t.Fatalf("reviewed %s = %v, want %v", key, command[key], input[inputKey])
		}
	}
	if command["destructive"] != true {
		t.Fatalf("reviewed command = %+v", command)
	}
	mutation := map[string]any{"type": "plugin_mutation", "request_id": "plugin-mutation"}
	for key, value := range input {
		mutation[key] = value
	}
	send(mutation)
	result := readUntil(t, messages, "plugin_mutation_result", "plugin-mutation")
	if result["success"] != true || result["exit_code"] != float64(0) {
		t.Fatalf("mutation result = %+v", result)
	}
	if _, err := os.Lstat(selected); !os.IsNotExist(err) {
		t.Fatalf("selected Plugin remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(neighbor, ".codex-plugin", "plugin.json")); err != nil {
		t.Fatalf("neighbor changed: %v", err)
	}
	after := inventory("plugins-after", 2)
	if hasPluginInventoryRow(after, "remove-proof") || !hasPluginInventoryRow(after, "neighbor") {
		t.Fatalf("reconciled inventory = %+v", after)
	}
}

func TestPluginsWebSocketRequiresCompleteIdentityAndRejectsDrift(t *testing.T) {
	home := configureSkillsTestHome(t)
	runtime := newServerPluginRuntime()
	runtime.add(t, home, "exact", "personal", "1.0.0")
	srv, conn, closeConn := newSkillsMutationTestServer(t, nil)
	defer closeConn()
	srv.pluginRuntime = runtime
	messages := messageSink(t, conn)
	if err := conn.WriteJSON(map[string]any{
		"type": "plugin_command", "request_id": "missing", "operation": "uninstall",
		"plugin_id": "exact@personal", "plugin_host": "codex", "scope": "user",
	}); err != nil {
		t.Fatal(err)
	}
	missing := readUntil(t, messages, "plugin_command_error", "missing")
	if missing["code"] != "invalid_request" || !strings.Contains(missing["message"].(string), "complete") {
		t.Fatalf("missing identity = %+v", missing)
	}

	if err := conn.WriteJSON(map[string]any{"type": "plugins_inventory", "request_id": "identity", "generation": 1}); err != nil {
		t.Fatal(err)
	}
	row := pluginInventoryRow(t, readUntil(t, messages, "plugins_inventory", "identity"), "exact")
	for _, scenario := range []struct {
		name  string
		field string
		value any
	}{
		{name: "host", field: "plugin_host", value: "claude"},
		{name: "source", field: "plugin_source", value: "cache"},
		{name: "revision", field: "plugin_revision", value: strings.Repeat("d", 64)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			request := map[string]any{"type": "plugin_command", "request_id": "drift-" + scenario.name}
			for key, value := range pluginWireInput(row) {
				request[key] = value
			}
			request[scenario.field] = scenario.value
			if err := conn.WriteJSON(request); err != nil {
				t.Fatal(err)
			}
			response := readUntil(t, messages, "plugin_command_error", "drift-"+scenario.name)
			if response["code"] != "command_rejected" {
				t.Fatalf("drift response = %+v", response)
			}
		})
	}
}

func TestPluginsMutationSupersedeAndDisconnectCancelExecution(t *testing.T) {
	home := configureSkillsTestHome(t)
	runtime := newServerPluginRuntime()
	runtime.add(t, home, "first", "personal", "1.0.0")
	runtime.add(t, home, "second", "personal", "1.0.0")
	probe := newMutationProbe()
	srv, conn, closeConn := newSkillsMutationTestServer(t, nil)
	srv.pluginRuntime = runtime
	srv.pluginMutationExecuteOverride = probe.executePlugin
	messages := messageSink(t, conn)
	if err := conn.WriteJSON(map[string]any{"type": "plugins_inventory", "request_id": "plugins", "generation": 1}); err != nil {
		t.Fatal(err)
	}
	payload := readUntil(t, messages, "plugins_inventory", "plugins")
	inputs := map[string]map[string]any{
		"first":  pluginWireInput(pluginInventoryRow(t, payload, "first")),
		"second": pluginWireInput(pluginInventoryRow(t, payload, "second")),
	}
	send := func(requestID, name string) {
		request := map[string]any{"type": "plugin_mutation", "request_id": requestID}
		for key, value := range inputs[name] {
			request[key] = value
		}
		if err := conn.WriteJSON(request); err != nil {
			t.Fatal(err)
		}
	}
	send("plugin-first", "first")
	waitProbeStarted(t, probe)
	send("plugin-second", "second")
	waitProbeStarted(t, probe)
	waitFor(t, func() bool { return probe.cancelled.Load() == 1 }, "first Plugin mutation was not canceled")
	if response := readUntil(t, messages, "plugin_mutation_error", "plugin-first"); response["code"] != "superseded" {
		t.Fatalf("superseded response = %+v", response)
	}
	closeConn()
	waitFor(t, func() bool { return probe.cancelled.Load() == 2 }, "disconnect did not cancel Plugin mutation")
	expectNoMessage(t, messages, "plugin_mutation_result")
}

func (p *mutationProbe) executePlugin(ctx context.Context, _ skillmgmt.PluginMutationCommand, _ skillmgmt.MutationExecutionOptions) (skillmgmt.MutationExecution, error) {
	p.executed.Add(1)
	p.started <- struct{}{}
	select {
	case <-p.release:
		return skillmgmt.MutationExecution{Success: true, ExitCode: 0, Output: "ok", DurationMS: 1}, nil
	case <-ctx.Done():
		p.cancelled.Add(1)
		return skillmgmt.MutationExecution{}, skillmgmt.ErrMutationCancelled
	}
}

func pluginInventoryRow(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()
	inventory := payload["inventory"].(map[string]any)
	for _, raw := range inventory["installed"].([]any) {
		row := raw.(map[string]any)
		if row["name"] == name {
			return row
		}
	}
	t.Fatalf("Plugin %q not found in %+v", name, payload)
	return nil
}

func hasPluginInventoryRow(payload map[string]any, name string) bool {
	inventory := payload["inventory"].(map[string]any)
	for _, raw := range inventory["installed"].([]any) {
		if raw.(map[string]any)["name"] == name {
			return true
		}
	}
	return false
}
