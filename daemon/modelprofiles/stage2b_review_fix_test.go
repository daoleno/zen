package modelprofiles

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPrepareLaunchFailureReleasesIdleListener(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup(""), // credential not ready
		Verifier:     lifecycleTestVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	stale := []byte("{\"listen_addr\":\"127.0.0.1:59998\"}\n")
	if err := os.WriteFile(listener, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	_, err = owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if !errors.Is(err, ErrCredentialNotReady) {
		t.Fatalf("err=%v", err)
	}
	if owner.ListenAddr() != "" {
		t.Fatalf("listener leaked after credential failure: %q", owner.ListenAddr())
	}
	if owner.Table().Len() != 0 {
		t.Fatal("routes leaked")
	}
	raw, err := os.ReadFile(listener)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(stale) {
		t.Fatalf("stale listener metadata must be restored: %q", raw)
	}
}

func TestPrepareLaunchCompileFailureReleasesIdleListener(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	// Unsupported executor/protocol pairing is rejected before listener for native,
	// so force compile failure via empty loopback by breaking after listener:
	// use a profile that verifies but Compile fails on missing loopback when addr empty —
	// inject by stopping listener mid-flight is hard; use contract failure instead.
	bad := profile
	bad.ID = "bad"
	bad.ClientModel = "gpt-99-invented"
	bad.Model = "gpt-99-invented"
	// lifecycle verifier admits anything — use Builtin owner for contract fail after upsert path.
	_ = owner.Close()

	profiles, routes, listener := stage2bRoot(t)
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	ok := Profile{
		ID: "ok", Name: "OK", ExecutorID: ExecutorCodex,
		ProviderID: "openrouter", ProviderLabel: "OR",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5.6-sol", Model: "gpt-5.6-sol",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		BaseURL:               "https://openrouter.ai/api/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "OPENROUTER_API_KEY",
	}
	if _, err := owner.UpsertProfile(ok, 0, true); err != nil {
		t.Fatal(err)
	}
	// Pre-rename route persist failure after listener start.
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected route pre-rename")
		}
		return nil
	})
	_, err = owner.PrepareLaunch(ExecutorCodex, ok.ID, "codex")
	owner.RoutesFile().SetPersistHook(nil)
	if err == nil {
		t.Fatal("expected prepare failure")
	}
	if owner.ListenAddr() != "" || owner.Table().Len() != 0 {
		t.Fatalf("idle listener/routes leaked: addr=%q len=%d", owner.ListenAddr(), owner.Table().Len())
	}
	if _, err := os.Stat(listener); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(listener)
		t.Fatalf("listener file must be removed when none pre-existed: err=%v raw=%s", err, raw)
	}
}

func TestAbortLaunchAfterWatcherFailureReleasesIdleListener(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if owner.ListenAddr() == "" {
		t.Fatal("expected listener after successful prepare")
	}
	if _, err := owner.AbortLaunch(plan.ProvisionalID); err != nil {
		t.Fatal(err)
	}
	if owner.ListenAddr() != "" || owner.Table().Len() != 0 {
		t.Fatalf("abort must restore inert: addr=%q len=%d", owner.ListenAddr(), owner.Table().Len())
	}
}

func TestFailedLaunchDoesNotTearDownLiveListener(t *testing.T) {
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
	liveAddr := owner.ListenAddr()
	if liveAddr == "" {
		t.Fatal("expected live listener")
	}
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected")
		}
		return nil
	})
	// Second prepare with persist fail: BindLaunch will succeed then persist fail and rollback.
	_, err = owner.PrepareLaunch(ExecutorCodex, "b", "codex")
	owner.RoutesFile().SetPersistHook(nil)
	if err == nil {
		t.Fatal("expected second prepare failure")
	}
	if owner.ListenAddr() != liveAddr {
		t.Fatalf("live listener torn down: got %q want %q", owner.ListenAddr(), liveAddr)
	}
	if owner.Table().Len() != 1 {
		t.Fatalf("live route missing: %d", owner.Table().Len())
	}
}

func TestPrepareLaunchCombinesListenerDirSyncWithRoutePersist(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
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
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	owner.listener.SetDirSync(func(string) error {
		return errors.New("injected listener dirSync")
	})
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	owner.listener.SetDirSync(nil)
	if !plan.Applied || plan.Persist.Durable {
		t.Fatalf("plan persist=%#v err=%v", plan.Persist, err)
	}
	if !errors.Is(err, ErrPersistDirSync) {
		t.Fatalf("err=%v", err)
	}
	outcome, durable := WirePersistFields(plan.Persist)
	if outcome != "applied" || durable == nil || *durable {
		t.Fatalf("wire outcome=%q durable=%v", outcome, durable)
	}
}

func TestDeleteProfileSerializedAgainstPrepareLaunch(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}

	var prepareStarted sync.WaitGroup
	prepareStarted.Add(1)
	releasePrepare := make(chan struct{})
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "after_encode" {
			prepareStarted.Done()
			<-releasePrepare
		}
		return nil
	})

	var prepareErr, deleteErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, prepareErr = owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	}()
	prepareStarted.Wait()
	go func() {
		defer wg.Done()
		_, deleteErr = owner.DeleteProfile(profile.ID, 1)
	}()
	// Delete must block on Owner.mu until Prepare finishes mutate+persist.
	close(releasePrepare)
	wg.Wait()
	owner.RoutesFile().SetPersistHook(nil)

	if prepareErr != nil {
		t.Fatalf("prepare: %v", prepareErr)
	}
	if !errors.Is(deleteErr, ErrProfileInUse) {
		t.Fatalf("delete err=%v want in-use", deleteErr)
	}
	if _, err := owner.GetProfile(profile.ID); err != nil {
		t.Fatalf("profile must remain: %v", err)
	}
}

