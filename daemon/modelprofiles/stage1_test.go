package modelprofiles

import (
	"context"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readyLookup(v string) func(string) (string, bool) {
	return func(string) (string, bool) { return v, v != "" }
}

func contractFor(profile Profile) VerifiedProfileContract {
	env := DefaultTestEnvelope()
	route, _ := RouteProtocolFor(profile.Protocol)
	return VerifiedProfileContract{
		Provenance:       ContractProvenanceBuiltinCatalog,
		ClientModelID:    profile.ClientModel,
		UpstreamModelID:  profile.Model,
		ExecutorID:       profile.ExecutorID,
		Protocol:         profile.Protocol,
		RouteProtocol:    route,
		ProviderID:       profile.ProviderID,
		ClientEnvelope:   env,
		UpstreamEnvelope: env,
		HistoryDomain:    DeriveOpaqueHistoryDomain(profile.Protocol, profile.ProviderID, profile.BaseURL, profile.Model, profile.ClientModel),
	}
}

func verifiedAuth(profile Profile) ContractAuth {
	return ContractAuth{Verified: contractFor(profile)}
}

// allowListVerifier admits exact profile client/upstream pairs (test-only; not discovery).
type allowListVerifier map[string]VerifiedProfileContract // key: executor|client|upstream

func (a allowListVerifier) VerifyProfileContract(profile Profile) (VerifiedProfileContract, error) {
	key := normalizeID(profile.ExecutorID) + "|" + profile.ClientModel + "|" + profile.Model
	v, ok := a[key]
	if !ok {
		return VerifiedProfileContract{}, ErrContractUnverified
	}
	return v, nil
}

func registerAllow(v allowListVerifier, profile Profile) allowListVerifier {
	if v == nil {
		v = allowListVerifier{}
	}
	key := normalizeID(profile.ExecutorID) + "|" + profile.ClientModel + "|" + profile.Model
	v[key] = contractFor(profile)
	return v
}

func codexResponsesProfile(id, client, upstream string) Profile {
	return Profile{
		ID: id, Name: id, ExecutorID: ExecutorCodex,
		ProviderID: "acme", ProviderLabel: "Acme",
		Protocol:              ProtocolOpenAIResponses,
		ClientModel:           client,
		ClientModelProvenance: ContractProvenanceBuiltinCatalog,
		Model:                 upstream,
		BaseURL:               "https://gateway.example/v1",
		AuthMode:              AuthModeBearerEnv,
		CredentialEnv:         "ACME_KEY",
	}
}

func claudeMessagesProfile(id, client, upstream string) Profile {
	return Profile{
		ID: id, Name: id, ExecutorID: ExecutorClaude,
		ProviderID: "anthropic", ProviderLabel: "Anthropic",
		Protocol:              ProtocolAnthropicMessages,
		ClientModel:           client,
		ClientModelProvenance: ContractProvenanceVerifiedAlias,
		Model:                 upstream,
		BaseURL:               "https://api.anthropic.com",
		AuthMode:              AuthModeXAPIKeyEnv,
		CredentialEnv:         "ANTHROPIC_API_KEY",
	}
}

func TestStoreCRUDRevisionAndAtomicPerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model-profiles.toml")
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	store.SetLookup(readyLookup("x"))
	profile := codexResponsesProfile("codex-main", "gpt-5", "org/upstream-1")
	if _, err := store.Create(profile, 0); err != nil {
		t.Fatal(err)
	}
	if store.Revision() != 1 {
		t.Fatalf("revision=%d", store.Revision())
	}
	if _, err := store.SetDefault(ExecutorCodex, profile.ID, 1); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("perm=%v", info.Mode())
	}
	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Revision() != 2 {
		t.Fatalf("reloaded revision=%d", reloaded.Revision())
	}
}

func TestCapabilitiesCodexClaudeNoOpenCode(t *testing.T) {
	caps := CapabilitiesFor(ExecutorCodex)
	var native, routed *ProtocolCapability
	for i := range caps.Protocols {
		p := &caps.Protocols[i]
		switch p.Protocol {
		case ProtocolOpenAINative:
			native = p
		case ProtocolOpenAIResponses:
			routed = p
		}
	}
	if native == nil || native.ActiveSwitch != "" || native.Routed {
		t.Fatalf("native=%#v", native)
	}
	if routed == nil || routed.ActiveSwitch != ActiveSwitchRouteBinding || !routed.Routed {
		t.Fatalf("routed=%#v", routed)
	}
	if SupportsExecutor("opencode") {
		t.Fatal("opencode must not be first-slice supported")
	}
}

