package modelprofiles

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// e2eUpstream records (Authorization, rewritten model) pairs of every request
// it serves and answers 2xx like a real Responses endpoint.
type e2eUpstream struct {
	mu     sync.Mutex
	calls  []upstreamCall
	server *httptest.Server
	// hold, when non-nil, blocks each request until released.
	hold chan struct{}
}

type upstreamCall struct {
	auth  string
	model string
}

func newE2EUpstream(t *testing.T, hold chan struct{}) *e2eUpstream {
	t.Helper()
	u := &e2eUpstream{hold: hold}
	u.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var obj struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &obj)
		u.mu.Lock()
		u.calls = append(u.calls, upstreamCall{auth: r.Header.Get("Authorization"), model: obj.Model})
		u.mu.Unlock()
		if hold != nil {
			<-hold
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"r_e2e","object":"response","output":[]}`)
	}))
	t.Cleanup(u.server.Close)
	return u
}

func (u *e2eUpstream) snapshot() []upstreamCall {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := append([]upstreamCall{}, u.calls...)
	return out
}

func (u *e2eUpstream) last() (upstreamCall, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.calls) == 0 {
		return upstreamCall{}, false
	}
	return u.calls[len(u.calls)-1], true
}

// seedModelCatalogs writes per-connection synced model catalogs so launch and
// activation admission resolve deterministically.
func seedModelCatalogs(t *testing.T, owner *Owner, models map[string][]string) {
	t.Helper()
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if owner.discovery == nil {
		owner.discovery = newModelDiscoveryCache()
	}
	for id, ids := range models {
		owner.discovery.put(id, ids, nil)
	}
	if owner.discoveryPath != "" {
		if serr := owner.discovery.save(owner.discoveryPath); serr != nil {
			t.Fatal(serr)
		}
	}
}

// postLoopback routes one request through the Zen loopback and asserts the
// response is 200.
func postLoopback(t *testing.T, routerAddr, routeID string) {
	t.Helper()
	base, err := LoopbackCodexBaseURL(routerAddr, routeID)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, base+"/responses", bytes.NewReader([]byte(`{"model":"cli","input":[]}`)))
	req.Header.Set("Authorization", "Bearer "+LoopbackAuthPlaceholder)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("route %s status=%d", routeID, resp.StatusCode)
	}
}

func connectionByName(t *testing.T, proj ProviderCatalogProjection, name string) ProviderConnection {
	t.Helper()
	for _, c := range proj.Connections {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("connection %q not in projection: %#v", name, proj.Connections)
	return ProviderConnection{}
}

func e2eCustomInput(id, name, baseURL, model string) ProviderConnectionInput {
	return ProviderConnectionInput{
		ID: id, Name: name, Client: ClientCodex,
		PresetID: ProviderPresetCustom, BaseURL: baseURL,
		ModelID: model, Advanced: true,
	}
}

// TestSessionProviderActivationE2E proves the painless-switch invariant on one
// live Session: requests through A, activate B (same URL, different key, model)
// then C (different URL/key/model) on the same Session without recreation; the
// next request after each activation carries the new Provider's auth, upstream
// and model; in-flight requests finish on the immutable old snapshot; the
// active route survives restart; unsupported models fail inline keeping the
// old route; concurrent Sessions and catalog defaults are untouched; the
// serving Provider is attributed on the Session selection.
func TestSessionProviderActivationE2E(t *testing.T) {
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

	// Provider A (shared URL, key-a, model-a), B (SAME URL, key-b, model-b),
	// C (different URL, key-c, model-c).
	projA, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", sharedURL, "model-a"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connA := projA.Connections[0]
	projB, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", sharedURL, "model-b"), "key-b", projA.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connB := connectionByName(t, projB, "Beta")
	projC, err := owner.UpsertProviderConnection(e2eCustomInput("", "Gamma", otherURL, "model-c"), "key-c", projB.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connC := connectionByName(t, projC, "Gamma")
	seedModelCatalogs(t, owner, map[string][]string{
		connA.ID: {"model-a"},
		connB.ID: {"model-b"},
		connC.ID: {"model-c"},
	})
	// A routed Codex default exists, so official-subscription stats must be
	// suppressed and attributed to the serving routed Provider.
	if _, err := owner.SetProviderDefault(ClientCodex, connA.ID, "model-a", projC.Revision); err != nil {
		t.Fatal(err)
	}

	// One live Session launched on A. The launch env points at the stable Zen
	// gateway (loopback), never at the upstream URL or key.
	plan, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plan.Command, sharedURL) || strings.Contains(plan.Command, "key-a") {
		t.Fatalf("launch baked upstream facts: %q", plan.Command)
	}
	for k, v := range plan.Env {
		if strings.Contains(v, sharedURL) || strings.Contains(v, "key-a") {
			t.Fatalf("launch env baked upstream facts: %s=%q", k, v)
		}
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-live"); err != nil {
		t.Fatal(err)
	}
	routeID := plan.State.Binding.RouteID
	routerSrv := httptest.NewServer(owner.router.Handler())
	t.Cleanup(routerSrv.Close)
	routerAddr := routerSrv.Listener.Addr().String()

	// Request 1: served by A with key-a and model-a.
	postLoopback(t, routerAddr, routeID)
	if got, _ := shared.last(); got.auth != "Bearer key-a" || got.model != "model-a" {
		t.Fatalf("request 1 must be A: %#v", got)
	}

	// Activate B: same URL, different key, different model — next request must
	// prove B while the Session/route stay identical.
	beforeRev := owner.Catalog().Revision
	beforeDefaults := owner.MustProjectForTest(t).Defaults
	state, snap, persist, err := owner.ActivateSessionProvider("s-live", connB.ID, "model-b")
	if err != nil || !persist.Applied {
		t.Fatalf("activate B err=%v persist=%#v", err, persist)
	}
	if state.Binding.RouteID != routeID || state.Binding.ProfileID != connB.ID || state.Binding.UpstreamModel != "model-b" {
		t.Fatalf("activate B binding=%#v", state.Binding)
	}
	if snap.Current == nil || snap.Current.ConnectionID != connB.ID || snap.Current.ModelID != "model-b" {
		t.Fatalf("activate B snap=%#v", snap)
	}
	if owner.Catalog().Revision != beforeRev {
		t.Fatalf("activation mutated catalog revision")
	}
	afterDefaults := owner.MustProjectForTest(t).Defaults
	if len(afterDefaults) != len(beforeDefaults) {
		t.Fatalf("activation mutated defaults: %#v -> %#v", beforeDefaults, afterDefaults)
	}
	if sel, ok := owner.SessionProviderSelection("s-live"); !ok || sel.ConnectionName != "Beta" || sel.ModelID != "model-b" {
		t.Fatalf("session attribution after B: %#v ok=%v", sel, ok)
	}

	postLoopback(t, routerAddr, routeID)
	if got, _ := shared.last(); got.auth != "Bearer key-b" || got.model != "model-b" {
		t.Fatalf("request 2 must be B: %#v", got)
	}

	// Activate C: different URL, key and model; the next request reaches the
	// other upstream with key-c/model-c.
	if _, _, _, err := owner.ActivateSessionProvider("s-live", connC.ID, "model-c"); err != nil {
		t.Fatal(err)
	}
	postLoopback(t, routerAddr, routeID)
	if got, _ := other.last(); got.auth != "Bearer key-c" || got.model != "model-c" {
		t.Fatalf("request 3 must be C: %#v", got)
	}
	if sel, _ := owner.SessionProviderSelection("s-live"); sel.ConnectionName != "Gamma" || sel.ModelID != "model-c" {
		t.Fatalf("session attribution after C: %#v", sel)
	}
	if !owner.CodexRoutedDefault() {
		t.Fatal("routed Codex default must suppress official-subscription stats")
	}

	// Failure rollback: an unsupported model fails inline and keeps the old
	// route; the daemon never substitutes another model.
	_, _, _, err = owner.ActivateSessionProvider("s-live", connC.ID, "bogus-model")
	if !errors.Is(err, ErrUpstreamModelRequired) {
		t.Fatalf("unsupported model err=%v", err)
	}
	state, _ = owner.Table().Get("s-live")
	if state.Binding.ProfileID != connC.ID || state.Binding.UpstreamModel != "model-c" {
		t.Fatalf("failed activation must keep the old route: %#v", state.Binding)
	}
	postLoopback(t, routerAddr, routeID)
	if got, _ := other.last(); got.auth != "Bearer key-c" || got.model != "model-c" {
		t.Fatalf("request after rollback must stay C: %#v", got)
	}

	// Concurrent Sessions: s2 on A is untouched by s1's activations.
	plan2, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan2.ProvisionalID, "s-other"); err != nil {
		t.Fatal(err)
	}
	postLoopback(t, routerAddr, plan2.State.Binding.RouteID)
	if got, _ := shared.last(); got.auth != "Bearer key-a" || got.model != "model-a" {
		t.Fatalf("s2 must stay on A: %#v", got)
	}
	postLoopback(t, routerAddr, routeID)
	if got, _ := other.last(); got.auth != "Bearer key-c" || got.model != "model-c" {
		t.Fatalf("s1 must stay on C: %#v", got)
	}

	// Restart restoration: the active route (s1 -> C, model-c) survives a
	// daemon restart and the next request still routes with C's auth/model.
	_ = owner.Close()
	owner2 := start()
	t.Cleanup(func() { _ = owner2.Close() })
	routerSrv2 := httptest.NewServer(owner2.router.Handler())
	t.Cleanup(routerSrv2.Close)
	state2, ok := owner2.Table().Get("s-live")
	if !ok || state2.Binding.ProfileID != connC.ID || state2.Binding.UpstreamModel != "model-c" {
		t.Fatalf("restored binding lost activation: %#v ok=%v", state2.Binding, ok)
	}
	postLoopback(t, routerSrv2.Listener.Addr().String(), routeID)
	if got, _ := other.last(); got.auth != "Bearer key-c" || got.model != "model-c" {
		t.Fatalf("request after restart must stay C: %#v", got)
	}
	// The restored binding also survives a further activation on the restarted
	// daemon (generation and catalog restored).
	if _, _, _, err := owner2.ActivateSessionProvider("s-live", connA.ID, "model-a"); err != nil {
		t.Fatalf("activate after restart err=%v", err)
	}
	postLoopback(t, routerSrv2.Listener.Addr().String(), routeID)
	if got, _ := shared.last(); got.auth != "Bearer key-a" || got.model != "model-a" {
		t.Fatalf("request after restart+activate must be A: %#v", got)
	}
}

// TestSessionProviderActivationInFlightSwitch proves an activation during an
// in-flight request: the in-flight request finishes against its immutable old
// snapshot (old auth), while the next request uses the new route.
func TestSessionProviderActivationInFlightSwitch(t *testing.T) {
	root := t.TempDir()
	hold := make(chan struct{})
	started := make(chan struct{})
	shared := newE2EUpstream(t, hold)
	shared.mu.Lock()
	shared.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		var obj struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &obj)
		shared.mu.Lock()
		shared.calls = append(shared.calls, upstreamCall{auth: r.Header.Get("Authorization"), model: obj.Model})
		shared.mu.Unlock()
		select {
		case <-started:
		default:
			close(started)
		}
		<-hold
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"r_e2e","object":"response","output":[]}`)
	})
	shared.mu.Unlock()

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
	t.Cleanup(func() { _ = owner.Close() })
	creds, err := NewFileCredentialStore(filepath.Join(root, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	owner.SetCredentialStore(creds)

	projA, err := owner.UpsertProviderConnection(e2eCustomInput("", "Alpha", shared.server.URL+"/v1", "model-a"), "key-a", 0, true)
	if err != nil {
		t.Fatal(err)
	}
	connA := projA.Connections[0]
	projB, err := owner.UpsertProviderConnection(e2eCustomInput("", "Beta", shared.server.URL+"/v1", "model-b"), "key-b", projA.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	connB := connectionByName(t, projB, "Beta")
	seedModelCatalogs(t, owner, map[string][]string{
		connA.ID: {"model-a"},
		connB.ID: {"model-b"},
	})
	plan, err := owner.PrepareLaunch(ExecutorCodex, connA.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-inflight"); err != nil {
		t.Fatal(err)
	}
	routerSrv := httptest.NewServer(owner.router.Handler())
	t.Cleanup(routerSrv.Close)
	routerAddr := routerSrv.Listener.Addr().String()
	routeID := plan.State.Binding.RouteID

	// Request in flight on A (held upstream).
	done := make(chan struct{})
	go func() {
		defer close(done)
		postLoopback(t, routerAddr, routeID)
	}()
	<-started

	// Activate B while A's request is in flight: must succeed immediately.
	if _, _, _, err := owner.ActivateSessionProvider("s-inflight", connB.ID, "model-b"); err != nil {
		t.Fatalf("activate while in-flight err=%v", err)
	}
	// The in-flight request still finishes on the old snapshot (key-a).
	close(hold)
	<-done
	if got, _ := shared.last(); got.auth != "Bearer key-a" || got.model != "model-a" {
		t.Fatalf("in-flight request must finish on old snapshot: %#v", got)
	}
	// The next request uses the new route (key-b).
	postLoopback(t, routerAddr, routeID)
	if got, _ := shared.last(); got.auth != "Bearer key-b" || got.model != "model-b" {
		t.Fatalf("next request must use new route: %#v", got)
	}
}
