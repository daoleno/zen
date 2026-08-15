package modelprofiles

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// lifecycleTestVerifier admits any ValidateProfile-passing profile with
// DefaultTestEnvelope. Production uses BuiltinEnvelopeVerifier; this seam is
// only for Owner mutate/persist/lifecycle tests that need arbitrary gateways.
type lifecycleTestVerifier struct{}

func (lifecycleTestVerifier) VerifyProfileContract(profile Profile) (VerifiedProfileContract, error) {
	return contractFor(profile), nil
}

func startTestOwner(t *testing.T, lookup func(string) (string, bool)) *Owner {
	t.Helper()
	profiles, routes, listener := stage2bRoot(t)
	if lookup == nil {
		lookup = readyLookup("secret-value-never-on-wire")
	}
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       lookup,
		Verifier:     lifecycleTestVerifier{},
	})
	if err != nil {
		t.Fatalf("StartOwner: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	return owner
}

func TestOwnerPersistFailpointRollsBackActivate(t *testing.T) {
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
	before, _ := owner.Table().Get("s1")
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_rename" {
			return errors.New("injected rename failure")
		}
		return nil
	})
	_, _, persist, err := owner.ActivateSession("s1", "b", 1)
	if err == nil || persist.Applied {
		t.Fatalf("expected not_applied persist failure: persist=%#v err=%v", persist, err)
	}
	owner.RoutesFile().SetPersistHook(nil)
	after, ok := owner.Table().Get("s1")
	if !ok || after.Binding.ProfileID != before.Binding.ProfileID || after.Generation != before.Generation {
		t.Fatalf("memory rolled back? after=%#v before=%#v", after.Binding, before.Binding)
	}
	raw, err := os.ReadFile(owner.RoutesFile().Path())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"profile_id": "b"`) {
		t.Fatalf("disk should not contain activated profile: %s", raw)
	}
}

func assertActivateAppliedOutcome(t *testing.T, owner *Owner, wantProfile string, wantGen int64, persist PersistResult, err error) {
	t.Helper()
	if !persist.Applied {
		t.Fatalf("expected applied: persist=%#v err=%v", persist, err)
	}
	if persist.Durable {
		t.Fatal("expected durable=false")
	}
	if !errors.Is(err, ErrPersistDirSync) {
		t.Fatalf("err=%v want ErrPersistDirSync", err)
	}
	state, ok := owner.Table().Get("s1")
	if !ok || state.Binding.ProfileID != wantProfile || state.Generation != wantGen {
		t.Fatalf("memory after=%#v", state)
	}
	raw, readErr := os.ReadFile(owner.RoutesFile().Path())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(raw), `"profile_id": "`+wantProfile+`"`) {
		t.Fatalf("disk missing applied profile: %s", raw)
	}
	outcome, durable := WirePersistFields(persist)
	if outcome != "applied" || durable == nil || *durable {
		t.Fatalf("wire outcome=%q durable=%v", outcome, durable)
	}
}

func TestOwnerPostRenamePersistKeepsMemoryAndReportsApplied(t *testing.T) {
	phases := []string{"after_rename", "before_dirsync", "after_dirsync"}
	for _, phase := range phases {
		t.Run(phase, func(t *testing.T) {
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
			failPhase := phase
			owner.RoutesFile().SetPersistHook(func(p string) error {
				if p == failPhase {
					return errors.New("injected " + failPhase)
				}
				return nil
			})
			_, _, persist, err := owner.ActivateSession("s1", "b", 1)
			owner.RoutesFile().SetPersistHook(nil)
			assertActivateAppliedOutcome(t, owner, "b", 2, persist, err)

			// Retry/compensation: clear failpoint and activate again succeeds durable.
			c := codexResponsesProfile("c", "gpt-5", "up-c")
			c.BaseURL = a.BaseURL
			c.ProviderID = a.ProviderID
			if _, err := owner.UpsertProfile(c, 2, true); err != nil {
				t.Fatal(err)
			}
			state, _, persist, err := owner.ActivateSession("s1", "c", 2)
			if err != nil || !persist.Applied || !persist.Durable || state.Binding.ProfileID != "c" {
				t.Fatalf("retry state=%#v persist=%#v err=%v", state, persist, err)
			}

			// Restart must load the applied (new) binding, not the pre-activate snapshot.
			profiles := owner.Store().Path()
			routes := owner.RoutesFile().Path()
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
			restored, ok := owner2.Table().Get("s1")
			if !ok || restored.Binding.ProfileID != "c" || restored.Generation != 3 {
				t.Fatalf("restart restored=%#v", restored)
			}
		})
	}
}

func TestOwnerRealDirSyncFailureKeepsAppliedMemory(t *testing.T) {
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
	owner.RoutesFile().SetDirSync(func(string) error {
		return errors.New("injected dirSync failure")
	})
	_, _, persist, err := owner.ActivateSession("s1", "b", 1)
	owner.RoutesFile().SetDirSync(nil)
	assertActivateAppliedOutcome(t, owner, "b", 2, persist, err)
}

func TestOwnerCommitPersistFailKeepsProvisionalBinding(t *testing.T) {
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
		if phase == "before_write" {
			return errors.New("injected write failure")
		}
		return nil
	})
	_, _, _, err = owner.CommitLaunch(plan.ProvisionalID, "agent:@1")
	if err == nil {
		t.Fatal("expected commit persist failure")
	}
	owner.RoutesFile().SetPersistHook(nil)
	if _, ok := owner.Table().Get("agent:@1"); ok {
		t.Fatal("agent binding must not remain after rolled-back commit")
	}
	if _, ok := owner.Table().Get(plan.ProvisionalID); !ok {
		t.Fatal("provisional binding must remain after rolled-back commit")
	}
	if _, err := owner.AbortLaunch(plan.ProvisionalID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ReleaseSession("agent:@1"); err != nil {
		t.Fatal(err)
	}
	if owner.Table().Len() != 0 {
		t.Fatalf("cleanup left bindings: %d", owner.Table().Len())
	}
}

func TestOwnerTransferPersistFailKeepsOldSession(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "old:@1"); err != nil {
		t.Fatal(err)
	}
	routeID, _ := owner.Table().Get("old:@1")
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "before_sync" {
			return errors.New("injected sync failure")
		}
		return nil
	})
	_, err = owner.TransferSession("old:@1", "new:@2")
	if err == nil {
		t.Fatal("expected transfer persist failure")
	}
	owner.RoutesFile().SetPersistHook(nil)
	if _, ok := owner.Table().Get("new:@2"); ok {
		t.Fatal("new session must not own binding after rollback")
	}
	got, ok := owner.Table().Get("old:@1")
	if !ok || got.Binding.RouteID != routeID.Binding.RouteID {
		t.Fatalf("old binding missing after rollback: %#v", got)
	}
}

func TestOwnerConcurrentMutatePersistSerializesGenerations(t *testing.T) {
	owner := startTestOwner(t, readyLookup("x"))
	a := codexResponsesProfile("a", "gpt-5", "up-a")
	b := codexResponsesProfile("b", "gpt-5", "up-b")
	c := codexResponsesProfile("c", "gpt-5", "up-c")
	for _, p := range []*Profile{&a, &b, &c} {
		p.BaseURL = a.BaseURL
		p.ProviderID = a.ProviderID
	}
	rev := int64(0)
	for _, p := range []Profile{a, b, c} {
		if _, err := owner.UpsertProfile(p, rev, true); err != nil {
			t.Fatal(err)
		}
		rev++
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, "a", "codex")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}

	var slowStarted sync.WaitGroup
	slowStarted.Add(1)
	releaseSlow := make(chan struct{})
	var slowRunning atomic.Bool
	owner.RoutesFile().SetPersistHook(func(phase string) error {
		if phase == "after_encode" && slowRunning.CompareAndSwap(false, true) {
			slowStarted.Done()
			<-releaseSlow
		}
		return nil
	})

	var firstErr, secondErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _, _, firstErr = owner.ActivateSession("s1", "b", 1)
	}()
	slowStarted.Wait()
	go func() {
		defer wg.Done()
		// Must not begin until first mutate+persist finishes (Owner.mu).
		_, _, _, secondErr = owner.ActivateSession("s1", "c", 2)
	}()
	time.Sleep(50 * time.Millisecond)
	close(releaseSlow)
	wg.Wait()
	owner.RoutesFile().SetPersistHook(nil)
	if firstErr != nil {
		t.Fatalf("first activate: %v", firstErr)
	}
	if secondErr != nil {
		t.Fatalf("second activate: %v", secondErr)
	}
	state, ok := owner.Table().Get("s1")
	if !ok || state.Generation != 3 || state.Binding.ProfileID != "c" {
		t.Fatalf("final state=%#v", state)
	}
	raw, err := os.ReadFile(owner.RoutesFile().Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"generation": 3`) || !strings.Contains(string(raw), `"profile_id": "c"`) {
		t.Fatalf("disk stale: %s", raw)
	}
}

