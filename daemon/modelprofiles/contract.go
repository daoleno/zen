package modelprofiles

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// History opacity tracking for Activate domain switching.
const (
	HistoryStateEmpty            = "empty"
	HistoryStateMayContainOpaque = "may_contain_opaque"
)

// Capability class / modality vocabulary (daemon-authorized envelopes only).
const (
	ReasoningClassNone     = "none"
	ReasoningClassStandard = "standard"
	ReasoningClassExtended = "extended"

	ThinkingClassNone     = "none"
	ThinkingClassStandard = "standard"
	ThinkingClassExtended = "extended"

	ToolClassNone     = "none"
	ToolClassFunction = "function"

	ModalityText  = "text"
	ModalityImage = "image"
)

// Fixed non-secret local placeholders injected into CLI env for clean-machine
// routed launches. Never stored in Profile/store/wire/history/logs as secrets.
// Router strips inbound auth and injects real upstream credentials.
const (
	LoopbackAuthPlaceholder = "zen-loopback-placeholder-not-a-secret"
	EnvOpenAIAPIKey         = "OPENAI_API_KEY"
	EnvAnthropicAuthToken   = "ANTHROPIC_AUTH_TOKEN"
)

// CapabilityEnvelope is the daemon-authorized capability surface for a model identity.
type CapabilityEnvelope struct {
	ContextWindowTokens int64
	ReasoningClass      string
	ThinkingClass       string
	ToolClass           string
	Modalities          []string
}

// VerifiedProfileContract is the daemon-authorized Session contract.
// ClientModelID and UpstreamModelID must exactly equal Profile fields — no drift.
type VerifiedProfileContract struct {
	Provenance       string
	ClientModelID    string
	UpstreamModelID  string
	ExecutorID       string
	Protocol         string
	RouteProtocol    string
	ProviderID       string
	ClientEnvelope   CapabilityEnvelope
	UpstreamEnvelope CapabilityEnvelope
	// HistoryDomain is provider/model-specific opaque-history compatibility identity.
	HistoryDomain string
}

// ProfileContractVerifier is a daemon-owned authority that admits a full profile contract.
// TOML capability/provenance/history claims are descriptive input only.
type ProfileContractVerifier interface {
	VerifyProfileContract(profile Profile) (VerifiedProfileContract, error)
}

// ContractAuth authorizes contract establishment for Bind/Compile/Activate/Restore.
type ContractAuth struct {
	Verifier ProfileContractVerifier
	Verified VerifiedProfileContract
}

// AuthorizeProfileContract admits a VerifiedProfileContract. Profile TOML self-claims
// are never sufficient. Verifier-returned model IDs must exactly match Profile.
func AuthorizeProfileContract(profile Profile, auth ContractAuth) (VerifiedProfileContract, error) {
	profile = normalizeProfile(profile)
	if err := ValidateProfile(profile); err != nil {
		return VerifiedProfileContract{}, err
	}
	var admitted VerifiedProfileContract
	if authHasVerified(auth.Verified) {
		admitted = normalizeVerifiedContract(auth.Verified)
	} else if auth.Verifier != nil {
		raw, err := auth.Verifier.VerifyProfileContract(profile)
		if err != nil {
			return VerifiedProfileContract{}, err
		}
		admitted = normalizeVerifiedContract(raw)
	} else {
		return VerifiedProfileContract{}, fmt.Errorf("%w: profile contract requires daemon verifier or VerifiedProfileContract", ErrContractUnverified)
	}
	if err := validateVerifiedProfileContract(admitted); err != nil {
		return VerifiedProfileContract{}, err
	}
	if admitted.ClientModelID != profile.ClientModel {
		return VerifiedProfileContract{}, fmt.Errorf("%w: verified client model %q != profile client_model %q", ErrContractUnverified, admitted.ClientModelID, profile.ClientModel)
	}
	if admitted.UpstreamModelID != profile.Model {
		return VerifiedProfileContract{}, fmt.Errorf("%w: verified upstream model %q != profile model %q", ErrContractUnverified, admitted.UpstreamModelID, profile.Model)
	}
	if normalizeID(admitted.ExecutorID) != normalizeID(profile.ExecutorID) {
		return VerifiedProfileContract{}, fmt.Errorf("%w: verified executor mismatch", ErrContractUnverified)
	}
	if normalizeID(admitted.Protocol) != normalizeID(profile.Protocol) {
		return VerifiedProfileContract{}, fmt.Errorf("%w: verified protocol mismatch", ErrContractUnverified)
	}
	if normalizeID(admitted.ProviderID) != normalizeID(profile.ProviderID) {
		return VerifiedProfileContract{}, fmt.Errorf("%w: verified provider mismatch", ErrContractUnverified)
	}
	wantRoute, needsRoute := RouteProtocolFor(profile.Protocol)
	if needsRoute {
		if admitted.RouteProtocol != wantRoute {
			return VerifiedProfileContract{}, fmt.Errorf("%w: verified route_protocol mismatch", ErrContractUnverified)
		}
	} else if admitted.RouteProtocol != "" {
		return VerifiedProfileContract{}, fmt.Errorf("%w: verified route_protocol must be empty for native", ErrContractUnverified)
	}
	if err := envelopeSupports(admitted.UpstreamEnvelope, admitted.ClientEnvelope); err != nil {
		return VerifiedProfileContract{}, fmt.Errorf("%w: upstream envelope cannot support client: %v", ErrContractUnverified, err)
	}
	return admitted, nil
}