func TestTOMLProvenanceClaimIsNotAuthorization(t *testing.T) {
	claimed := codexResponsesProfile("hand-edit", "gpt-5", "upstream-1")
	claimed.ClientModelProvenance = ContractProvenanceBuiltinCatalog
	if err := ValidateProfile(claimed); err != nil {
		t.Fatalf("descriptive claim should validate: %v", err)
	}
	if _, err := ContractFromProfile(claimed, ContractAuth{}); !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("ContractFromProfile err=%v", err)
	}
	_, err := Compile("codex", claimed, CompileOptions{
		LoopbackRouteURL: "http://127.0.0.1:4317/r/rt_x/v1",
		CatalogRevision:  1,
		Lookup:           readyLookup("secret"),
	})
	if !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("Compile without auth err=%v", err)
	}
	_, err = NewRouteTable().BindLaunch("s", claimed, 1, ContractAuth{})
	if !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("BindLaunch without auth err=%v", err)
	}
	resolved, err := Compile("codex", claimed, CompileOptions{
		LoopbackRouteURL:        "http://127.0.0.1:4317/r/rt_x/v1",
		CatalogRevision:         1,
		Lookup:                  readyLookup("secret"),
		VerifiedProfileContract: contractFor(claimed),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Env[EnvOpenAIAPIKey] != LoopbackAuthPlaceholder {
		t.Fatalf("missing placeholder env: %v", resolved.Env)
	}
	if strings.Contains(resolved.Command, LoopbackAuthPlaceholder) || strings.Contains(resolved.Command, "--disable") {
		t.Fatalf("command leak/flags: %q", resolved.Command)
	}

	zen := claimed
	zen.ClientModel = "zenith-lab/gpt-style"
	zen.Model = "zenith-lab/gpt-style"
	zen.AuthMode = AuthModeNone
	zen.CredentialEnv = ""
	zen.ClientModelProvenance = ""
	verifier := registerAllow(nil, zen)
	state, err := NewRouteTable().BindLaunch("zen", zen, 1, ContractAuth{Verifier: verifier})
	if err != nil {
		t.Fatal(err)
	}
	if state.Binding.ClientModel != "zenith-lab/gpt-style" {
		t.Fatalf("binding=%#v", state.Binding)
	}

	invented := claimed
	invented.ClientModel = "gpt-5-lookalike"
	invented.Model = "gpt-5-lookalike"
	invented.AuthMode = AuthModeNone
	invented.CredentialEnv = ""
	invented.ClientModelProvenance = ContractProvenanceBuiltinCatalog
	_, err = NewRouteTable().BindLaunch("bad", invented, 1, ContractAuth{Verifier: verifier})
	if !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("invented bypass err=%v", err)
	}
}

func TestVerifierIDsMustExactlyMatchProfile(t *testing.T) {
	p := codexResponsesProfile("p", "gpt-5", "up-1")
	p.AuthMode = AuthModeNone
	p.CredentialEnv = ""
	drift := contractFor(p)
	drift.ClientModelID = "gpt-5-other"
	_, err := AuthorizeProfileContract(p, ContractAuth{Verified: drift})
	if !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("client drift err=%v", err)
	}
	drift = contractFor(p)
	drift.UpstreamModelID = "up-other"
	_, err = AuthorizeProfileContract(p, ContractAuth{Verified: drift})
	if !errors.Is(err, ErrContractUnverified) {
		t.Fatalf("upstream drift err=%v", err)
	}
}

func TestSSRFHostDrivenAllowLoopback(t *testing.T) {
	ctx := context.Background()
	if _, err := resolveSafeHost(ctx, "127.0.0.1"); err != nil {
		t.Fatalf("literal loopback: %v", err)
	}
	if _, err := resolveSafeHost(ctx, "localhost"); err != nil {
		t.Fatalf("localhost: %v", err)
	}
	if _, err := resolveSafeHost(ctx, "10.0.0.1"); !errors.Is(err, ErrUpstreamSSRF) {
		t.Fatalf("private literal err=%v", err)
	}
	if _, err := resolveSafeHost(ctx, "169.254.169.254"); !errors.Is(err, ErrUpstreamSSRF) {
		t.Fatalf("metadata err=%v", err)
	}
	// Remote name resolving to loopback must fail (deterministic fake DNS).
	fake := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	if _, err := resolveSafeHostLookup(ctx, "evil.example", fake); !errors.Is(err, ErrUpstreamSSRF) {
		t.Fatalf("dns->loopback err=%v", err)
	}
	fakePriv := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.1.2.3")}, nil
	}
	if _, err := resolveSafeHostLookup(ctx, "evil.example", fakePriv); !errors.Is(err, ErrUpstreamSSRF) {
		t.Fatalf("dns->private err=%v", err)
	}
	if hostExplicitlyAllowsLoopback("evil.example") {
		t.Fatal("remote name must not allow loopback")
	}
	if isNativePassthroughHost("evil.example") {
		t.Fatal("mutable allowlist must not exist; official check only")
	}
}