func TestOwnerResumeLaunchIgnoresCatalogEdits(t *testing.T) {
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
	state, _ := owner.Table().Get("s1")
	edited := profile
	edited.Model = "up-edited"
	edited.BaseURL = "https://evil.example/v1"
	edited.CredentialEnv = "OTHER_KEY"
	if _, err := owner.UpsertProfile(edited, 1, false); err != nil {
		t.Fatal(err)
	}
	cmd, env, found, err := owner.ResumeLaunch("s1", "codex resume thread-1")
	if err != nil || !found {
		t.Fatalf("resume err=%v found=%v", err, found)
	}
	if !strings.Contains(cmd, state.Binding.RouteID) {
		t.Fatalf("resume lost route id: %s", cmd)
	}
	if strings.Contains(cmd, "evil.example") || strings.Contains(cmd, "up-edited") {
		t.Fatalf("resume used edited catalog: %s", cmd)
	}
	if env[EnvOpenAIAPIKey] != LoopbackAuthPlaceholder {
		t.Fatalf("placeholder=%q", env[EnvOpenAIAPIKey])
	}
	raw, _ := json.Marshal(env)
	if strings.Contains(string(raw), "secret-value") {
		t.Fatal("secret in resume env")
	}
}

func TestOwnerListenerOccupiedNoRoutesFallsBack(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	profiles, routes, listener := stage2bRoot(t)
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
		PreferAddr:   occupied.Addr().String(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	if owner.ListenAddr() != "" {
		t.Fatal("cold start with PreferAddr must stay inert until managed launch")
	}
	profile := codexResponsesProfile("codex-main", "gpt-5", "up-1")
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex"); err != nil {
		t.Fatal(err)
	}
	if owner.ListenAddr() == "" || owner.ListenAddr() == occupied.Addr().String() {
		t.Fatalf("expected ephemeral fallback, got %q", owner.ListenAddr())
	}
}

