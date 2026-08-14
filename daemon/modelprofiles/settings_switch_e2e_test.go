package modelprofiles

import (
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

// TestSettingsSwitchPreferredProviderOrchestration proves the Settings-only
// Provider switch contract end-to-end on the daemon:
//
//  1. Selecting a Provider in Settings persists the preferred Provider (catalog
//     client default) with NO fabricated model — a new default connection is
//     model-required until the client chooses a model.
//  2. Current-model carryover: when the current model is enabled+available on
//     the newly selected Provider, activating the exact new Provider + current
//     Model pair switches the same Session without recreation.
//  3. The carried model is then recorded on the preferred Provider so future
//     Sessions and restart restoration stay deterministic.
//  4. An unsupported current model never falls back: activation fails inline,
//     the Session keeps its old route, the preferred Provider stays recorded
//     without a model, and the next request still routes the old Provider.
//  5. Concurrent Sessions are untouched; the catalog revision is never mutated
//     by activation; restart restores preferred Provider + route determinis-
//     tically.
func TestSettingsSwitchPreferredProviderOrchestration(t *testing.T) {
	root := t.TempDir()
	shared := newE2EUpstream(t, nil)
	other := newE2EUpstream(t, nil)
	sharedURL := shared.server.URL + "/v1"
	otherURL := other.server.URL + "/v1"

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
	t.Cleanup(func() { _ = owner.Close() })

	// A (current Provider, gpt-5.6-sol), B (new Provider: supports gpt-5.6-sol AND
	// gpt-5.5), C (new Provider that does NOT support gpt-5.6-sol).
	projA, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", sharedURL, "gpt-5.6-sol"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connA := projA.Connections[0]
	projB, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", sharedURL, "gpt-5.5"), "key-b", projA.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connB := connectionByName(t, projB, "Beta")
	projC, err := owner.UpsertProviderConnection(e2eCustomInput("", "Gamma", otherURL, "gpt-5.4"), "key-c", projB.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connC := connectionByName(t, projC, "Gamma")
	seedModelCatalogs(t, owner, map[string][]string{
		connA.ID: {"gpt-5.6-sol"},
		connB.ID: {"gpt-5.6-sol", "gpt-5.5"},
		connC.ID: {"gpt-5.4"},
	})
	if _, err := owner.SetProviderDefault(ClientCodex, connA.ID, "gpt-5.6-sol", projC.Revision); err != nil {
		t.Fatal(err)
	}

	// Two live Sessions on A; the first is the "current" one for the switch.
	plan1, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan1.ProvisionalID, "s-current"); err != nil {
		t.Fatal(err)
	}
	routeID := plan1.State.Binding.RouteID
	plan2, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan2.ProvisionalID, "s-other"); err != nil {
		t.Fatal(err)
	}
	routerSrv := httptest.NewServer(owner.router.Handler())
	t.Cleanup(routerSrv.Close)
	routerAddr := routerSrv.Listener.Addr().String()

	// 1. Settings records the preferred Provider without fabricating a model:
	// a new default connection is model-required until the client chooses.
	projSwitch, err := owner.SetProviderDefault(ClientCodex, connB.ID, "", owner.Catalog().Revision)
	if err != nil {
		t.Fatal(err)
	}
	def := projSwitch.Defaults[ClientCodex]
	if def.ConnectionID != connB.ID || def.ModelID != "" {
		t.Fatalf("preferred after switch must be %s with no fabricated model: %#v", connB.ID, def)
	}

	// 2. Current-model carryover on the same Session: B supports gpt-5.6-sol, so
	// the exact new Provider + current Model pair activates in place.
	beforeRev := owner.Catalog().Revision
	state, snap, persist, err := owner.ActivateSessionProvider("s-current", connB.ID, "gpt-5.6-sol", "")
	if err != nil || !persist.Applied {
		t.Fatalf("carryover activate err=%v persist=%#v", err, persist)
	}
	if state.Binding.RouteID != routeID {
		t.Fatalf("carryover must keep the same route: %s -> %s", routeID, state.Binding.RouteID)
	}
	if state.Binding.ProfileID != connB.ID || state.Binding.UpstreamModel != "gpt-5.6-sol" {
		t.Fatalf("carryover binding=%#v", state.Binding)
	}
	if snap.Current == nil || snap.Current.ConnectionID != connB.ID || snap.Current.ModelID != "gpt-5.6-sol" {
		t.Fatalf("carryover snap=%#v", snap)
	}
	if sel, ok := owner.SessionProviderSelection("s-current"); !ok || sel.ConnectionName != "Beta" || sel.ModelID != "gpt-5.6-sol" {
		t.Fatalf("session attribution after carryover: %#v ok=%v", sel, ok)
	}
	if owner.Catalog().Revision != beforeRev {
		t.Fatalf("activation mutated catalog revision")
	}
	if def := owner.MustProjectForTest(t).Defaults[ClientCodex]; def.ConnectionID != connB.ID || def.ModelID != "" {
		t.Fatalf("activation must not mutate the preferred default: %#v", def)
	}

	// 3. The carried model is recorded on the preferred Provider (the App does
	// this after the acknowledged activation).
	if _, err := owner.SetProviderDefault(ClientCodex, connB.ID, "gpt-5.6-sol", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	if def := owner.MustProjectForTest(t).Defaults[ClientCodex]; def.ConnectionID != connB.ID || def.ModelID != "gpt-5.6-sol" {
		t.Fatalf("preferred default must carry the model: %#v", def)
	}

	// The next request on the current Session routes B with key-b/gpt-5.6-sol.
	postLoopback(t, routerAddr, routeID, "gpt-5.6-sol")
	if got, _ := shared.last(); got.auth != "Bearer key-b" || got.model != "gpt-5.6-sol" {
		t.Fatalf("request after carryover must be B: %#v", got)
	}

	// 4. Unsupported current model never falls back: switching to C while the
	// Session runs gpt-5.6-sol (not supported by C) fails inline, keeps the old
	// route, and leaves the preferred Provider recorded without a model.
	if _, err := owner.SetProviderDefault(ClientCodex, connC.ID, "", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	if def := owner.MustProjectForTest(t).Defaults[ClientCodex]; def.ConnectionID != connC.ID || def.ModelID != "" {
		t.Fatalf("preferred must switch to C with no model: %#v", def)
	}
	_, _, _, err = owner.ActivateSessionProvider("s-current", connC.ID, "gpt-5.6-sol", "")
	if !errors.Is(err, ErrUpstreamModelRequired) {
		t.Fatalf("unsupported carryover must fail closed with ErrUpstreamModelRequired, got %v", err)
	}
	state, _ = owner.Table().Get("s-current")
	if state.Binding.ProfileID != connB.ID || state.Binding.UpstreamModel != "gpt-5.6-sol" {
		t.Fatalf("failed carryover must keep the old route: %#v", state.Binding)
	}
	// The daemon never substitutes another model into the preferred default.
	if def := owner.MustProjectForTest(t).Defaults[ClientCodex]; def.ModelID != "" {
		t.Fatalf("failed carryover must not fabricate a default model: %#v", def)
	}
	postLoopback(t, routerAddr, routeID, "gpt-5.6-sol")
	if got, _ := shared.last(); got.auth != "Bearer key-b" || got.model != "gpt-5.6-sol" {
		t.Fatalf("request after failed carryover must stay B: %#v", got)
	}

	// 5. Concurrent Session isolation: s-other stays on A throughout.
	postLoopback(t, routerAddr, plan2.State.Binding.RouteID, "gpt-5.6-sol")
	if got, _ := shared.last(); got.auth != "Bearer key-a" || got.model != "gpt-5.6-sol" {
		t.Fatalf("s-other must stay on A: %#v", got)
	}

	// 6. Restart restoration: preferred Provider (C, no model) and the current
	// Session route (B + gpt-5.6-sol) restore deterministically; the next request
	// still routes the acknowledged route.
	_ = owner.Close()
	owner2 := start()
	t.Cleanup(func() { _ = owner2.Close() })
	routerSrv2 := httptest.NewServer(owner2.router.Handler())
	t.Cleanup(routerSrv2.Close)
	if def := owner2.MustProjectForTest(t).Defaults[ClientCodex]; def.ConnectionID != connC.ID || def.ModelID != "" {
		t.Fatalf("restart must restore preferred Provider: %#v", def)
	}
	state2, ok := owner2.Table().Get("s-current")
	if !ok || state2.Binding.ProfileID != connB.ID || state2.Binding.UpstreamModel != "gpt-5.6-sol" {
		t.Fatalf("restart must restore the Session route: %#v ok=%v", state2.Binding, ok)
	}
	postLoopback(t, routerSrv2.Listener.Addr().String(), routeID, "gpt-5.6-sol")
	if got, _ := shared.last(); got.auth != "Bearer key-b" || got.model != "gpt-5.6-sol" {
		t.Fatalf("request after restart must stay B: %#v", got)
	}

	// The model-required state is durable and recoverable: choosing gpt-5.4 in
	// the Composer activates the exact preferred pair on the same Session.
	if _, _, _, err := owner2.ActivateSessionProvider("s-current", connC.ID, "gpt-5.4", ""); err != nil {
		t.Fatalf("model choice activation err=%v", err)
	}
	postLoopback(t, routerSrv2.Listener.Addr().String(), routeID, "gpt-5.4")
	if got, _ := other.last(); got.auth != "Bearer key-c" || got.model != "gpt-5.4" {
		t.Fatalf("request after model choice must route C: %#v", got)
	}
}

// TestSetProviderDefaultClearsModelOnProviderChange proves the Settings switch
// never persists a fabricated model: switching the preferred Provider clears
// the client-selected model (model-required), re-selecting the same Provider
// keeps it, and restart restores the exact pair.
func TestSetProviderDefaultClearsModelOnProviderChange(t *testing.T) {
	root := t.TempDir()
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
	t.Cleanup(func() { _ = owner.Close() })

	projA, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", "https://a.example/v1", "gpt-5.6-sol"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connA := projA.Connections[0]
	projB, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", "https://b.example/v1", "gpt-5.5"), "key-b", projA.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connB := connectionByName(t, projB, "Beta")

	if _, err := owner.SetProviderDefault(ClientCodex, connA.ID, "gpt-5.6-sol", owner.Catalog().Revision); err != nil {
		t.Fatal(err)
	}
	// Re-selecting the same Provider keeps the recorded model.
	proj, err := owner.SetProviderDefault(ClientCodex, connA.ID, "", owner.Catalog().Revision)
	if err != nil {
		t.Fatal(err)
	}
	if def := proj.Defaults[ClientCodex]; def.ConnectionID != connA.ID || def.ModelID != "gpt-5.6-sol" {
		t.Fatalf("re-select must keep the model: %#v", def)
	}
	// Switching to a new Provider clears the model — never first-supported.
	proj, err = owner.SetProviderDefault(ClientCodex, connB.ID, "", owner.Catalog().Revision)
	if err != nil {
		t.Fatal(err)
	}
	if def := proj.Defaults[ClientCodex]; def.ConnectionID != connB.ID || def.ModelID != "" {
		t.Fatalf("provider switch must clear the model: %#v", def)
	}
	if got := owner.store.DefaultModelID(ClientCodex); got != "" {
		t.Fatalf("model-required state must be durable: %q", got)
	}

	// Restart restores the exact model-required preferred Provider.
	_ = owner.Close()
	owner2 := start()
	t.Cleanup(func() { _ = owner2.Close() })
	def := owner2.MustProjectForTest(t).Defaults[ClientCodex]
	if def.ConnectionID != connB.ID || def.ModelID != "" {
		t.Fatalf("restart must restore model-required default: %#v", def)
	}
	if strings.TrimSpace(def.ModelID) != "" {
		t.Fatalf("restart must never fabricate a model: %q", def.ModelID)
	}
}