func TestCompileCodexBuiltinOpenAIAndClaudeEnv(t *testing.T) {
	profile := codexResponsesProfile("gw", "gpt-5", "org/m1")
	_, err := Compile("codex", profile, CompileOptions{
		LoopbackRouteURL:        "http://127.0.0.1:4317/r/rt_x/v1",
		CatalogRevision:         1,
		Lookup:                  readyLookup(""),
		VerifiedProfileContract: contractFor(profile),
	})
	if !errors.Is(err, ErrCredentialNotReady) {
		t.Fatalf("err=%v", err)
	}
	resolved, err := Compile("codex", profile, CompileOptions{
		LoopbackRouteURL:        "http://127.0.0.1:4317/r/rt_x/v1",
		CatalogRevision:         1,
		Lookup:                  readyLookup("secret"),
		VerifiedProfileContract: contractFor(profile),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Env[EnvOpenAIAPIKey] != LoopbackAuthPlaceholder {
		t.Fatalf("env=%v", resolved.Env)
	}
	if strings.Contains(resolved.Command, "--disable") {
		t.Fatalf("no-op disable flags must be removed: %q", resolved.Command)
	}
	if resolved.CodexWebSocketNote == "" {
		t.Fatal("expected websocket fallback note")
	}

	claude := claudeMessagesProfile("c1", "claude-sonnet-4-6", "claude-sonnet-4-6")
	claude.AuthMode = AuthModeNone
	claude.CredentialEnv = ""
	resolved, err = Compile("claude", claude, CompileOptions{
		LoopbackRouteURL:        "http://127.0.0.1:4317/r/rt_y",
		CatalogRevision:         1,
		VerifiedProfileContract: contractFor(claude),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Env[EnvAnthropicBaseURL] != "http://127.0.0.1:4317/r/rt_y" {
		t.Fatalf("env=%v", resolved.Env)
	}
	if resolved.Env[EnvAnthropicAuthToken] != LoopbackAuthPlaceholder {
		t.Fatalf("missing anthropic placeholder: %v", resolved.Env)
	}
}

func TestRouteTableActivateContractAndHistoryRules(t *testing.T) {
	table := NewRouteTable()
	table.SetLookup(readyLookup("x"))

	native := Profile{
		ID: "n", Name: "N", ExecutorID: ExecutorCodex,
		ProviderID: "openai", ProviderLabel: "OpenAI",
		Protocol: ProtocolOpenAINative, ClientModel: "gpt-5", Model: "gpt-5",
		AuthMode: AuthModeNone,
	}
	_, err := table.BindLaunch("native", native, 1, verifiedAuth(native))
	if err != nil {
		t.Fatal(err)
	}
	routed := codexResponsesProfile("r1", "gpt-5", "m1")
	state, err := table.BindLaunch("s", routed, 1, verifiedAuth(routed))
	if err != nil {
		t.Fatal(err)
	}
	if state.Binding.HistoryState != HistoryStateEmpty {
		t.Fatalf("history state=%q", state.Binding.HistoryState)
	}
	// Empty history: domain may change with same client contract.
	next := codexResponsesProfile("r2", "gpt-5", "m2")
	_, err = table.Activate("s", next, 2, 1, verifiedAuth(next))
	if err != nil {
		t.Fatal(err)
	}
	if err := table.MarkHistoryMayContainOpaque(state.Binding.RouteID); err != nil {
		t.Fatal(err)
	}
	got, _ := table.Get("s")
	if got.Binding.HistoryState != HistoryStateMayContainOpaque {
		t.Fatalf("state=%q", got.Binding.HistoryState)
	}
	other := codexResponsesProfile("r3", "gpt-5", "m3")
	got, err = table.Activate("s", other, 3, 2, verifiedAuth(other))
	if err != nil {
		t.Fatalf("opaque same-protocol portable domain change err=%v", err)
	}
	if got.Binding.HistoryPortability != HistoryPortabilityStripOpaque ||
		got.History[len(got.History)-1].HistoryDegradation != HistoryDegradationStripOpaque {
		t.Fatalf("portable activate missing degradation: %#v", got)
	}
	sameDomain := other
	sameDomain.ID = "r4"
	sameDomain.Model = "m3"
	_, err = table.Activate("s", sameDomain, 4, 3, verifiedAuth(sameDomain))
	if err != nil {
		t.Fatal(err)
	}
}

func TestInternalJSONFailClosed(t *testing.T) {
	for _, target := range []any{RouteBinding{}, RouteActivationEvent{}, SessionRouteState{}, ResolvedLaunch{}} {
		if _, err := json.Marshal(target); !errors.Is(err, ErrInternalNotWire) {
			t.Fatalf("%T marshal err=%v", target, err)
		}
	}
}
