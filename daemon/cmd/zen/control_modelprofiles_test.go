package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/daoleno/zen/daemon/brain"
	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/modelprofiles"
	"github.com/daoleno/zen/daemon/work"
)

func ptrConn(in modelprofiles.ProviderConnectionInput) *modelprofiles.ProviderConnectionInput {
	return &in
}

func providerConnFromProfile(p modelprofiles.Profile) modelprofiles.ProviderConnectionInput {
	client := modelprofiles.ClientCodex
	switch p.ExecutorID {
	case modelprofiles.ExecutorClaude:
		client = modelprofiles.ClientClaude
	}
	return modelprofiles.ProviderConnectionInput{
		ID: p.ID, Name: p.Name, Client: client,
		PresetID: modelprofiles.ProviderPresetCustom, BaseURL: p.BaseURL, ModelID: p.Model, Advanced: true,
	}
}

type controlProfileVerifier struct{}

func (controlProfileVerifier) VerifyProfileContract(profile modelprofiles.Profile) (modelprofiles.VerifiedProfileContract, error) {
	env := modelprofiles.DefaultTestEnvelope()
	route, _ := modelprofiles.RouteProtocolFor(profile.Protocol)
	return modelprofiles.VerifiedProfileContract{
		Provenance:       modelprofiles.ContractProvenanceBuiltinCatalog,
		ClientModelID:    profile.ClientModel,
		UpstreamModelID:  profile.Model,
		ExecutorID:       profile.ExecutorID,
		Protocol:         profile.Protocol,
		RouteProtocol:    route,
		ProviderID:       profile.ProviderID,
		ClientEnvelope:   env,
		UpstreamEnvelope: env,
		HistoryDomain: modelprofiles.DeriveOpaqueHistoryDomain(
			profile.Protocol, profile.ProviderID, profile.BaseURL, profile.Model, profile.ClientModel,
		),
	}, nil
}

