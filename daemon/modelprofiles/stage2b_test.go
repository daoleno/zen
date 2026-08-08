package modelprofiles

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func stage2bRoot(t *testing.T) (profiles, routes, listener string) {
	t.Helper()
	root := t.TempDir()
	return filepath.Join(root, "model-profiles.toml"),
		filepath.Join(root, "route-bindings.json"),
		filepath.Join(root, "route-listener.json")
}

func TestOwnerStartEmptyInstallOK(t *testing.T) {
	owner := startTestOwner(t, nil)
	if owner.ListenAddr() != "" {
		t.Fatalf("cold start must stay inert, listen=%q", owner.ListenAddr())
	}
	if owner.Catalog().Revision != 0 {
		t.Fatalf("revision=%d", owner.Catalog().Revision)
	}
	if owner.Table().Len() != 0 {
		t.Fatalf("table len=%d", owner.Table().Len())
	}
}

func TestOwnerColdLegacyStartupInertUntilManagedLaunch(t *testing.T) {
	root := t.TempDir()
	profiles := filepath.Join(root, "model-profiles.toml")
	routes := filepath.Join(root, "route-bindings.json")
	listener := filepath.Join(root, "route-listener.json")
	stale := []byte("{\"listen_addr\":\"127.0.0.1:59999\"}\n")
	if err := os.WriteFile(listener, stale, 0o600); err != nil {
		t.Fatal(err)
	}

	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })

	if owner.ListenAddr() != "" {
		t.Fatalf("expected no listener, got %q", owner.ListenAddr())
	}
	for _, path := range []string{profiles, routes} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cold start must not create %s: err=%v", path, err)
		}
	}
	if _, err := os.Stat(listener); !os.IsNotExist(err) {
		t.Fatalf("zero-route StartOwner must converge stale listener to absent: err=%v", err)
	}

	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profiles); err != nil {
		t.Fatalf("catalog write: %v", err)
	}
	if _, err := os.Stat(listener); !os.IsNotExist(err) {
		t.Fatalf("catalog CRUD must not create listener: err=%v", err)
	}
	if owner.ListenAddr() != "" {
		t.Fatal("catalog CRUD must leave owner inert")
	}
	if _, err := os.Stat(routes); !os.IsNotExist(err) {
		t.Fatalf("routes must stay absent until launch: err=%v", err)
	}

	// Restart with catalog present but no live routes: still inert.
	_ = owner.Close()
	if err := os.WriteFile(listener, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	owner, err = StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if owner.ListenAddr() != "" {
		t.Fatal("profiles without routes must stay inert on restart")
	}
	if _, err := os.Stat(listener); !os.IsNotExist(err) {
		t.Fatalf("restart with zero routes must converge listener: err=%v", err)
	}

	// Plant stale metadata immediately before first managed launch — must not sticky-bind it.
	if err := os.WriteFile(listener, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if owner.ListenAddr() == "" || owner.ListenAddr() == "127.0.0.1:59999" {
		t.Fatalf("first managed launch must bind fresh listener, got %q", owner.ListenAddr())
	}
	if _, err := os.Stat(listener); err != nil {
		t.Fatalf("listener file after launch: %v", err)
	}
	raw, err := os.ReadFile(listener)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "59999") {
		t.Fatalf("stale port must not be reused: %s", raw)
	}
	if !strings.Contains(plan.Command, "openai_base_url=") {
		t.Fatalf("plan missing loopback: %s", plan.Command)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(routes); err != nil {
		t.Fatalf("routes after commit: %v", err)
	}

	// Live restore still requires the original bound port.
	addr := owner.ListenAddr()
	_ = owner.Close()
	hold, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	_, err = StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
	})
	if !errors.Is(err, ErrListenerFailed) {
		t.Fatalf("live restore must require original port: err=%v", err)
	}
	hold.Close()
	owner2, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner2.Close() })
	if owner2.ListenAddr() != addr {
		t.Fatalf("restored listen %q want %q", owner2.ListenAddr(), addr)
	}
}

func TestOwnerStartMalformedProfilesFailClosed(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	if err := os.WriteFile(profiles, []byte("revision = \"nope\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
	})
	if err == nil {
		t.Fatal("expected fail closed on malformed profiles")
	}
}

func TestOwnerStartMalformedRoutesFailClosed(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	if err := os.WriteFile(routes, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
	})
	if err == nil {
		t.Fatal("expected fail closed on malformed routes")
	}
}

func TestOwnerStartPortConflictWithPreferAndRoutesFailClosed(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	profiles, routes, listener := stage2bRoot(t)
	// Seed a live route so occupied PreferAddr must fail closed.
	seed, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := seed.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := seed.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := seed.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	_ = seed.Close()

	_, err = StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
		PreferAddr:   occupied.Addr().String(),
	})
	if !errors.Is(err, ErrListenerFailed) {
		t.Fatalf("err=%v want ErrListenerFailed", err)
	}
}