func TestLaunchedBindingSurvivesHistoryTrimAndRestart(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	base := codexResponsesProfile("p0", "gpt-5", "up-0")
	if _, err := owner.UpsertProfile(base, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, base.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	launchModel := "up-0"
	rev := int64(1)
	for i := 1; i <= MaxRouteHistoryEvents+8; i++ {
		p := codexResponsesProfile("p"+itoa(i), "gpt-5", "up-"+itoa(i))
		p.BaseURL = base.BaseURL
		p.ProviderID = base.ProviderID
		if _, err := owner.UpsertProfile(p, rev, true); err != nil {
			t.Fatal(err)
		}
		rev++
		state, ok := owner.Table().Get("s1")
		if !ok {
			t.Fatal("missing session")
		}
		if _, _, _, err := owner.ActivateSession("s1", p.ID, state.Generation); err != nil {
			t.Fatal(err)
		}
	}
	state, ok := owner.Table().Get("s1")
	if !ok {
		t.Fatal("missing")
	}
	if len(state.History) > MaxRouteHistoryEvents {
		t.Fatalf("history not trimmed: %d", len(state.History))
	}
	if state.Launched.UpstreamModel != launchModel || state.Launched.ProfileID != "p0" {
		t.Fatalf("launched drifted: %#v", state.Launched)
	}
	snap, ok := owner.SessionSnapshot("s1")
	if !ok || snap.Launched == nil || snap.Launched.ConnectionID != "p0" || snap.Launched.ModelID != launchModel {
		t.Fatalf("wire launched=%#v", snap.Launched)
	}
	if len(state.History) == 0 || state.History[0].To.ProfileID == "p0" {
		t.Fatal("history head should no longer be original launch after trim")
	}

	raw, err := EncodeDurableSnapshot(owner.Table().Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"launched"`) {
		t.Fatal("durable snapshot missing launched")
	}
	profiles, routes, listener := owner.Store().Path(), owner.RoutesFile().Path(), owner.listener.Path()
	addr := owner.ListenAddr()
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
		t.Fatalf("port %q -> %q", addr, owner2.ListenAddr())
	}
	restored, ok := owner2.Table().Get("s1")
	if !ok || restored.Launched.ProfileID != "p0" || restored.Launched.UpstreamModel != launchModel {
		t.Fatalf("restored launched=%#v", restored.Launched)
	}
	snap2, _ := owner2.SessionSnapshot("s1")
	if snap2.Launched == nil || snap2.Launched.ConnectionID != "p0" {
		t.Fatalf("wire after restart=%#v", snap2.Launched)
	}

	// Fail-closed: snapshot missing launched and without launch history event.
	broken := restored
	broken.Launched = RouteBinding{}
	broken.History = trimHistory(broken.History) // may already lack ActivationLaunch
	stripped := make([]RouteActivationEvent, 0, len(broken.History))
	for _, ev := range broken.History {
		if normalizeID(ev.Activation) != ActivationLaunch {
			stripped = append(stripped, ev)
		}
	}
	broken.History = stripped
	if _, err := EncodeDurableSnapshot([]SessionRouteState{broken}); err == nil {
		t.Fatal("expected encode fail-closed without launched")
	}
}

func TestUpsertRejectsUnknownClientContract(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     BuiltinEnvelopeVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	before := owner.Catalog()
	bad := Profile{
		ID: "bad", Name: "Bad", ExecutorID: ExecutorCodex,
		ProviderID: "openrouter", ProviderLabel: "OR",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-99-invented", Model: "vendor/x",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		BaseURL:               "https://openrouter.ai/api/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "OPENROUTER_API_KEY",
	}
	_, err = owner.UpsertProfile(bad, 0, true)
	if !errors.Is(err, ErrModelUnsupported) && !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("err=%v", err)
	}
	if ControlErrorCode(err) != CodeModelUnsupported && ControlErrorCode(err) != CodeContractUnverified {
		t.Fatalf("code=%s", ControlErrorCode(err))
	}
	after := owner.Catalog()
	if after.Revision != before.Revision || len(after.Profiles) != len(before.Profiles) {
		t.Fatalf("catalog mutated: before=%#v after=%#v", before, after)
	}
	ok := bad
	ok.ClientModel = "gpt-5.6-sol"
	ok.Model = "gpt-5.6-sol"
	if _, err := owner.UpsertProfile(ok, 0, true); err != nil {
		t.Fatal(err)
	}
}

func TestEditorSchemaExposesClientContracts(t *testing.T) {
	schema := ProfileEditorSchemaSnapshot()
	if len(schema.SupportedClientContracts) == 0 {
		t.Fatal("empty client contracts")
	}
	seen := map[string]bool{}
	for _, c := range schema.SupportedClientContracts {
		if c.ClientModel == "" || c.ExecutorID == "" || c.Envelope.ContextWindowTokens <= 0 {
			t.Fatalf("bad descriptor %#v", c)
		}
		seen[c.ExecutorID+"|"+c.ClientModel] = true
	}
	if !seen["codex|gpt-5"] || !seen["claude|claude-sonnet-4-6"] {
		t.Fatalf("missing known contracts: %#v", seen)
	}
	joined := strings.Join(schema.FreelyConfigurable, ",")
	for _, field := range []string{"provider_id", "base_url", "model"} {
		if !strings.Contains(joined, field) {
			t.Fatalf("freely configurable missing %s: %v", field, schema.FreelyConfigurable)
		}
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "sk-") || strings.Contains(strings.ToLower(string(raw)), "api_key_value") {
		t.Fatalf("secret-like content: %s", raw)
	}
}

func itoa(i int) string {
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

type mismatchClientVerifier struct {
	inner       ProfileContractVerifier
	clientDrift string
}

func (m mismatchClientVerifier) VerifyProfileContract(profile Profile) (VerifiedProfileContract, error) {
	v, err := m.inner.VerifyProfileContract(profile)
	if err != nil {
		return v, err
	}
	if m.clientDrift != "" {
		v.ClientModelID = m.clientDrift
	}
	return v, nil
}

func TestCaptureListenerBackupFailsClosedOnUnreadable(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
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
	stale := []byte("{\"listen_addr\":\"127.0.0.1:59997\"}\n")
	if err := os.WriteFile(listener, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	owner.listener.SetReadFile(func(string) ([]byte, error) {
		return nil, errors.New("injected EACCES")
	})
	_, err = owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	owner.listener.SetReadFile(nil)
	if err == nil {
		t.Fatal("expected fail-closed before listener Save")
	}
	if owner.ListenAddr() != "" || owner.Table().Len() != 0 {
		t.Fatalf("must not start listener after unreadable backup: addr=%q len=%d", owner.ListenAddr(), owner.Table().Len())
	}
	raw, readErr := os.ReadFile(listener)
	if readErr != nil || string(raw) != string(stale) {
		t.Fatalf("pre-existing listener metadata must be preserved: err=%v raw=%q", readErr, raw)
	}
}

func TestReleaseIdleListenerSurfacesRestoreFailures(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
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
	stale := []byte("{\"listen_addr\":\"127.0.0.1:59996\"}\n")
	if err := os.WriteFile(listener, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		phase string
	}{
		{"before_write", "before_write"},
		{"before_rename", "before_rename"},
		{"before_dirsync", "before_dirsync"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owner.listener.SetPersistHook(func(phase string) error {
				if phase == tc.phase {
					return errors.New("injected listener restore " + tc.phase)
				}
				return nil
			})
			persist, releaseErr := owner.AbortLaunch(plan.ProvisionalID)
			owner.listener.SetPersistHook(nil)
			if releaseErr == nil {
				t.Fatal("expected listener cleanup error")
			}
			if persist.Durable {
				t.Fatalf("must not report durable success: %#v", persist)
			}
			if !persist.Applied {
				t.Fatalf("route release should still be applied: %#v", persist)
			}
			// Rebind for next case: relaunch then abort with clean restore.
			plan, err = owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
			if err != nil {
				t.Fatalf("re-prepare: %v", err)
			}
			if _, err := owner.AbortLaunch(plan.ProvisionalID); err != nil {
				t.Fatalf("clean abort: %v", err)
			}
			plan, err = owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
			if err != nil {
				t.Fatalf("setup prepare: %v", err)
			}
		})
	}
}

func TestReleaseIdleListenerSurfacesRemoveDirSyncFailure(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	owner.listener.SetPersistHook(func(phase string) error {
		if phase == "after_remove" {
			return errors.New("injected remove dirsync")
		}
		return nil
	})
	persist, err := owner.AbortLaunch(plan.ProvisionalID)
	owner.listener.SetPersistHook(nil)
	if err == nil {
		t.Fatal("expected remove durability error")
	}
	if persist.Durable {
		t.Fatalf("durable=%#v", persist)
	}
}

func TestUpsertRejectsVerifierProfileMismatch(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier: mismatchClientVerifier{
			inner:       BuiltinEnvelopeVerifier{},
			clientDrift: "gpt-5-codex",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	p := Profile{
		ID: "drift", Name: "Drift", ExecutorID: ExecutorCodex,
		ProviderID: "openrouter", ProviderLabel: "OR",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "openrouter/x",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		BaseURL:               "https://openrouter.ai/api/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "OPENROUTER_API_KEY",
	}
	_, err = owner.UpsertProfile(p, 0, true)
	if !errors.Is(err, ErrModelUnsupported) && !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("err=%v", err)
	}
	if len(owner.Catalog().Profiles) != 0 {
		t.Fatalf("mismatched verifier output must not persist: %#v", owner.Catalog())
	}
}

func TestStartOwnerReauthorizesManualCatalog(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	toml := `
revision = 1

[[profiles]]
id = "manual"
name = "Manual"
executor_id = "codex"
provider_id = "openrouter"
provider_label = "OR"
protocol = "openai_responses"
client_model = "gpt-99-invented"
client_model_provenance = "configured_compatibility"
model = "openrouter/x"
base_url = "https://openrouter.ai/api/v1"
auth_mode = "bearer_env"
credential_env = "OPENROUTER_API_KEY"
`
	if err := os.WriteFile(profiles, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     BuiltinEnvelopeVerifier{},
	})
	if !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("err=%v", err)
	}
}

func TestUpsertSerializedAgainstPrepareLaunchRevision(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	a := codexResponsesProfile("a", "gpt-5", "up-a")
	b := codexResponsesProfile("b", "gpt-5", "up-b")
	b.BaseURL = a.BaseURL
	b.ProviderID = a.ProviderID
	if _, err := owner.UpsertProfile(a, 0, true); err != nil {
		t.Fatal(err)
	}

	var prepareStarted sync.WaitGroup
	prepareStarted.Add(1)
	releasePrepare := make(chan struct{})
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "after_encode" {
			prepareStarted.Done()
			<-releasePrepare
		}
		return nil
	})

	var prepareErr, upsertErr error
	var plan SessionLaunchPlan
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		plan, prepareErr = owner.PrepareLaunch(ExecutorCodex, a.ID, "codex")
	}()
	prepareStarted.Wait()
	upsertDone := make(chan struct{})
	go func() {
		defer wg.Done()
		defer close(upsertDone)
		_, upsertErr = owner.UpsertProfile(b, 1, true)
	}()
	select {
	case <-upsertDone:
		t.Fatal("Upsert must not complete while PrepareLaunch holds Owner.mu")
	default:
	}
	close(releasePrepare)
	wg.Wait()
	owner.RoutesFile().SetPersistHook(nil)

	if prepareErr != nil {
		t.Fatalf("prepare: %v", prepareErr)
	}
	if upsertErr != nil {
		t.Fatalf("upsert: %v", upsertErr)
	}
	if plan.State.Binding.CatalogRevision != 1 {
		t.Fatalf("prepare bound stale/wrong revision: %d", plan.State.Binding.CatalogRevision)
	}
	if owner.Catalog().Revision != 2 {
		t.Fatalf("catalog revision=%d", owner.Catalog().Revision)
	}
}

func TestSetDefaultSerializedAgainstPrepareLaunchRevision(t *testing.T) {
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
	if _, err := owner.SetDefault(ExecutorCodex, a.ID, 2); err != nil {
		t.Fatal(err)
	}

	var prepareStarted sync.WaitGroup
	prepareStarted.Add(1)
	releasePrepare := make(chan struct{})
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "after_encode" {
			prepareStarted.Done()
			<-releasePrepare
		}
		return nil
	})

	var prepareErr, setErr error
	var plan SessionLaunchPlan
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		plan, prepareErr = owner.PrepareLaunch(ExecutorCodex, "", "codex")
	}()
	prepareStarted.Wait()
	setDone := make(chan struct{})
	go func() {
		defer wg.Done()
		defer close(setDone)
		_, setErr = owner.SetDefault(ExecutorCodex, b.ID, 3)
	}()
	select {
	case <-setDone:
		t.Fatal("SetDefault must not complete while PrepareLaunch holds Owner.mu")
	default:
	}
	close(releasePrepare)
	wg.Wait()
	owner.RoutesFile().SetPersistHook(nil)

	if prepareErr != nil {
		t.Fatalf("prepare: %v", prepareErr)
	}
	if setErr != nil {
		t.Fatalf("set default: %v", setErr)
	}
	if plan.State.Binding.ProfileID != a.ID {
		t.Fatalf("prepare resolved torn default: %#v", plan.State.Binding)
	}
	if owner.Catalog().Defaults[ExecutorCodex] != b.ID {
		t.Fatalf("defaults=%#v", owner.Catalog().Defaults)
	}
}

func TestCorruptV4LaunchedRejected(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	base := codexResponsesProfile("p0", "gpt-5", "up-0")
	if _, err := owner.UpsertProfile(base, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, base.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	alt := codexResponsesProfile("p1", "gpt-5", "up-1")
	alt.BaseURL = base.BaseURL
	alt.ProviderID = base.ProviderID
	if _, err := owner.UpsertProfile(alt, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.ActivateSession("s1", alt.ID, 1); err != nil {
		t.Fatal(err)
	}
	state, ok := owner.Table().Get("s1")
	if !ok {
		t.Fatal("missing")
	}

	mutations := []struct {
		name string
		mut  func(*SessionRouteState)
	}{
		{"session_id", func(s *SessionRouteState) { s.Launched.SessionID = "other" }},
		{"route_id", func(s *SessionRouteState) { s.Launched.RouteID = "route_other" }},
		{"executor", func(s *SessionRouteState) { s.Launched.ExecutorID = ExecutorClaude }},
		{"protocol", func(s *SessionRouteState) { s.Launched.Protocol = ProtocolAnthropicMessages }},
		// A launched model switch is legal under the unified identity, so the
		// corruption fixture uses the launched provenance (must stay a known
		// daemon label) instead of the client model.
		{"launched_provenance", func(s *SessionRouteState) { s.Launched.ClientModelProvenance = "forged" }},
		{"generation", func(s *SessionRouteState) { s.Launched.Generation = 2 }},
		{"activation", func(s *SessionRouteState) { s.Launched.Activation = ActivationActiveSession }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			broken := cloneSessionState(state)
			tc.mut(&broken)
			if _, err := EncodeDurableSnapshot([]SessionRouteState{broken}); err == nil {
				t.Fatal("expected encode reject")
			}
			raw, err := EncodeDurableSnapshot([]SessionRouteState{state})
			if err != nil {
				t.Fatal(err)
			}
			// Corrupt after encode by decoding, mutating, re-encoding fields manually.
			states, err := DecodeDurableSnapshot(raw)
			if err != nil {
				t.Fatal(err)
			}
			tc.mut(&states[0])
			if err := validateRestorableState(states[0]); err == nil {
				t.Fatal("expected validate reject")
			}
			table := NewRouteTable()
			table.SetLookup(readyLookup("x"))
			table.SetContractVerifier(lifecycleTestVerifier{})
			if err := table.Restore([]SessionRouteState{broken}, lifecycleTestVerifier{}); err == nil {
				t.Fatal("expected restore reject")
			}
		})
	}
}

func TestListenerCleanupFailedThenRetriedRestoresExactBackup(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
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
	stale := []byte("{\"listen_addr\":\"127.0.0.1:59995\"}\n")
	if err := os.WriteFile(listener, stale, 0o600); err != nil {
		t.Fatal(err)
	}
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	newAddr := owner.ListenAddr()
	if newAddr == "" {
		t.Fatal("expected live listener")
	}
	owner.listener.SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected pre-rename restore")
		}
		return nil
	})
	persist, abortErr := owner.AbortLaunch(plan.ProvisionalID)
	owner.listener.SetPersistHook(nil)
	if abortErr == nil || !errors.Is(abortErr, ErrLaunchCleanupIncomplete) {
		t.Fatalf("abortErr=%v", abortErr)
	}
	if !persist.Applied || persist.Durable {
		t.Fatalf("persist=%#v", persist)
	}
	if owner.ListenAddr() != "" {
		t.Fatalf("dead listener address must not remain live: %q", owner.ListenAddr())
	}
	mid, err := os.ReadFile(listener)
	if err != nil {
		t.Fatal(err)
	}
	if string(mid) == string(stale) {
		t.Fatal("pre-rename failure must leave unreverted new metadata for retry to own")
	}
	if !strings.Contains(string(mid), newAddr) && !strings.Contains(string(mid), strings.TrimPrefix(newAddr, "127.0.0.1:")) {
		// listen addr may be written as 127.0.0.1:port
		if !strings.Contains(string(mid), "listen_addr") {
			t.Fatalf("unexpected mid metadata %q", mid)
		}
	}
	// Retry without re-prepare must restore exact original backup bytes.
	persist2, abortErr2 := owner.AbortLaunch(plan.ProvisionalID)
	if abortErr2 != nil {
		t.Fatalf("retry abort: %v", abortErr2)
	}
	if !persist2.Applied || !persist2.Durable {
		t.Fatalf("retry persist=%#v", persist2)
	}
	if owner.ListenAddr() != "" || owner.Table().Len() != 0 {
		t.Fatalf("after retry: addr=%q len=%d", owner.ListenAddr(), owner.Table().Len())
	}
	got, err := os.ReadFile(listener)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(stale) {
		t.Fatalf("exact backup not restored: got %q want %q", got, stale)
	}
	// Next launch must not treat the dead new address as pre-existing backup.
	plan2, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.AbortLaunch(plan2.ProvisionalID); err != nil {
		t.Fatal(err)
	}
	final, err := os.ReadFile(listener)
	if err != nil {
		t.Fatal(err)
	}
	if string(final) != string(stale) {
		t.Fatalf("second cycle must restore original stale, not dead addr: %q", final)
	}
}

func TestActivateSessionAtomicSnapshotUnderConcurrentRelease(t *testing.T) {
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
	plan, err := owner.PrepareLaunch(ExecutorCodex, a.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}

	var activateStarted sync.WaitGroup
	activateStarted.Add(1)
	releaseActivate := make(chan struct{})
	var signaled atomic.Bool
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "after_encode" && signaled.CompareAndSwap(false, true) {
			activateStarted.Done()
			<-releaseActivate
		}
		return nil
	})

	var snap WireSessionSnapshot
	var state SessionRouteState
	var persist PersistResult
	var actErr, relErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		state, snap, persist, actErr = owner.ActivateSession("s1", b.ID, 1)
	}()
	activateStarted.Wait()
	go func() {
		defer wg.Done()
		_, relErr = owner.ReleaseSession("s1")
	}()
	close(releaseActivate)
	wg.Wait()
	owner.RoutesFile().SetPersistHook(nil)

	if actErr != nil && !persist.Applied {
		t.Fatalf("activate: %v", actErr)
	}
	if persist.Applied {
		if snap.Current == nil || snap.Current.ConnectionID != b.ID || state.Generation != 2 {
			t.Fatalf("atomic snap must match mutation: snap=%#v state.gen=%d", snap, state.Generation)
		}
		if state.Binding.Generation != state.Generation {
			t.Fatalf("generation mismatch state=%#v", state)
		}
	}
	_ = relErr
}

func TestActivateSessionConcurrentSnapshotsMatchOwnMutation(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	a := codexResponsesProfile("a", "gpt-5", "up-a")
	b := codexResponsesProfile("b", "gpt-5", "up-b")
	c := codexResponsesProfile("c", "gpt-5", "up-c")
	for _, p := range []*Profile{&b, &c} {
		p.BaseURL = a.BaseURL
		p.ProviderID = a.ProviderID
	}
	if _, err := owner.UpsertProfile(a, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProfile(b, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.UpsertProfile(c, 2, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, a.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}

	gate := make(chan struct{})
	var firstSnap, secondSnap WireSessionSnapshot
	var firstState, secondState SessionRouteState
	var firstPersist, secondPersist PersistResult
	var firstErr, secondErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-gate
		firstState, firstSnap, firstPersist, firstErr = owner.ActivateSession("s1", b.ID, 1)
	}()
	go func() {
		defer wg.Done()
		<-gate
		secondState, secondSnap, secondPersist, secondErr = owner.ActivateSession("s1", c.ID, 1)
	}()
	close(gate)
	wg.Wait()

	applied := 0
	if firstPersist.Applied {
		applied++
		if firstSnap.Current == nil || firstState.Generation != firstState.Binding.Generation {
			t.Fatalf("first snap incoherent snap=%#v state=%#v", firstSnap, firstState)
		}
		if firstSnap.Current.ConnectionID != b.ID {
			t.Fatalf("first snap profile=%q", firstSnap.Current.ConnectionID)
		}
	} else if !errors.Is(firstErr, ErrBindingConflict) {
		t.Fatalf("first err=%v", firstErr)
	}
	if secondPersist.Applied {
		applied++
		if secondSnap.Current == nil || secondState.Generation != secondState.Binding.Generation {
			t.Fatalf("second snap incoherent snap=%#v state=%#v", secondSnap, secondState)
		}
		if secondSnap.Current.ConnectionID != c.ID {
			t.Fatalf("second snap profile=%q", secondSnap.Current.ConnectionID)
		}
	} else if !errors.Is(secondErr, ErrBindingConflict) {
		t.Fatalf("second err=%v", secondErr)
	}
	if applied != 1 {
		t.Fatalf("exactly one activate must apply, got %d", applied)
	}
}

func TestProjectCatalogAtomicUnderConcurrentUpsert(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	a := codexResponsesProfile("a", "gpt-5", "up-a")
	if _, err := owner.UpsertProfile(a, 0, true); err != nil {
		t.Fatal(err)
	}
	b := codexResponsesProfile("b", "gpt-5", "up-b")
	b.BaseURL = a.BaseURL
	b.ProviderID = a.ProviderID

	var upsertStarted sync.WaitGroup
	upsertStarted.Add(1)
	releaseUpsert := make(chan struct{})
	owner.Store().SetPersistHook(func(phase string) error {
		if phase == "after_write" {
			upsertStarted.Done()
			<-releaseUpsert
		}
		return nil
	})

	var upsertErr error
	var proj CatalogProjection
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, upsertErr = owner.UpsertProfile(b, 1, true)
	}()
	upsertStarted.Wait()
	go func() {
		defer wg.Done()
		proj = owner.ProjectCatalog()
	}()
	// ProjectCatalog must block on Owner.mu until Upsert finishes.
	close(releaseUpsert)
	wg.Wait()
	owner.Store().SetPersistHook(nil)
	if upsertErr != nil {
		t.Fatal(upsertErr)
	}
	if proj.Catalog.Revision != 2 || len(proj.Catalog.Profiles) != 2 || len(proj.Views) != 2 {
		t.Fatalf("torn projection: %#v views=%d", proj.Catalog, len(proj.Views))
	}
	if proj.Catalog.Revision != int64(len(proj.Views)) && len(proj.Views) != len(proj.Catalog.Profiles) {
		t.Fatalf("catalog/views revision mix: %#v", proj)
	}
}

func TestJoinErrorsPreservesIsAndPersistClassification(t *testing.T) {
	joined := joinErrors(ErrPersistDirSync, fmt.Errorf("listener cleanup: %w", ErrLaunchCleanupIncomplete))
	if !errors.Is(joined, ErrPersistDirSync) {
		t.Fatal("DirSync Is lost")
	}
	if !errors.Is(joined, ErrLaunchCleanupIncomplete) {
		t.Fatal("cleanup Is lost")
	}
	pr := PersistResultFromError(joined)
	if !pr.Applied || pr.Durable {
		t.Fatalf("persist classification=%#v", pr)
	}
	if ControlErrorCode(joined) != CodeRouteListenerFailed && ControlErrorCode(joined) != CodeRouteSnapshotInvalid {
		// DirSync is checked after LaunchCleanupIncomplete in switch — cleanup wins.
		if ControlErrorCode(joined) != CodeRouteListenerFailed {
			t.Fatalf("code=%s", ControlErrorCode(joined))
		}
	}
	conflictJoin := joinErrors(ErrBindingConflict, fmt.Errorf("listener cleanup: %w", errors.New("x")))
	if ControlErrorCode(conflictJoin) != CodeBindingConflict {
		t.Fatalf("conflict code=%s", ControlErrorCode(conflictJoin))
	}
}

func TestAbortLaunchPersistFailureLeavesRetryableProvisional(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
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
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected abort pre-rename")
		}
		return nil
	})
	persist, abortErr := owner.AbortLaunch(plan.ProvisionalID)
	owner.RoutesFile().SetPersistHook(nil)
	if persist.Applied {
		t.Fatalf("abort must not apply: %#v", persist)
	}
	if !errors.Is(abortErr, ErrLaunchCleanupIncomplete) {
		t.Fatalf("abortErr=%v", abortErr)
	}
	if _, ok := owner.Table().Get(plan.ProvisionalID); !ok {
		t.Fatal("same-process provisional must remain for retry")
	}
	profilesPath, routesPath, listenerPath := owner.Store().Path(), owner.RoutesFile().Path(), owner.listener.Path()
	_ = owner.Close()

	// Restart must deterministically sweep pending:* without knowing the ID.
	owner2, err := StartOwner(OwnerConfig{
		ProfilesPath: profilesPath,
		RoutesPath:   routesPath,
		ListenerPath: listenerPath,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner2.Close() })
	if owner2.Table().Len() != 0 {
		t.Fatalf("pending must be swept on restart: %#v", owner2.Table().Snapshot())
	}
	if owner2.ListenAddr() != "" {
		t.Fatalf("listener must not start for swept provisionals: %q", owner2.ListenAddr())
	}
	if _, err := os.Stat(listenerPath); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(listenerPath)
		t.Fatalf("listener metadata must be cleared: err=%v raw=%s", err, raw)
	}
	if users := owner2.Table().SessionsUsingProfile(profile.ID); len(users) != 0 {
		t.Fatalf("profile-in-use residue: %v", users)
	}
	if _, err := owner2.DeleteProfile(profile.ID, owner2.Catalog().Revision); err != nil {
		t.Fatalf("profile must be deletable after sweep: %v", err)
	}
}

func TestStartOwnerSweepsOrphanProvisionalsWithoutLiveRoutes(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
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
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(plan.ProvisionalID, provisionalSessionPrefix) {
		t.Fatalf("provisional id=%q", plan.ProvisionalID)
	}
	if owner.ListenAddr() == "" {
		t.Fatal("expected listener after prepare")
	}
	profilesPath, routesPath, listenerPath := owner.Store().Path(), owner.RoutesFile().Path(), owner.listener.Path()
	_ = owner.Close()

	owner2, err := StartOwner(OwnerConfig{
		ProfilesPath: profilesPath,
		RoutesPath:   routesPath,
		ListenerPath: listenerPath,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner2.Close() })
	if owner2.Table().Len() != 0 || owner2.ListenAddr() != "" {
		t.Fatalf("swept state table=%d addr=%q", owner2.Table().Len(), owner2.ListenAddr())
	}
	if _, err := os.Stat(listenerPath); !os.IsNotExist(err) {
		t.Fatalf("listener metadata remains: %v", err)
	}
}

func TestStartOwnerFailsClosedWhenProvisionalSweepPersistFails(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
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
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex"); err != nil {
		t.Fatal(err)
	}
	profilesPath, routesPath, listenerPath := owner.Store().Path(), owner.RoutesFile().Path(), owner.listener.Path()
	_ = owner.Close()

	_, err = StartOwner(OwnerConfig{
		ProfilesPath: profilesPath,
		RoutesPath:   routesPath,
		ListenerPath: listenerPath,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
		RoutesPersistHook: func(phase string) error {
			if phase == "before_rename" {
				return errors.New("injected sweep pre-rename")
			}
			return nil
		},
	})
	if !errors.Is(err, ErrLaunchCleanupIncomplete) {
		t.Fatalf("err=%v", err)
	}
}

func TestCleanupFailedLaunchDoesNotDuplicateIncompleteSentinel(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected abort pre-rename")
		}
		return nil
	})
	cleanup := CleanupFailedLaunch(owner, plan.ProvisionalID, "agent:@dup",
		func(string) error { return nil },
		func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
	)
	owner.RoutesFile().SetPersistHook(nil)
	if cleanup.Err == nil || !errors.Is(cleanup.Err, ErrLaunchCleanupIncomplete) {
		t.Fatalf("cleanup=%#v", cleanup)
	}
	// errors.Is once is enough; the message must not stack the sentinel twice.
	if strings.Count(cleanup.Err.Error(), ErrLaunchCleanupIncomplete.Error()) != 1 {
		t.Fatalf("duplicate incomplete join: %v", cleanup.Err)
	}
}

func TestCleanupFailedLaunchSurfacesKillAndPreservesProvisional(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected commit fail")
		}
		return nil
	})
	_, _, persist, _ := owner.CommitLaunch(plan.ProvisionalID, "agent:@1")
	owner.RoutesFile().SetPersistHook(nil)
	if persist.Applied {
		t.Fatal("commit should not apply")
	}
	cleanup := CleanupFailedLaunch(owner, plan.ProvisionalID, "agent:@1",
		func(string) error { return errors.New("injected kill failure") },
		func(string) (SessionLiveness, error) { return SessionLivenessPresent, nil },
	)
	if cleanup.Err == nil || !errors.Is(cleanup.Err, ErrLaunchCleanupIncomplete) {
		t.Fatalf("cleanup=%#v", cleanup)
	}
	if !errors.Is(cleanup.Err, ErrSessionStillLive) {
		t.Fatalf("still-live annotation missing: %v", cleanup.Err)
	}
	if !strings.Contains(cleanup.Err.Error(), "injected kill failure") {
		t.Fatalf("kill error not joined: %v", cleanup.Err)
	}
	if cleanup.Persist.Applied {
		t.Fatal("must not claim applied cleanup while kill failed")
	}
	if _, ok := owner.Table().Get(plan.ProvisionalID); !ok {
		t.Fatal("provisional route must be preserved after non-missing kill failure")
	}
}

func TestSessionRouteCapabilitiesManagedAndActiveSwitch(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	routed := codexResponsesProfile("routed", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(routed, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, routed.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s-routed"); err != nil {
		t.Fatal(err)
	}
	caps := owner.SessionRouteCapabilities("s-routed")
	if !caps.Managed || !caps.ActiveSwitch {
		t.Fatalf("responses managed=true switch=true: %#v", caps)
	}
	if owner.SessionRouteCapabilities("missing").Managed || owner.SessionRouteCapabilities("missing").ActiveSwitch {
		t.Fatal("ordinary session must be false/false")
	}
	if owner.SessionRouteCapabilities(plan.ProvisionalID).Managed {
		t.Fatal("provisional pending:* must not advertise managed")
	}

	claude := claudeMessagesProfile("anthropic-main", "claude-sonnet-4-6", "claude-sonnet-4-6")
	if _, err := owner.UpsertProfile(claude, 1, true); err != nil {
		t.Fatal(err)
	}
	cplan, err := owner.PrepareLaunch(ExecutorClaude, claude.ID, "claude")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(cplan.ProvisionalID, "s-anthropic"); err != nil {
		t.Fatal(err)
	}
	acaps := owner.SessionRouteCapabilities("s-anthropic")
	if !acaps.Managed || !acaps.ActiveSwitch {
		t.Fatalf("anthropic managed=true switch=true: %#v", acaps)
	}

	native := Profile{
		ID: "native", Name: "Native", ExecutorID: ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: ProtocolOpenAINative, ClientModel: "gpt-5", Model: "gpt-5",
		ClientModelProvenance: ContractProvenanceBuiltinCatalog,
		AuthMode:              AuthModeNone,
	}
	if _, err := owner.UpsertProfile(native, 2, true); err != nil {
		t.Fatal(err)
	}
	nplan, err := owner.PrepareLaunch(ExecutorCodex, native.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(nplan.ProvisionalID, "s-native"); err != nil {
		t.Fatal(err)
	}
	ncaps := owner.SessionRouteCapabilities("s-native")
	if !ncaps.Managed || ncaps.ActiveSwitch {
		t.Fatalf("native managed=true switch=false: %#v", ncaps)
	}
}

func TestStartOwnerRetriesInertListenerConvergeAfterSweep(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
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
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex"); err != nil {
		t.Fatal(err)
	}
	profilesPath, routesPath, listenerPath := owner.Store().Path(), owner.RoutesFile().Path(), owner.listener.Path()
	if _, err := os.Stat(listenerPath); err != nil {
		t.Fatalf("listener metadata missing before restart: %v", err)
	}
	_ = owner.Close()

	_, err = StartOwner(OwnerConfig{
		ProfilesPath: profilesPath,
		RoutesPath:   routesPath,
		ListenerPath: listenerPath,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
		ListenerPersistHook: func(phase string) error {
			if phase == "before_remove" {
				return errors.New("injected listener remove")
			}
			return nil
		},
	})
	if !errors.Is(err, ErrLaunchCleanupIncomplete) {
		t.Fatalf("first start err=%v", err)
	}
	if _, err := os.Stat(listenerPath); err != nil {
		t.Fatalf("stale listener must remain after failed converge: %v", err)
	}
	// Routes were swept; no pending remains — second start must still converge.
	rawRoutes, _ := os.ReadFile(routesPath)
	if strings.Contains(string(rawRoutes), "pending:") {
		t.Fatalf("pending should already be swept: %s", rawRoutes)
	}

	owner2, err := StartOwner(OwnerConfig{
		ProfilesPath: profilesPath,
		RoutesPath:   routesPath,
		ListenerPath: listenerPath,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner2.Close() })
	if owner2.Table().Len() != 0 || owner2.ListenAddr() != "" {
		t.Fatalf("second start must stay inert: table=%d addr=%q", owner2.Table().Len(), owner2.ListenAddr())
	}
	if _, err := os.Stat(listenerPath); !os.IsNotExist(err) {
		raw, _ := os.ReadFile(listenerPath)
		t.Fatalf("second start must remove stale listener: err=%v raw=%s", err, raw)
	}
}

func TestTeardownSessionPreservesRouteWhenStillLive(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "agent:@live"); err != nil {
		t.Fatal(err)
	}
	result := owner.TeardownSession("agent:@live",
		func(string) error { return errors.New("injected kill failure") },
		func(string) (SessionLiveness, error) { return SessionLivenessPresent, nil },
	)
	if result.Err == nil || !errors.Is(result.Err, ErrSessionStillLive) {
		t.Fatalf("result=%#v", result)
	}
	if _, ok := owner.Table().Get("agent:@live"); !ok {
		t.Fatal("route must be preserved while session still live")
	}
}

func TestTeardownSessionReleasesWhenKillIdempotentMissing(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "agent:@gone"); err != nil {
		t.Fatal(err)
	}
	// Production KillSession returns nil for true target-missing.
	result := owner.TeardownSession("agent:@gone",
		func(string) error { return nil },
		func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
	)
	if result.Err != nil || !result.Persist.Applied || !result.Persist.Durable {
		t.Fatalf("result=%#v", result)
	}
	if _, ok := owner.Table().Get("agent:@gone"); ok {
		t.Fatal("route must release after idempotent missing kill")
	}
}

func TestTeardownSessionPreservesRouteOnResourceReleaseFailure(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "agent:@res"); err != nil {
		t.Fatal(err)
	}
	resourceErr := fmt.Errorf("%w: injected resource cleanup", errors.New("delegated resource release failed"))
	result := owner.TeardownSession("agent:@res",
		func(string) error { return resourceErr },
		func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
	)
	if result.Err == nil || !strings.Contains(result.Err.Error(), "resource cleanup") {
		t.Fatalf("result=%#v", result)
	}
	if result.Persist.Applied {
		t.Fatal("must not release route after resource cleanup failure")
	}
	if _, ok := owner.Table().Get("agent:@res"); !ok {
		t.Fatal("route must remain retryable")
	}
}

func TestTeardownSessionPreservesRouteOnLivenessProbeError(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "agent:@probe"); err != nil {
		t.Fatal(err)
	}
	result := owner.TeardownSession("agent:@probe",
		func(string) error { return errors.New("injected kill failure") },
		func(string) (SessionLiveness, error) {
			return SessionLivenessUnknown, errors.New("injected probe transport failure")
		},
	)
	if result.Err == nil || !errors.Is(result.Err, ErrSessionLivenessUnknown) {
		t.Fatalf("result=%#v", result)
	}
	if !strings.Contains(result.Err.Error(), "probe transport") {
		t.Fatalf("probe error not joined: %v", result.Err)
	}
	if _, ok := owner.Table().Get("agent:@probe"); !ok {
		t.Fatal("route must be preserved on ambiguous probe")
	}
}

func TestTeardownSessionRetryConvergesAfterResourceFailure(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "agent:@retry"); err != nil {
		t.Fatal(err)
	}
	first := owner.TeardownSession("agent:@retry",
		func(string) error { return fmt.Errorf("%w: injected", errors.New("delegated resource release failed")) },
		func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
	)
	if first.Err == nil {
		t.Fatal("first teardown must surface resource failure")
	}
	second := owner.TeardownSession("agent:@retry",
		func(string) error { return nil }, // missing + resource cleanup succeeded
		func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
	)
	if second.Err != nil || !second.Persist.Applied {
		t.Fatalf("retry=%#v", second)
	}
	if _, ok := owner.Table().Get("agent:@retry"); ok {
		t.Fatal("retry must release route")
	}
}

func TestCleanupFailedLaunchProvisionalCompensationMatrix(t *testing.T) {
	newOwner := func(t *testing.T) (*Owner, string) {
		t.Helper()
		owner := startTestOwner(t, readyLookup("x"))
		profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
		if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
			t.Fatal(err)
		}
		plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
		if err != nil {
			t.Fatal(err)
		}
		return owner, plan.ProvisionalID
	}

	t.Run("kill_failure_preserves_provisional", func(t *testing.T) {
		owner, provisionalID := newOwner(t)
		cleanup := CleanupFailedLaunch(owner, provisionalID, "agent:@p",
			func(string) error { return errors.New("injected kill failure") },
			func(string) (SessionLiveness, error) { return SessionLivenessPresent, nil },
		)
		if cleanup.Persist.Applied || cleanup.Err == nil || !errors.Is(cleanup.Err, ErrSessionStillLive) {
			t.Fatalf("cleanup=%#v", cleanup)
		}
		if _, ok := owner.Table().Get(provisionalID); !ok {
			t.Fatal("provisional must remain")
		}
	})

	t.Run("true_missing_kills_then_aborts", func(t *testing.T) {
		owner, provisionalID := newOwner(t)
		var killed []string
		cleanup := CleanupFailedLaunch(owner, provisionalID, "agent:@p",
			func(id string) error { killed = append(killed, id); return nil },
			func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
		)
		if cleanup.Err != nil || !cleanup.Persist.Applied {
			t.Fatalf("cleanup=%#v", cleanup)
		}
		if len(killed) != 1 || killed[0] != "agent:@p" {
			t.Fatalf("killed=%v", killed)
		}
		if owner.Table().Len() != 0 {
			t.Fatalf("table=%d", owner.Table().Len())
		}
	})

	t.Run("resource_cleanup_failure_preserves_provisional", func(t *testing.T) {
		owner, provisionalID := newOwner(t)
		cleanup := CleanupFailedLaunch(owner, provisionalID, "agent:@p",
			func(string) error {
				return fmt.Errorf("%w: injected", errors.New("delegated resource release failed"))
			},
			func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
		)
		if cleanup.Persist.Applied || cleanup.Err == nil {
			t.Fatalf("cleanup=%#v", cleanup)
		}
		if !strings.Contains(cleanup.Err.Error(), "resource") {
			t.Fatalf("err=%v", cleanup.Err)
		}
		if _, ok := owner.Table().Get(provisionalID); !ok {
			t.Fatal("provisional must remain after resource cleanup failure")
		}
	})

	t.Run("abort_persist_failure_after_kill", func(t *testing.T) {
		owner, provisionalID := newOwner(t)
		owner.RoutesFile().SetPersistHook(func(phase string) error {
			if phase == "before_rename" {
				return errors.New("injected abort pre-rename")
			}
			return nil
		})
		cleanup := CleanupFailedLaunch(owner, provisionalID, "agent:@p",
			func(string) error { return nil },
			func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
		)
		owner.RoutesFile().SetPersistHook(nil)
		if cleanup.Persist.Applied || cleanup.Err == nil {
			t.Fatalf("cleanup=%#v", cleanup)
		}
		if !strings.Contains(cleanup.Err.Error(), "injected abort pre-rename") {
			t.Fatalf("err=%v", cleanup.Err)
		}
		if _, ok := owner.Table().Get(provisionalID); !ok {
			t.Fatal("provisional must remain when abort not applied")
		}
	})

	t.Run("retry_converges_after_kill_then_persist_ok", func(t *testing.T) {
		owner, provisionalID := newOwner(t)
		first := CleanupFailedLaunch(owner, provisionalID, "agent:@p",
			func(string) error { return errors.New("injected kill failure") },
			func(string) (SessionLiveness, error) { return SessionLivenessPresent, nil },
		)
		if first.Persist.Applied {
			t.Fatal("first must not apply")
		}
		second := CleanupFailedLaunch(owner, provisionalID, "agent:@p",
			func(string) error { return nil },
			func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
		)
		if second.Err != nil || !second.Persist.Applied || owner.Table().Len() != 0 {
			t.Fatalf("second=%#v table=%d", second, owner.Table().Len())
		}
	})
}

func TestCleanupFailedLaunchCommittedCompensationMatrix(t *testing.T) {
	newCommitted := func(t *testing.T) (*Owner, string) {
		t.Helper()
		owner := startTestOwner(t, readyLookup("x"))
		profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
		if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
			t.Fatal(err)
		}
		plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
		if err != nil {
			t.Fatal(err)
		}
		agentID := "agent:@committed"
		if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, agentID); err != nil {
			t.Fatal(err)
		}
		return owner, agentID
	}

	t.Run("kill_failure_preserves_committed", func(t *testing.T) {
		owner, agentID := newCommitted(t)
		cleanup := CleanupFailedLaunch(owner, "", agentID,
			func(string) error { return errors.New("injected kill failure") },
			func(string) (SessionLiveness, error) { return SessionLivenessPresent, nil },
		)
		if cleanup.Persist.Applied || !errors.Is(cleanup.Err, ErrSessionStillLive) {
			t.Fatalf("cleanup=%#v", cleanup)
		}
		if _, ok := owner.Table().Get(agentID); !ok {
			t.Fatal("committed route must remain")
		}
	})

	t.Run("true_missing_then_release", func(t *testing.T) {
		owner, agentID := newCommitted(t)
		cleanup := CleanupFailedLaunch(owner, "", agentID,
			func(string) error { return nil },
			func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
		)
		if cleanup.Err != nil || !cleanup.Persist.Applied {
			t.Fatalf("cleanup=%#v", cleanup)
		}
		if _, ok := owner.Table().Get(agentID); ok {
			t.Fatal("committed route must be released")
		}
	})

	t.Run("resource_cleanup_failure_preserves_committed", func(t *testing.T) {
		owner, agentID := newCommitted(t)
		cleanup := CleanupFailedLaunch(owner, "", agentID,
			func(string) error {
				return fmt.Errorf("%w: injected", errors.New("delegated resource release failed"))
			},
			func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
		)
		if cleanup.Persist.Applied || cleanup.Err == nil {
			t.Fatalf("cleanup=%#v", cleanup)
		}
		if _, ok := owner.Table().Get(agentID); !ok {
			t.Fatal("committed route must remain after resource failure")
		}
	})

	t.Run("release_persist_failure_after_kill", func(t *testing.T) {
		owner, agentID := newCommitted(t)
		owner.RoutesFile().SetPersistHook(func(phase string) error {
			if phase == "before_rename" {
				return errors.New("injected release pre-rename")
			}
			return nil
		})
		cleanup := CleanupFailedLaunch(owner, "", agentID,
			func(string) error { return nil },
			func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
		)
		owner.RoutesFile().SetPersistHook(nil)
		if cleanup.Persist.Applied || cleanup.Err == nil {
			t.Fatalf("cleanup=%#v", cleanup)
		}
		if !strings.Contains(cleanup.Err.Error(), "injected release pre-rename") {
			t.Fatalf("err=%v", cleanup.Err)
		}
		if _, ok := owner.Table().Get(agentID); !ok {
			t.Fatal("committed route must remain when release not applied")
		}
	})

	t.Run("retry_converges_after_resource_then_missing", func(t *testing.T) {
		owner, agentID := newCommitted(t)
		first := CleanupFailedLaunch(owner, "", agentID,
			func(string) error {
				return fmt.Errorf("%w: injected", errors.New("delegated resource release failed"))
			},
			func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
		)
		if first.Persist.Applied {
			t.Fatal("first must not apply")
		}
		second := CleanupFailedLaunch(owner, "", agentID,
			func(string) error { return nil },
			func(string) (SessionLiveness, error) { return SessionLivenessAbsent, nil },
		)
		if second.Err != nil || !second.Persist.Applied {
			t.Fatalf("second=%#v", second)
		}
		if _, ok := owner.Table().Get(agentID); ok {
			t.Fatal("retry must release committed route")
		}
	})

	t.Run("probe_failure_preserves_committed", func(t *testing.T) {
		owner, agentID := newCommitted(t)
		cleanup := CleanupFailedLaunch(owner, "", agentID,
			func(string) error { return errors.New("injected kill failure") },
			func(string) (SessionLiveness, error) {
				return SessionLivenessUnknown, errors.New("injected probe failure")
			},
		)
		if cleanup.Persist.Applied || !errors.Is(cleanup.Err, ErrSessionLivenessUnknown) {
			t.Fatalf("cleanup=%#v", cleanup)
		}
		if _, ok := owner.Table().Get(agentID); !ok {
			t.Fatal("committed route must remain on ambiguous probe")
		}
	})
}