func TestControlModelProfileHandlersCreateActivateErrorsTeardown(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:  fw,
		execs:    &work.ExecutorConfig{ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}}},
		profiles: owner,
		stateDir: t.TempDir(),
	}

	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	resp := app.HandleControlRequest(control.Request{
		Type: "provider_upsert", Operation: "create", Revision: 0, ProviderConnection: ptrConn(providerConnFromProfile(profile)),
	})
	if !resp.OK || resp.Providers == nil {
		t.Fatalf("upsert=%#v", resp)
	}
	if resp.PersistenceOutcome != control.PersistenceApplied || resp.PersistenceDurable == nil || !*resp.PersistenceDurable {
		t.Fatalf("upsert persist=%#v durable=%v", resp.PersistenceOutcome, resp.PersistenceDurable)
	}
	if resp.Providers == nil || len(resp.Providers.Presets) == 0 {
		t.Fatalf("upsert mutation schema missing: %#v", resp.Providers)
	}
	raw, _ := json.Marshal(resp)
	if strings.Contains(string(raw), "secret") {
		t.Fatalf("secret leaked: %s", raw)
	}

	list := app.HandleControlRequest(control.Request{Type: "provider_list"})
	if !list.OK || len(list.Providers.Connections) != 1 {
		t.Fatalf("list=%#v", list)
	}
	if list.Providers == nil || len(list.Providers.Presets) == 0 {
		t.Fatalf("schema missing: %#v", list.Providers)
	}

	spawn := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Codex", Executor: "codex", ExecutorID: "codex",
		Cwd: t.TempDir(), ProfileID: profile.ID, Hidden: true,
	})
	if !spawn.OK || spawn.Agent == nil || spawn.SessionRoute == nil {
		t.Fatalf("spawn=%#v", spawn)
	}
	if !strings.Contains(spawn.Agent.Command, "openai_base_url=") {
		t.Fatalf("spawn command=%q", spawn.Agent.Command)
	}
	agentID := spawn.Agent.ID

	alt := profile
	alt.ID = "codex-alt"
	alt.Model = "up-2"
	if resp := app.HandleControlRequest(control.Request{
		Type: "provider_upsert", Operation: "create", Revision: 1, ProviderConnection: ptrConn(providerConnFromProfile(alt)),
	}); !resp.OK {
		t.Fatalf("alt upsert=%#v", resp)
	}

	cas := app.HandleControlRequest(control.Request{
		Type: "session_provider_activate", AgentID: agentID, ConnectionID: "missing-connection",
	})
	if cas.OK || cas.Error == nil || cas.Error.Code != modelprofiles.CodeProfileNotFound {
		t.Fatalf("cas=%#v", cas)
	}

	act := app.HandleControlRequest(control.Request{
		Type: "session_provider_activate", AgentID: agentID, ConnectionID: alt.ID,
	})
	if !act.OK || act.SessionRoute == nil || act.Binding == nil {
		t.Fatalf("activate=%#v", act)
	}
	if act.PersistenceOutcome != control.PersistenceApplied || act.PersistenceDurable == nil || !*act.PersistenceDurable {
		t.Fatalf("activate persistence=%#v durable=%v", act.PersistenceOutcome, act.PersistenceDurable)
	}
	if act.SessionRoute.Launched == nil || act.SessionRoute.Current == nil {
		t.Fatalf("activate snapshot missing launched/current: %#v", act.SessionRoute)
	}
	if act.SessionRoute.Launched.ConnectionID != profile.ID || act.SessionRoute.Launched.ModelID != "up-1" {
		t.Fatalf("activate launched=%#v", act.SessionRoute.Launched)
	}
	if act.SessionRoute.Current.ConnectionID != alt.ID || act.Binding.ConnectionID != alt.ID {
		t.Fatalf("binding must match current: route=%#v binding=%#v", act.SessionRoute.Current, act.Binding)
	}
	state, ok := owner.Table().Get(agentID)
	if !ok || state.Generation != state.Binding.Generation {
		t.Fatalf("internal generation mismatch: %#v", state)
	}
	if spawn.PersistenceOutcome != control.PersistenceApplied || spawn.PersistenceDurable == nil || !*spawn.PersistenceDurable {
		t.Fatalf("spawn persistence=%#v durable=%v", spawn.PersistenceOutcome, spawn.PersistenceDurable)
	}

	get := app.HandleControlRequest(control.Request{Type: "session_provider_get", AgentID: agentID})
	if !get.OK || get.SessionRoute == nil {
		t.Fatalf("get=%#v", get)
	}

	closeResp := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	if !closeResp.OK {
		t.Fatalf("close=%#v", closeResp)
	}
	if len(fw.killed) != 1 || fw.killed[0] != agentID {
		t.Fatalf("killed=%v", fw.killed)
	}
	if _, ok := owner.SessionSnapshot(agentID); ok {
		t.Fatal("route binding should be released on close")
	}
}

func TestControlSpawnCommitPersistFailureCleansAgentBinding(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}

	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:  fw,
		execs:    &work.ExecutorConfig{ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}}},
		profiles: owner,
		stateDir: t.TempDir(),
	}
	var saves atomic.Int64
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" && saves.Add(1) == 2 {
			return errors.New("injected commit persist failure")
		}
		return nil
	})
	resp := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Codex", Executor: "codex", ExecutorID: "codex",
		Cwd: t.TempDir(), ProfileID: profile.ID, Hidden: true,
	})
	owner.RoutesFile().SetPersistHook(nil)
	if resp.OK {
		t.Fatalf("expected spawn failure, got %#v", resp)
	}
	if owner.Table().Len() != 0 {
		t.Fatalf("orphan bindings: %d %#v", owner.Table().Len(), owner.Table().Snapshot())
	}
	if len(fw.killed) != 1 {
		t.Fatalf("killed=%v", fw.killed)
	}
}

