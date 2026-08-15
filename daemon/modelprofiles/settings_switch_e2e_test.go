package modelprofiles

import (
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// TestSettingsDefaultDoesNotMutateCurrentRuntime proves Settings owns only the
// future-thread default. The acknowledged current runtime remains sendable and
// both independent values restore after restart.
func TestSettingsDefaultDoesNotMutateCurrentRuntime(t *testing.T) {
	root := t.TempDir()
	upstreamA := newE2EUpstream(t, nil)
	upstreamB := newE2EUpstream(t, nil)
	start := func() *Owner {
		owner, err := StartOwner(OwnerConfig{
			ProfilesPath:  filepath.Join(root, "model-profiles.toml"),
			RoutesPath:    filepath.Join(root, "route-bindings.json"),
			ListenerPath:  filepath.Join(root, "route-listener.json"),
			DiscoveryPath: filepath.Join(root, "provider-discovery.json"),
			Lookup:        func(string) (string, bool) { return "", false },
			Verifier:      BuiltinEnvelopeVerifier{},
		})
		if err != nil {
			t.Fatal(err)
		}
		creds, err := NewFileCredentialStore(filepath.Join(root, "credentials.json"))
		if err != nil {
			t.Fatal(err)
		}
		owner.SetCredentialStore(creds)
		return owner
	}

	owner := start()
	connAProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", upstreamA.server.URL+"/v1", "gpt-5.4"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connA := connAProjection.Connections[0]
	connBProjection, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", upstreamB.server.URL+"/v1", "gpt-5.5"), "key-b", connAProjection.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connB := connectionByName(t, connBProjection, "Beta")
	seedModelCatalogs(t, owner, map[string][]string{
		connA.ID: {"gpt-5.4"},
		connB.ID: {"gpt-5.5"},
	})
	if _, err := owner.SetProviderDefault(ClientCodex, connA.ID, "gpt-5.4", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetProviderDefault(ClientCodex, connB.ID, "", owner.Catalog().Revision); err == nil {
		t.Fatal("different Provider default accepted without an atomic model")
	}
	launch, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(launch.ProvisionalID, "thread-1"); err != nil {
		t.Fatal(err)
	}
	routeID := launch.State.Binding.RouteID

	if _, err := owner.SetProviderDefault(ClientCodex, connB.ID, "gpt-5.5", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	selection, ok := owner.ThreadRuntime("thread-1")
	if !ok || selection.ConnectionID != connA.ID || selection.ModelID != "gpt-5.4" {
		t.Fatalf("Settings mutated current runtime: %#v", selection)
	}
	router := httptest.NewServer(owner.router.Handler())
	postLoopback(t, router.Listener.Addr().String(), routeID, "gpt-5.4")
	if got, _ := upstreamA.last(); got.auth != "Bearer key-a" || got.model != "gpt-5.4" {
		t.Fatalf("current runtime stopped sending after Settings change: %#v", got)
	}
	router.Close()
	_ = owner.Close()

	restored := start()
	t.Cleanup(func() { _ = restored.Close() })
	if def := restored.MustProjectForTest(t).Defaults[ClientCodex]; def.ConnectionID != connB.ID || def.ModelID != "gpt-5.5" {
		t.Fatalf("future default not restored: %#v", def)
	}
	selection, ok = restored.ThreadRuntime("thread-1")
	if !ok || selection.ConnectionID != connA.ID || selection.ModelID != "gpt-5.4" {
		t.Fatalf("acknowledged runtime not restored: %#v", selection)
	}
}