func TestOwnerListenerOccupiedWithRoutesFailClosed(t *testing.T) {
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
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
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
		t.Fatalf("err=%v", err)
	}
}

func TestOwnerMalformedListenerNoRoutesOK(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	stale := []byte(`{not-json`)
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
		t.Fatal("malformed listener with no routes must stay inert")
	}
	// Zero-route converge removes unusable listener metadata rather than retaining it.
	if _, err := os.Stat(listener); !os.IsNotExist(err) {
		t.Fatalf("malformed listener must be removed on inert converge: err=%v", err)
	}
}

func TestOwnerMalformedListenerWithRoutesFailClosed(t *testing.T) {
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
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	_ = owner.Close()
	if err := os.WriteFile(listener, []byte(`{bad`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		Verifier:     lifecycleTestVerifier{},
	})
	if err == nil {
		t.Fatal("expected malformed listener fail closed with live routes")
	}
}

func TestBuiltinVerifierKnownAndUnknown(t *testing.T) {
	v := BuiltinEnvelopeVerifier{}
	okProfile := Profile{
		ID: "codex-main", Name: "Codex", ExecutorID: ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5", Model: "gpt-5",
		ClientModelProvenance: ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://api.openai.com/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "OPENAI_API_KEY",
	}
	got, err := v.VerifyProfileContract(okProfile)
	if err != nil {
		t.Fatal(err)
	}
	if got.Provenance != ContractProvenanceCodexCatalog {
		t.Fatalf("provenance=%q", got.Provenance)
	}
	claude := Profile{
		ID: "claude-main", Name: "Claude", ExecutorID: ExecutorClaude,
		ProviderID: "anthropic", ProviderLabel: "Anthropic",
		Protocol: ProtocolAnthropicMessages, ClientModel: "claude-sonnet-4-6", Model: "claude-sonnet-4-6",
		ClientModelProvenance: ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://api.anthropic.com",
		AuthMode:              AuthModeXAPIKeyEnv,
		CredentialEnv:         "ANTHROPIC_API_KEY",
	}
	if _, err := v.VerifyProfileContract(claude); err != nil {
		t.Fatal(err)
	}
	unknown := okProfile
	unknown.ClientModel = "gpt-99-invented"
	unknown.Model = "gpt-99-invented"
	if _, err := v.VerifyProfileContract(unknown); !errors.Is(err, ErrModelUnsupported) {
		t.Fatalf("unknown model must fail closed, err=%v", err)
	}
}