func TestControlActivateAppliedButNotDurableKeepsSession(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	alt := profile
	alt.ID = "codex-alt"
	alt.Model = "up-2"
	if _, err := owner.UpsertProfile(alt, 1, true); err != nil {
		t.Fatal(err)
	}

	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:  fw,
		execs:    &work.ExecutorConfig{ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}}},
		profiles: owner,
		stateDir: t.TempDir(),
	}
	spawn := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Codex", Executor: "codex", ExecutorID: "codex",
		Cwd: t.TempDir(), ProfileID: profile.ID, Hidden: true,
	})
	if !spawn.OK || spawn.Agent == nil {
		t.Fatalf("spawn=%#v", spawn)
	}
	agentID := spawn.Agent.ID
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "after_rename" {
			return errors.New("injected after_rename")
		}
		return nil
	})
	act := app.HandleControlRequest(control.Request{
		Type: "session_provider_activate", AgentID: agentID, ConnectionID: alt.ID,
	})
	owner.RoutesFile().SetPersistHook(nil)
	if !act.OK || act.SessionRoute == nil || act.Binding == nil || act.Binding.ConnectionID != alt.ID {
		t.Fatalf("activate must succeed applied: %#v", act)
	}
	if act.PersistenceOutcome != control.PersistenceApplied || act.PersistenceDurable == nil || *act.PersistenceDurable {
		t.Fatalf("want applied durable=false got outcome=%q durable=%v", act.PersistenceOutcome, act.PersistenceDurable)
	}
	if len(fw.killed) != 0 {
		t.Fatalf("must not kill live session: %v", fw.killed)
	}
	snap, ok := owner.SessionSnapshot(agentID)
	if !ok || snap.Current == nil || snap.Current.ConnectionID != alt.ID {
		t.Fatalf("memory must keep applied binding: %#v", snap)
	}
}

func TestControlCatalogMutationsDirSyncAppliedNotDurable(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	app := &controlApp{profiles: owner, stateDir: t.TempDir()}

	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}

	// Not-applied CAS failure.
	bad := app.HandleControlRequest(control.Request{
		Type: "provider_upsert", Operation: "create", Revision: 99, ProviderConnection: ptrConn(providerConnFromProfile(profile)),
	})
	if bad.OK || bad.Error == nil || bad.Error.Code != modelprofiles.CodeProfileConflict {
		t.Fatalf("not-applied=%#v", bad)
	}

	owner.Store().SetDirSync(func(string) error {
		return errors.New("injected catalog dirSync")
	})
	defer owner.Store().SetDirSync(nil)

	assertApplied := func(resp control.Response) {
		t.Helper()
		if !resp.OK || resp.Providers == nil || resp.Providers.Connections == nil {
			t.Fatalf("resp=%#v", resp)
		}
		if resp.PersistenceOutcome != control.PersistenceApplied || resp.PersistenceDurable == nil || *resp.PersistenceDurable {
			t.Fatalf("persist outcome=%q durable=%v", resp.PersistenceOutcome, resp.PersistenceDurable)
		}
		if resp.Confirmation == "" {
			t.Fatal("expected durability confirmation warning")
		}
	}

	create := app.HandleControlRequest(control.Request{
		Type: "provider_upsert", Operation: "create", Revision: 0, ProviderConnection: ptrConn(providerConnFromProfile(profile)),
	})
	assertApplied(create)
	if create.Providers.Revision != 1 || len(create.Providers.Connections) != 1 {
		t.Fatalf("create catalog=%#v", create.Providers)
	}

	updated := profile
	updated.Name = "Codex Updated"
	updated.Model = "up-2"
	update := app.HandleControlRequest(control.Request{
		Type: "provider_upsert", Operation: "update", Revision: 1, ProviderConnection: ptrConn(providerConnFromProfile(updated)),
	})
	assertApplied(update)
	got, err := owner.GetProfile(profile.ID)
	if err != nil || got.Name != "Codex Updated" {
		t.Fatalf("update memory=%#v err=%v", got, err)
	}

	setDef := app.HandleControlRequest(control.Request{
		Type: "provider_set_default", ExecutorID: modelprofiles.ExecutorCodex,
		ProfileID: profile.ID, Revision: 2,
	})
	assertApplied(setDef)
	if setDef.Providers.Defaults[modelprofiles.ExecutorCodex].ConnectionID != profile.ID {
		t.Fatalf("defaults=%#v", setDef.Providers.Defaults)
	}

	clear := app.HandleControlRequest(control.Request{
		Type: "provider_set_default", ExecutorID: modelprofiles.ExecutorCodex,
		ProfileID: "", Revision: 3,
	})
	assertApplied(clear)

	del := app.HandleControlRequest(control.Request{
		Type: "provider_delete", ProfileID: profile.ID, Revision: 4,
	})
	assertApplied(del)
	if del.Providers.Revision != 5 || len(del.Providers.Connections) != 0 {
		t.Fatalf("delete catalog=%#v", del.Providers)
	}
	if owner.Catalog().Revision != 5 || len(owner.Catalog().Profiles) != 0 {
		t.Fatalf("delete memory=%#v", owner.Catalog())
	}
}