func TestOwnerPrepareLaunchCompileAndCommit(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "org/up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetDefault(ExecutorCodex, profile.ID, 1); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Applied || plan.Bypass || plan.ProvisionalID == "" {
		t.Fatalf("plan=%#v", plan)
	}
	if !strings.Contains(plan.Command, "openai_base_url=") {
		t.Fatalf("command missing route: %s", plan.Command)
	}
	if plan.Env[EnvOpenAIAPIKey] != LoopbackAuthPlaceholder {
		t.Fatalf("env placeholder=%q", plan.Env[EnvOpenAIAPIKey])
	}
	if strings.Contains(plan.Command, "secret-value") || plan.Env[EnvOpenAIAPIKey] == "secret-value-never-on-wire" {
		t.Fatal("secret leaked into launch plan")
	}
	raw, _ := json.Marshal(plan.Wire)
	if strings.Contains(string(raw), "secret-value") {
		t.Fatal("secret leaked into wire")
	}
	state, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "tmux:@1")
	if err != nil {
		t.Fatal(err)
	}
	if state.Binding.SessionID != "tmux:@1" || state.Binding.RouteID == "" {
		t.Fatalf("state=%#v", state)
	}
	snap, ok := owner.SessionSnapshot("tmux:@1")
	if !ok || snap.Current == nil || snap.Current.ConnectionID != profile.ID {
		t.Fatalf("snapshot=%#v ok=%v", snap, ok)
	}
}

func TestOwnerRawCommandBypassesProfiles(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "org/up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetDefault(ExecutorCodex, profile.ID, 1); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch("", "", "zsh -lc 'echo hi'")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Bypass || plan.Applied {
		t.Fatalf("expected bypass, got %#v", plan)
	}
}

func TestOwnerOfficialLoginBypassesWhenClientHasNoProviderDefault(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("available-but-not-selected", "gpt-5", "org/up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Bypass || plan.Applied || plan.Command != "codex" || len(plan.Env) != 0 {
		t.Fatalf("official-login launch must be untouched: %#v", plan)
	}
}

func TestOwnerDefaultEditDoesNotMutateLiveSession(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	a := codexResponsesProfile("a", "gpt-5", "up-a")
	b := codexResponsesProfile("b", "gpt-5", "up-b")
	if _, err := owner.UpsertProfile(a, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProfile(b, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.SetDefault(ExecutorCodex, "a", 2); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	before, _ := owner.Table().Get("s1")
	if _, err := owner.SetDefault(ExecutorCodex, "b", 3); err != nil {
		t.Fatal(err)
	}
	after, _ := owner.Table().Get("s1")
	if after.Binding.ProfileID != before.Binding.ProfileID || after.Generation != before.Generation {
		t.Fatalf("live session mutated: before=%#v after=%#v", before.Binding, after.Binding)
	}
}

func TestOwnerDeleteInUseRejected(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	_, err = owner.DeleteProfile(profile.ID, 1)
	if !errors.Is(err, ErrProfileInUse) {
		t.Fatalf("err=%v", err)
	}
}

func TestOwnerActivateCASAndPersist(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	a := codexResponsesProfile("a", "gpt-5", "up-a")
	b := codexResponsesProfile("b", "gpt-5", "up-b")
	b.BaseURL = a.BaseURL
	b.ProviderID = a.ProviderID
	if _, err := owner.UpsertProfile(a, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProfile(b, 1, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "a", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = owner.ActivateSession("s1", "b", 99)
	if !errors.Is(err, ErrBindingConflict) {
		t.Fatalf("cas err=%v", err)
	}
	state, _, _, err := owner.ActivateSession("s1", "b", 1)
	if err != nil {
		t.Fatal(err)
	}
	if state.Binding.ProfileID != "b" || state.Generation != 2 {
		t.Fatalf("state=%#v", state.Binding)
	}
	routeID := state.Binding.RouteID

	// Restart owner: same listener + route id restored.
	addr := owner.ListenAddr()
	profiles := owner.Store().Path()
	routes := owner.routes.Path()
	listener := owner.listener.Path()
	_ = owner.Close()

	owner2, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner2.Close() })
	if owner2.ListenAddr() != addr {
		t.Fatalf("listen addr changed %q -> %q", addr, owner2.ListenAddr())
	}
	restored, ok := owner2.Table().Get("s1")
	if !ok || restored.Binding.RouteID != routeID || restored.Binding.ProfileID != "b" {
		t.Fatalf("restored=%#v ok=%v", restored, ok)
	}
}

func TestOwnerCredentialNotReadyFailClosedOnLaunch(t *testing.T) {
	owner := startTestOwner(t, readyLookup(""))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	_, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if !errors.Is(err, ErrCredentialNotReady) {
		t.Fatalf("err=%v", err)
	}
}

func TestOwnerRouterServesBoundRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	profile.BaseURL = upstream.URL + "/v1"
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	state, _ := owner.Table().Get("s1")
	base, err := LoopbackCodexBaseURL(owner.ListenAddr(), state.Binding.RouteID)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Post(base+"/responses", "application/json", strings.NewReader(`{"model":"cli"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
}

func TestControlErrorCodeStable(t *testing.T) {
	if ControlErrorCode(ErrBindingBusy) != CodeBindingBusy {
		t.Fatal(ControlErrorCode(ErrBindingBusy))
	}
	if ControlErrorCode(ErrProfileInUse) != CodeProfileInUse {
		t.Fatal(ControlErrorCode(ErrProfileInUse))
	}
}