func TestBuiltinVerifierOpenRouterCustomGatewayAndAlias(t *testing.T) {
	v := BuiltinEnvelopeVerifier{}
	openrouter := Profile{
		ID: "or-codex", Name: "OpenRouter", ExecutorID: ExecutorCodex,
		ProviderID: "openrouter", ProviderLabel: "OpenRouter",
		Protocol: ProtocolOpenAIResponses, ClientModel: "openai/gpt-5", Model: "openai/gpt-5",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		BaseURL:               "https://openrouter.ai/api/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "OPENROUTER_API_KEY",
	}
	got, err := v.VerifyProfileContract(openrouter)
	if err != nil {
		t.Fatal(err)
	}
	if got.ProviderID != "openrouter" || got.UpstreamModelID != "openai/gpt-5" {
		t.Fatalf("got=%#v", got)
	}
	if got.Provenance != ContractProvenanceConfiguredCompatibility {
		t.Fatalf("provenance=%q", got.Provenance)
	}
	if got.UpstreamEnvelope.ContextWindowTokens != got.ClientEnvelope.ContextWindowTokens {
		t.Fatal("upstream envelope must mirror selected client contract")
	}
	// Custom gateway serving a daemon-known model: the exact slug is the single
	// identity (client_model == model); unknown gateway-only slugs fail closed.
	customGW := openrouter
	customGW.ID = "custom-gw"
	customGW.ProviderID = "acme-gateway"
	customGW.ProviderLabel = "Acme Gateway"
	customGW.BaseURL = "https://gateway.acme.example/v1"
	customGW.ClientModel = "gpt-5.6-sol"
	customGW.Model = "gpt-5.6-sol"
	customGW.CredentialEnv = "ACME_KEY"
	got2, err := v.VerifyProfileContract(customGW)
	if err != nil {
		t.Fatal(err)
	}
	wantDomain := DeriveOpaqueHistoryDomain(
		customGW.Protocol, customGW.ProviderID, customGW.BaseURL, customGW.Model, customGW.ClientModel,
	)
	if got2.HistoryDomain != wantDomain {
		t.Fatalf("history domain=%q want=%q", got2.HistoryDomain, wantDomain)
	}
	loopback := customGW
	loopback.BaseURL = "http://127.0.0.1:8080/v1"
	if _, err := v.VerifyProfileContract(loopback); err != nil {
		t.Fatalf("explicit loopback gateway: %v", err)
	}
	mismatch := openrouter
	mismatch.ExecutorID = ExecutorClaude
	if _, err := v.VerifyProfileContract(mismatch); !errors.Is(err, ErrContractUnverified) && !errors.Is(err, ErrUnsupportedProtocol) {
		// Claude executor + openai_responses protocol or unknown client for claude.
		if err == nil {
			t.Fatal("expected executor/protocol mismatch fail closed")
		}
	}
	// Unified identity: openai_native with a mismatched (but known) model is
	// rejected; the exact slug is the only admitted shape.
	native := Profile{
		ID: "native", Name: "Native", ExecutorID: ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: ProtocolOpenAINative, ClientModel: "gpt-5", Model: "gpt-5.1",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		AuthMode:              AuthModeNone,
	}
	if _, err := v.VerifyProfileContract(native); !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("openai_native identity mismatch err=%v", err)
	}
}