func TestControlUpsertUnknownClientContractRejected(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		// production builtin verifier
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	app := &controlApp{profiles: owner, stateDir: t.TempDir()}
	resp := app.HandleControlRequest(control.Request{
		Type: "provider_upsert", Operation: "create", Revision: 0,
	ProviderConnection: &modelprofiles.ProviderConnectionInput{
			ID: "bad", Name: "Bad", Client: modelprofiles.ClientCodex,
			PresetID: modelprofiles.ProviderPresetOpenRouter, ModelID: "totally-untrusted-model",
		},
	})
	if resp.OK || resp.Error == nil || resp.Error.Code != modelprofiles.CodeProfileInvalid {
		t.Fatalf("resp=%#v", resp)
	}
	if owner.Catalog().Revision != 0 {
		t.Fatalf("revision mutated: %d", owner.Catalog().Revision)
	}
}

func TestControlActivateLaunchedSurvivesHistoryTrimAndRestart(t *testing.T) {
	root := t.TempDir()
	profiles := filepath.Join(root, "model-profiles.toml")
	routes := filepath.Join(root, "route-bindings.json")
	listener := filepath.Join(root, "route-listener.json")
	lookup := func(string) (string, bool) { return "ready", true }
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: profiles, RoutesPath: routes, ListenerPath: listener,
		Lookup: lookup, Verifier: controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	base := modelprofiles.Profile{
		ID: "p0", Name: "P0", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-0",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(base, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(modelprofiles.ExecutorCodex, base.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "tmux:@trim"); err != nil {
		t.Fatal(err)
	}
	app := &controlApp{profiles: owner, stateDir: t.TempDir()}
	rev := int64(1)
	for i := 1; i <= modelprofiles.MaxRouteHistoryEvents+8; i++ {
		p := base
		p.ID = "p" + itoaControl(i)
		p.Name = p.ID
		p.Model = "up-" + itoaControl(i)
		if _, err := owner.UpsertProfile(p, rev, true); err != nil {
			t.Fatal(err)
		}
		rev++
		_, ok := owner.Table().Get("tmux:@trim")
		if !ok {
			t.Fatal("missing")
		}
		act := app.HandleControlRequest(control.Request{
			Type: "session_provider_activate", AgentID: "tmux:@trim", ConnectionID: p.ID,
		})
		if !act.OK || act.SessionRoute == nil || act.SessionRoute.Launched == nil {
			t.Fatalf("activate i=%d %#v", i, act)
		}
		if act.SessionRoute.Launched.ConnectionID != "p0" || act.SessionRoute.Launched.ModelID != "up-0" {
			t.Fatalf("launched drifted i=%d %#v", i, act.SessionRoute.Launched)
		}
		if act.Binding == nil || act.Binding.ConnectionID != p.ID {
			t.Fatalf("binding %#v", act.Binding)
		}
	}
	_ = owner.Close()

	owner2, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: profiles, RoutesPath: routes, ListenerPath: listener,
		Lookup: lookup, Verifier: controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner2.Close() })
	app2 := &controlApp{profiles: owner2, stateDir: t.TempDir()}
	get := app2.HandleControlRequest(control.Request{Type: "session_provider_get", AgentID: "tmux:@trim"})
	if !get.OK || get.SessionRoute == nil || get.SessionRoute.Launched == nil {
		t.Fatalf("get after restart %#v", get)
	}
	if get.SessionRoute.Launched.ConnectionID != "p0" || get.SessionRoute.Current == nil {
		t.Fatalf("restart snapshot %#v", get.SessionRoute)
	}
	restored, ok := owner2.Table().Get("tmux:@trim")
	if !ok || len(restored.History) > modelprofiles.MaxRouteHistoryEvents {
		t.Fatalf("history len=%d ok=%v", len(restored.History), ok)
	}
}