func authHasVerified(v VerifiedProfileContract) bool {
	return normalizeSpace(v.ClientModelID) != "" ||
		normalizeSpace(v.UpstreamModelID) != "" ||
		normalizeID(v.Provenance) != "" ||
		normalizeSpace(v.HistoryDomain) != ""
}

func normalizeVerifiedContract(v VerifiedProfileContract) VerifiedProfileContract {
	v.Provenance = normalizeID(v.Provenance)
	v.ClientModelID = normalizeSpace(v.ClientModelID)
	v.UpstreamModelID = normalizeSpace(v.UpstreamModelID)
	v.ExecutorID = normalizeID(v.ExecutorID)
	v.Protocol = normalizeID(v.Protocol)
	v.RouteProtocol = normalizeID(v.RouteProtocol)
	v.ProviderID = normalizeID(v.ProviderID)
	v.HistoryDomain = normalizeSpace(v.HistoryDomain)
	v.ClientEnvelope = normalizeEnvelope(v.ClientEnvelope)
	v.UpstreamEnvelope = normalizeEnvelope(v.UpstreamEnvelope)
	return v
}

func normalizeEnvelope(e CapabilityEnvelope) CapabilityEnvelope {
	e.ReasoningClass = normalizeID(e.ReasoningClass)
	e.ThinkingClass = normalizeID(e.ThinkingClass)
	e.ToolClass = normalizeID(e.ToolClass)
	mods := make([]string, 0, len(e.Modalities))
	seen := map[string]struct{}{}
	for _, m := range e.Modalities {
		m = normalizeID(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		mods = append(mods, m)
	}
	sort.Strings(mods)
	e.Modalities = mods
	return e
}

func validateVerifiedProfileContract(v VerifiedProfileContract) error {
	if err := RequireContractProvenance(v.Provenance); err != nil {
		return err
	}
	if err := ValidateModelID(v.ClientModelID); err != nil {
		return fmt.Errorf("%w: client model: %v", ErrContractUnverified, err)
	}
	if err := ValidateModelID(v.UpstreamModelID); err != nil {
		return fmt.Errorf("%w: upstream model: %v", ErrContractUnverified, err)
	}
	if normalizeID(v.ExecutorID) == "" || normalizeID(v.Protocol) == "" || normalizeID(v.ProviderID) == "" {
		return fmt.Errorf("%w: verified contract missing executor/protocol/provider", ErrContractUnverified)
	}
	if strings.TrimSpace(v.HistoryDomain) == "" {
		return fmt.Errorf("%w: history_domain required from trusted verifier", ErrContractUnverified)
	}
	if err := validateEnvelope(v.ClientEnvelope); err != nil {
		return fmt.Errorf("%w: client envelope: %v", ErrContractUnverified, err)
	}
	if err := validateEnvelope(v.UpstreamEnvelope); err != nil {
		return fmt.Errorf("%w: upstream envelope: %v", ErrContractUnverified, err)
	}
	return nil
}

func validateEnvelope(e CapabilityEnvelope) error {
	if e.ContextWindowTokens <= 0 {
		return fmt.Errorf("context_window_tokens must be positive")
	}
	if !knownReasoningClass(e.ReasoningClass) {
		return fmt.Errorf("unknown reasoning class %q", e.ReasoningClass)
	}
	if !knownThinkingClass(e.ThinkingClass) {
		return fmt.Errorf("unknown thinking class %q", e.ThinkingClass)
	}
	if !knownToolClass(e.ToolClass) {
		return fmt.Errorf("unknown tool class %q", e.ToolClass)
	}
	if len(e.Modalities) == 0 {
		return fmt.Errorf("modalities required")
	}
	for _, m := range e.Modalities {
		if !knownModality(m) {
			return fmt.Errorf("unknown modality %q", m)
		}
	}
	return nil
}

func knownReasoningClass(v string) bool {
	switch normalizeID(v) {
	case ReasoningClassNone, ReasoningClassStandard, ReasoningClassExtended:
		return true
	default:
		return false
	}
}

func knownThinkingClass(v string) bool {
	switch normalizeID(v) {
	case ThinkingClassNone, ThinkingClassStandard, ThinkingClassExtended:
		return true
	default:
		return false
	}
}

func knownToolClass(v string) bool {
	switch normalizeID(v) {
	case ToolClassNone, ToolClassFunction:
		return true
	default:
		return false
	}
}

func knownModality(v string) bool {
	switch normalizeID(v) {
	case ModalityText, ModalityImage:
		return true
	default:
		return false
	}
}

func classRank(kind, class string) int {
	class = normalizeID(class)
	switch kind {
	case "reasoning", "thinking":
		switch class {
		case ReasoningClassNone:
			return 0
		case ReasoningClassStandard:
			return 1
		case ReasoningClassExtended:
			return 2
		}
	case "tool":
		switch class {
		case ToolClassNone:
			return 0
		case ToolClassFunction:
			return 1
		}
	}
	return -1
}

// envelopeSupports reports whether upstream can satisfy a client envelope.
func envelopeSupports(upstream, client CapabilityEnvelope) error {
	if upstream.ContextWindowTokens < client.ContextWindowTokens {
		return fmt.Errorf("context_window_tokens %d < client %d", upstream.ContextWindowTokens, client.ContextWindowTokens)
	}
	if classRank("reasoning", upstream.ReasoningClass) < classRank("reasoning", client.ReasoningClass) {
		return fmt.Errorf("reasoning class %q cannot support client %q", upstream.ReasoningClass, client.ReasoningClass)
	}
	if classRank("thinking", upstream.ThinkingClass) < classRank("thinking", client.ThinkingClass) {
		return fmt.Errorf("thinking class %q cannot support client %q", upstream.ThinkingClass, client.ThinkingClass)
	}
	if classRank("tool", upstream.ToolClass) < classRank("tool", client.ToolClass) {
		return fmt.Errorf("tool class %q cannot support client %q", upstream.ToolClass, client.ToolClass)
	}
	upMods := map[string]struct{}{}
	for _, m := range upstream.Modalities {
		upMods[normalizeID(m)] = struct{}{}
	}
	for _, m := range client.Modalities {
		if _, ok := upMods[normalizeID(m)]; !ok {
			return fmt.Errorf("upstream missing modality %q", m)
		}
	}
	return nil
}

// DeriveOpaqueHistoryDomain builds a provider/model/contract-specific opaque
// domain. Not a protocol-wide default; used by trusted verifiers / tests.
// clientContractID is the daemon-known ClientModel / compatibility contract id.
func DeriveOpaqueHistoryDomain(protocol, providerID, upstreamBaseURL, upstreamModel, clientContractID string) string {
	origin := upstreamOriginKey(upstreamBaseURL)
	return strings.Join([]string{
		normalizeID(protocol),
		normalizeID(providerID),
		origin,
		normalizeSpace(upstreamModel),
		normalizeSpace(clientContractID),
	}, "|")
}

func upstreamOriginKey(raw string) string {
	raw = normalizeSpace(raw)
	if raw == "" {
		return "none"
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "invalid"
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// envelopesEqual compares capability envelopes (order-insensitive modalities).
func envelopesEqual(a, b CapabilityEnvelope) bool {
	a = normalizeEnvelope(a)
	b = normalizeEnvelope(b)
	if a.ContextWindowTokens != b.ContextWindowTokens ||
		a.ReasoningClass != b.ReasoningClass ||
		a.ThinkingClass != b.ThinkingClass ||
		a.ToolClass != b.ToolClass ||
		len(a.Modalities) != len(b.Modalities) {
		return false
	}
	for i := range a.Modalities {
		if a.Modalities[i] != b.Modalities[i] {
			return false
		}
	}
	return true
}

// DefaultTestEnvelope is a minimal text+function envelope for package tests.
func DefaultTestEnvelope() CapabilityEnvelope {
	return CapabilityEnvelope{
		ContextWindowTokens: 128000,
		ReasoningClass:      ReasoningClassStandard,
		ThinkingClass:       ThinkingClassNone,
		ToolClass:           ToolClassFunction,
		Modalities:          []string{ModalityText},
	}
}