func TestBuiltinSameContractHotSwitchAndHistoryDomains(t *testing.T) {
	v := BuiltinEnvelopeVerifier{}
	a := Profile{
		ID: "a", Name: "A", ExecutorID: ExecutorCodex,
		ProviderID: "openrouter", ProviderLabel: "OpenRouter",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5.6-sol", Model: "gpt-5.6-sol",
		ClientModelProvenance: ContractProvenanceConfiguredCompatibility,
		BaseURL:               "https://openrouter.ai/api/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "OPENROUTER_API_KEY",
	}
	b := a
	b.ID = "b"
	b.Name = "B"
	b.ClientModel = "gpt-5.5"
	b.Model = "gpt-5.5"
	ca, err := v.VerifyProfileContract(a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := v.VerifyProfileContract(b)
	if err != nil {
		t.Fatal(err)
	}
	// Unified identity: each model is its own contract identity.
	if ca.ClientModelID != "gpt-5.6-sol" || cb.ClientModelID != "gpt-5.5" {
		t.Fatalf("client identities=%q/%q", ca.ClientModelID, cb.ClientModelID)
	}
	if ca.HistoryDomain == cb.HistoryDomain {
		t.Fatal("different models must yield different history domains")
	}

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
	// Empty history: same client contract, different upstream — hot switch allowed.
	state, _, persist, err := owner.ActivateSession("s1", "b", 1)
	if err != nil || !persist.Applied || state.Binding.ProfileID != "b" {
		t.Fatalf("hot switch state=%#v persist=%#v err=%v", state, persist, err)
	}
	routeID := state.Binding.RouteID
	if err := owner.Table().MarkHistoryMayContainOpaque(routeID); err != nil {
		t.Fatal(err)
	}
	state, _, persist, err = owner.ActivateSession("s1", "a", 2)
	if err != nil || !persist.Applied {
		t.Fatalf("opaque same-protocol portable activate: persist=%#v err=%v", persist, err)
	}
	if state.Binding.HistoryPortability != HistoryPortabilityStripOpaque {
		t.Fatalf("history_portability=%q", state.Binding.HistoryPortability)
	}
	if state.Binding.HistoryDomain != ca.HistoryDomain {
		t.Fatalf("domain after portable activate=%q want=%q", state.Binding.HistoryDomain, ca.HistoryDomain)
	}
	if len(state.History) == 0 || state.History[len(state.History)-1].HistoryDegradation != HistoryDegradationStripOpaque {
		t.Fatalf("activation must record degradation: %#v", state.History)
	}
	_ = routeID
}

func TestBuiltinRestoreReverify(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
		// production verifier
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := Profile{
		ID: "codex-main", Name: "Codex", ExecutorID: ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5.1", Model: "gpt-5.1",
		ClientModelProvenance: ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://api.openai.com/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "OPENAI_API_KEY",
	}
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
	addr := owner.ListenAddr()
	_ = owner.Close()

	owner2, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner2.Close() })
	if owner2.ListenAddr() != addr {
		t.Fatalf("port changed %s -> %s", addr, owner2.ListenAddr())
	}
	state, ok := owner2.Table().Get("s1")
	if !ok || state.Binding.ClientModel != "gpt-5.1" || state.Binding.UpstreamModel != "gpt-5.1" {
		t.Fatalf("restored=%#v", state)
	}
}