func itoaControl(i int) string {
	if i == 0 {
		return "0"
	}
	var b [16]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	return string(b[n:])
}

func TestControlSpawnCreateFailureSurfacesAbortCleanup(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	fw.createErr = errors.New("injected tmux create failure")
	app := &controlApp{
		watcher:  fw,
		execs:    &work.ExecutorConfig{ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}}},
		profiles: owner,
		stateDir: t.TempDir(),
	}
	var saves atomic.Int64
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		// PrepareLaunch rename #1 succeeds; AbortLaunch rename #2 fails closed.
		if phase == "before_rename" && saves.Add(1) == 2 {
			return errors.New("injected abort pre-rename")
		}
		return nil
	})
	resp := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Codex", Executor: "codex", ExecutorID: "codex",
		Cwd: t.TempDir(), ProfileID: profile.ID, Hidden: true,
	})
	owner.RoutesFile().SetPersistHook(nil)
	if resp.OK || resp.Error == nil {
		t.Fatalf("resp=%#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "injected tmux create failure") {
		t.Fatalf("create error missing: %#v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "injected abort pre-rename") {
		t.Fatalf("abort failure must be surfaced: %#v", resp.Error)
	}
	if owner.Table().Len() == 0 {
		t.Fatal("must not report clean failure while provisional ownership survives")
	}
}

func TestControlSpawnCommitCleanupKillFailureSurfaced(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	fw.killErr = errors.New("injected kill failure")
	fw.killLeavesLive = true
	app := &controlApp{
		watcher:  fw,
		execs:    &work.ExecutorConfig{ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}}},
		profiles: owner,
		stateDir: t.TempDir(),
	}
	var saves atomic.Int64
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" && saves.Add(1) == 2 {
			return errors.New("injected commit persist failure")
		}
		return nil
	})
	resp := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Codex", Executor: "codex", ExecutorID: "codex",
		Cwd: t.TempDir(), ProfileID: profile.ID, Hidden: true,
	})
	owner.RoutesFile().SetPersistHook(nil)
	if resp.OK || resp.Error == nil {
		t.Fatalf("resp=%#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "injected kill failure") {
		t.Fatalf("kill failure not surfaced: %#v", resp.Error)
	}
	if owner.Table().Len() == 0 {
		t.Fatal("provisional/committed route must be preserved while kill failed and session still live")
	}
}

func TestControlSpawnAttachOwnerFailureReleasesCommittedRoute(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	store := newControlBrainStore(t)
	item, err := store.CreateWork(brain.Work{
		Title: "Attach race", Objective: "lose CAS after commit",
		Status: brain.WorkOpen, CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	fw.onCreate = func(string) {
		if _, attachErr := store.AttachWorkOwner(item.ID, "incumbent:@9"); attachErr != nil {
			t.Fatalf("attach incumbent: %v", attachErr)
		}
	}
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs:      &work.ExecutorConfig{ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}}},
		profiles:   owner,
		stateDir:   t.TempDir(),
	}
	resp := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Losing", Executor: "codex", ExecutorID: "codex",
		Cwd: t.TempDir(), ProfileID: profile.ID, Prompt: "lose", WorkID: item.ID,
	})
	if resp.OK || resp.Error == nil {
		t.Fatalf("resp=%#v", resp)
	}
	if owner.Table().Len() != 0 {
		t.Fatalf("committed route must be released: %#v", owner.Table().Snapshot())
	}
	if len(fw.killed) != 1 {
		t.Fatalf("killed=%v", fw.killed)
	}
}

func TestControlSpawnAttachOwnerFailureSurfacesKillAndPreservesRoute(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	store := newControlBrainStore(t)
	item, err := store.CreateWork(brain.Work{
		Title: "Attach race", Objective: "surface kill",
		Status: brain.WorkOpen, CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	fw.killErr = errors.New("injected kill failure")
	fw.killLeavesLive = true
	fw.onCreate = func(string) {
		if _, attachErr := store.AttachWorkOwner(item.ID, "incumbent:@9"); attachErr != nil {
			t.Fatalf("attach incumbent: %v", attachErr)
		}
	}
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs:      &work.ExecutorConfig{ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}}},
		profiles:   owner,
		stateDir:   t.TempDir(),
	}
	resp := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Losing", Executor: "codex", ExecutorID: "codex",
		Cwd: t.TempDir(), ProfileID: profile.ID, Prompt: "lose", WorkID: item.ID,
	})
	if resp.OK || resp.Error == nil {
		t.Fatalf("resp=%#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "injected kill failure") {
		t.Fatalf("cleanup failures not surfaced: %#v", resp.Error)
	}
	if owner.Table().Len() == 0 {
		t.Fatal("committed route must be preserved while kill failed and session still live")
	}
}

func TestControlSpawnAttachOwnerFailureReleasePersistSurfacedAfterKill(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	store := newControlBrainStore(t)
	item, err := store.CreateWork(brain.Work{
		Title: "Attach race", Objective: "surface release",
		Status: brain.WorkOpen, CompletionPolicy: brain.CompletionBounded,
	})
	if err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	fw.onCreate = func(string) {
		if _, attachErr := store.AttachWorkOwner(item.ID, "incumbent:@9"); attachErr != nil {
			t.Fatalf("attach incumbent: %v", attachErr)
		}
	}
	app := &controlApp{
		watcher:    fw,
		brainStore: store,
		execs:      &work.ExecutorConfig{ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}}},
		profiles:   owner,
		stateDir:   t.TempDir(),
	}
	var releases atomic.Int64
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			n := releases.Add(1)
			// 1=prepare, 2=commit, 3=release after successful kill
			if n == 3 {
				return errors.New("injected release pre-rename")
			}
		}
		return nil
	})
	resp := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Losing", Executor: "codex", ExecutorID: "codex",
		Cwd: t.TempDir(), ProfileID: profile.ID, Prompt: "lose", WorkID: item.ID,
	})
	owner.RoutesFile().SetPersistHook(nil)
	if resp.OK || resp.Error == nil {
		t.Fatalf("resp=%#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "injected release pre-rename") {
		t.Fatalf("release failure not surfaced: %#v", resp.Error)
	}
	if owner.Table().Len() == 0 {
		t.Fatal("route must remain when release persist not applied")
	}
}

func controlRoutedSpawn(t *testing.T) (*controlApp, *modelprofiles.Owner, *fakeControlWatcher, string) {
	t.Helper()
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:    fw,
		brainStore: newControlBrainStore(t),
		execs:      &work.ExecutorConfig{ByName: map[string]work.Executor{"codex": {Name: "codex", Command: "codex", Kind: "codex"}}},
		profiles:   owner,
		stateDir:   t.TempDir(),
	}
	spawn := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Routed", Executor: "codex", ExecutorID: "codex",
		Cwd: t.TempDir(), ProfileID: profile.ID, Prompt: "hi",
	})
	if !spawn.OK || spawn.Agent == nil {
		t.Fatalf("spawn=%#v", spawn)
	}
	return app, owner, fw, spawn.Agent.ID
}