// TestStartOwnerKeepsStaleContractRoutes reproduces the daemon-upgrade hazard:
// a durable route written under an older client contract no longer verifies
// under the current daemon authority. StartOwner must keep the stale route
// live (restore it, report the drift, keep the loopback listener on the same
// port) instead of refusing to start or dropping the Session — the running
// CLI's own request identity stays authoritative and converges via adoption.
func TestStartOwnerKeepsStaleContractRoutes(t *testing.T) {
	profiles, routes, listener := stage2bRoot(t)
	owner, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
	})
	if err != nil {
		t.Fatal(err)
	}
	profile := Profile{
		ID: "codex-main", Name: "Codex", ExecutorID: ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: ProtocolOpenAIResponses, ClientModel: "gpt-5.1", Model: "gpt-5.1",
		ClientModelProvenance: ContractProvenanceBuiltinCatalog,
		BaseURL:               "https://api.openai.com/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "OPENAI_API_KEY",
	}
	if _, err := owner.UpsertProfile(profile, 0, true); err != nil {
		t.Fatal(err)
	}
	plan, err := owner.PrepareLaunch(ExecutorCodex, profile.ID, "codex")
	if err != nil {
		t.Fatal(err)
	}
	addr := owner.ListenAddr()
	if addr == "" {
		t.Fatal("first owner must be listening")
	}
	if _, _, _, err := owner.CommitLaunch(plan.ProvisionalID, "s1"); err != nil {
		t.Fatal(err)
	}
	_ = owner.Close()

	// Rewrite the durable route as an older daemon would have: client model
	// gpt-5.1 with an upstream identity that is not in the current catalog.
	raw, err := os.ReadFile(routes)
	if err != nil {
		t.Fatal(err)
	}
	states, err := DecodeDurableSnapshot(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("want 1 persisted route, have %d", len(states))
	}
	states[0].Binding.UpstreamModel = "codex-auto-review"
	states[0].Launched.UpstreamModel = "codex-auto-review"
	stale, err := EncodeDurableSnapshot(states)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(routes, stale, 0o600); err != nil {
		t.Fatal(err)
	}

	owner2, err := StartOwner(OwnerConfig{
		ProfilesPath: profiles,
		RoutesPath:   routes,
		ListenerPath: listener,
		Lookup:       readyLookup("x"),
	})
	if err != nil {
		t.Fatalf("stale contract must not brick startup: %v", err)
	}
	defer func() { _ = owner2.Close() }()

	notices := owner2.RestoreContractNotices()
	if len(notices) != 1 || notices[0].SessionID != "s1" || !strings.Contains(notices[0].Reason, "codex-auto-review") {
		t.Fatalf("drift notices=%#v", notices)
	}
	state, ok := owner2.Table().Get("s1")
	if !ok || state.Binding.UpstreamModel != "codex-auto-review" {
		t.Fatalf("stale route must stay live after restore: %#v", state.Binding)
	}
	// A live route keeps the loopback listener on the persisted port so the
	// surviving CLI process keeps working (or errors naturally) after restart.
	if owner2.ListenAddr() != addr {
		t.Fatalf("listener port changed %s -> %s", addr, owner2.ListenAddr())
	}
	// Nothing was rewritten: the stale route is still durable as-is.
	kept, err := os.ReadFile(routes)
	if err != nil {
		t.Fatal(err)
	}
	states2, err := DecodeDurableSnapshot(kept)
	if err != nil {
		t.Fatal(err)
	}
	if len(states2) != 1 || states2[0].Binding.UpstreamModel != "codex-auto-review" {
		t.Fatalf("route file must be untouched: %#v", states2)
	}
}