func TestControlAgentSpawnAliasUsesCanonicalProfileClient(t *testing.T) {
	root := t.TempDir()
	owner, err := modelprofiles.StartOwner(modelprofiles.OwnerConfig{
		ProfilesPath: filepath.Join(root, "model-profiles.toml"),
		RoutesPath:   filepath.Join(root, "route-bindings.json"),
		ListenerPath: filepath.Join(root, "route-listener.json"),
		Lookup:       func(string) (string, bool) { return "ready", true },
		Verifier:     controlProfileVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	profile := modelprofiles.Profile{
		ID: "codex-main", Name: "Codex Main", ExecutorID: modelprofiles.ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol: modelprofiles.ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "up-1",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetDefault(modelprofiles.ExecutorCodex, profile.ID, 1); err != nil {
		t.Fatal(err)
	}
	fw := newFakeControlWatcher()
	app := &controlApp{
		watcher:    fw,
		brainStore: newControlBrainStore(t),
		execs: &work.ExecutorConfig{ByName: map[string]work.Executor{
			"primary": {Name: "primary", Command: "codex", Kind: "codex"},
			"desk":    {Name: "desk", Command: "claude", Kind: "claude"},
		}},
		profiles: owner,
		stateDir: t.TempDir(),
	}

	// Alias executor ID must still resolve the codex default Profile.
	spawn := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Alias", Executor: "primary",
		Cwd: t.TempDir(), Hidden: true,
	})
	if !spawn.OK || spawn.Agent == nil || spawn.SessionRoute == nil || spawn.SessionRoute.Current == nil {
		t.Fatalf("codex alias spawn=%#v", spawn)
	}
	if spawn.SessionRoute.Current.ConnectionID != profile.ID {
		t.Fatalf("alias must bind codex default, got %#v", spawn.SessionRoute.Current)
	}

	claude := modelprofiles.Profile{
		ID: "claude-main", Name: "Claude", ExecutorID: modelprofiles.ExecutorClaude,
		ProviderID: "anthropic", ProviderLabel: "Anthropic",
		Protocol: modelprofiles.ProtocolAnthropicMessages, ClientModel: "claude-sonnet-4-6", Model: "claude-sonnet-4-6",
		ClientModelProvenance: modelprofiles.ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://api.anthropic.com",
		AuthMode:              modelprofiles.AuthModeBearerEnv,
		CredentialEnv:         "ANTHROPIC_API_KEY",
	}
	if _, err := owner.UpsertProfile(claude, 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetDefault(modelprofiles.ExecutorClaude, claude.ID, 3); err != nil {
		t.Fatal(err)
	}
	spawnClaude := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "ClaudeAlias", Executor: "desk",
		Cwd: t.TempDir(), Hidden: true,
	})
	if !spawnClaude.OK || spawnClaude.SessionRoute == nil || spawnClaude.SessionRoute.Current == nil {
		t.Fatalf("claude alias spawn=%#v", spawnClaude)
	}
	if spawnClaude.SessionRoute.Current.ConnectionID != claude.ID {
		t.Fatalf("claude alias must bind default, got %#v", spawnClaude.SessionRoute.Current)
	}

	// Explicit command remains expert bypass (no managed route).
	raw := app.HandleControlRequest(control.Request{
		Type: "agent_spawn", Name: "Raw", Command: "my-custom-agent --flag",
		Cwd: t.TempDir(), Hidden: true,
	})
	if !raw.OK || raw.SessionRoute != nil {
		t.Fatalf("raw custom bypass=%#v", raw)
	}
}

func TestControlAgentCloseKillAndReleaseSuccess(t *testing.T) {
	app, owner, fw, agentID := controlRoutedSpawn(t)
	resp := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	if !resp.OK {
		t.Fatalf("close=%#v", resp)
	}
	if len(fw.killed) != 1 || fw.killed[0] != agentID {
		t.Fatalf("killed=%v", fw.killed)
	}
	if _, ok := owner.SessionSnapshot(agentID); ok {
		t.Fatal("route must be released")
	}
}

func TestControlAgentCloseReleasePersistFailureSurfaced(t *testing.T) {
	app, owner, fw, agentID := controlRoutedSpawn(t)
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected release pre-rename")
		}
		return nil
	})
	resp := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	owner.RoutesFile().SetPersistHook(nil)
	if resp.OK || resp.Error == nil {
		t.Fatalf("resp=%#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "injected release pre-rename") {
		t.Fatalf("error=%#v", resp.Error)
	}
	if len(fw.killed) != 1 {
		t.Fatalf("killed=%v", fw.killed)
	}
	if _, ok := owner.SessionSnapshot(agentID); !ok {
		t.Fatal("route must remain when release not applied")
	}
}

func TestControlAgentCloseRetryConvergesAfterReleaseFailure(t *testing.T) {
	app, owner, fw, agentID := controlRoutedSpawn(t)
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected release pre-rename")
		}
		return nil
	})
	first := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	if first.OK {
		t.Fatalf("first=%#v", first)
	}
	owner.RoutesFile().SetPersistHook(nil)
	fw.reportKillMissing = true
	second := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	if !second.OK {
		t.Fatalf("retry=%#v", second)
	}
	if _, ok := owner.SessionSnapshot(agentID); ok {
		t.Fatal("retry must release route")
	}
}

func TestControlAgentCloseKillFailureStillLivePreservesRoute(t *testing.T) {
	app, owner, fw, agentID := controlRoutedSpawn(t)
	fw.killErr = errors.New("injected kill failure")
	fw.killLeavesLive = true
	resp := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	if resp.OK || resp.Error == nil {
		t.Fatalf("resp=%#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "injected kill failure") {
		t.Fatalf("error=%#v", resp.Error)
	}
	if _, ok := owner.SessionSnapshot(agentID); !ok {
		t.Fatal("route must be preserved while still live")
	}
	if !fw.HasSession(agentID) {
		t.Fatal("session must remain live")
	}
}

func TestControlAgentCloseAppliedNonDurableReleaseSurfaced(t *testing.T) {
	app, owner, _, agentID := controlRoutedSpawn(t)
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "after_rename" {
			return errors.New("injected dirsync")
		}
		return nil
	})
	resp := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	owner.RoutesFile().SetPersistHook(nil)
	if resp.OK || resp.Error == nil {
		t.Fatalf("applied+non-durable must not return success: %#v", resp)
	}
	if resp.PersistenceOutcome != control.PersistenceApplied || resp.PersistenceDurable == nil || *resp.PersistenceDurable {
		t.Fatalf("persistence fields=%#v durable=%v", resp.PersistenceOutcome, resp.PersistenceDurable)
	}
	if _, ok := owner.SessionSnapshot(agentID); ok {
		t.Fatal("memory/disk rename applied — route should be gone")
	}
}

func TestControlAgentCloseResourceReleaseFailurePreservesRoute(t *testing.T) {
	app, owner, fw, agentID := controlRoutedSpawn(t)
	fw.killErr = fmt.Errorf("%w: injected resource cleanup", errors.New("delegated resource release failed"))
	// Window gone, resource cleanup failed — HasSession/Probe absent.
	resp := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	if resp.OK || resp.Error == nil {
		t.Fatalf("resp=%#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "resource cleanup") {
		t.Fatalf("error=%#v", resp.Error)
	}
	if _, ok := owner.SessionSnapshot(agentID); !ok {
		t.Fatal("route must remain retryable after resource cleanup failure")
	}
}

func TestControlAgentCloseProbeFailurePreservesRoute(t *testing.T) {
	app, owner, fw, agentID := controlRoutedSpawn(t)
	fw.killErr = errors.New("injected kill failure")
	fw.killLeavesLive = true
	fw.probeErr = errors.New("injected probe transport failure")
	resp := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	if resp.OK || resp.Error == nil {
		t.Fatalf("resp=%#v", resp)
	}
	if !strings.Contains(resp.Error.Message, "probe transport") {
		t.Fatalf("error=%#v", resp.Error)
	}
	if _, ok := owner.SessionSnapshot(agentID); !ok {
		t.Fatal("route must remain on ambiguous probe")
	}
}

func TestControlAgentCloseResourceFailureThenMissingRetryConverges(t *testing.T) {
	app, owner, fw, agentID := controlRoutedSpawn(t)
	fw.killErr = fmt.Errorf("%w: injected", errors.New("delegated resource release failed"))
	first := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	if first.OK {
		t.Fatalf("first=%#v", first)
	}
	fw.killErr = nil
	fw.reportKillMissing = true
	second := app.HandleControlRequest(control.Request{Type: "agent_close", AgentID: agentID, Force: true})
	if !second.OK {
		t.Fatalf("retry=%#v", second)
	}
	if _, ok := owner.SessionSnapshot(agentID); ok {
		t.Fatal("retry must release")
	}
}
