package modelprofiles

import (
	"fmt"
)

// BuiltinEnvelopeVerifier is the Stage 2B daemon contract authority.
// It admits profiles whose ClientModel matches a daemon-known Codex/Claude
// client compatibility contract. Provider ID/label, Gateway URL, and upstream
// model ID may be any ValidateProfile-legal values; UpstreamEnvelope is the
// selected daemon-owned client contract envelope (configured compatibility
// mapping), not a claim that upstream capabilities were discovered.
// Profile TOML provenance/capability/history claims are never trusted.
type BuiltinEnvelopeVerifier struct{}

// VerifyProfileContract admits a profile when the client model resolves through
// the builtin client catalog and executor/protocol pairing is supported.
func (BuiltinEnvelopeVerifier) VerifyProfileContract(profile Profile) (VerifiedProfileContract, error) {
	profile = normalizeProfile(profile)
	if err := ValidateProfile(profile); err != nil {
		return VerifiedProfileContract{}, err
	}
	if !SupportsExecutor(profile.ExecutorID) {
		return VerifiedProfileContract{}, fmt.Errorf("%w: %s", ErrUnsupportedExecutor, profile.ExecutorID)
	}
	route, needs := RouteProtocolFor(profile.Protocol)
	if needs && route == "" {
		return VerifiedProfileContract{}, fmt.Errorf("%w: protocol %q", ErrUnsupportedProtocol, profile.Protocol)
	}

	clientEnv, ok := lookupBuiltinClient(profile.ExecutorID, profile.ClientModel)
	if !ok {
		return VerifiedProfileContract{}, fmt.Errorf("%w: unknown client compatibility contract %q for executor %s", ErrContractUnverified, profile.ClientModel, profile.ExecutorID)
	}

	// openai_native: CLI talks to the native provider; upstream identity must equal client.
	if normalizeID(profile.Protocol) == ProtocolOpenAINative && profile.ClientModel != profile.Model {
		return VerifiedProfileContract{}, fmt.Errorf("%w: openai_native upstream must equal client model", ErrContractUnverified)
	}

	// UpstreamEnvelope mirrors the chosen client contract as a configured
	// compatibility mapping — not probed upstream capability discovery.
	// DeepSeek's trusted Responses envelope (text-only, etc.) is enforced at
	// request sanitize time so same-Session Activate can keep the immutable
	// Codex ClientModelContract while still rejecting unsupported semantics.
	return VerifiedProfileContract{
		Provenance:       ContractProvenanceConfiguredCompatibility,
		ClientModelID:    profile.ClientModel,
		UpstreamModelID:  profile.Model,
		ExecutorID:       profile.ExecutorID,
		Protocol:         profile.Protocol,
		RouteProtocol:    route,
		ProviderID:       profile.ProviderID,
		ClientEnvelope:   clientEnv,
		UpstreamEnvelope: clientEnv,
		HistoryDomain: DeriveOpaqueHistoryDomain(
			profile.Protocol,
			profile.ProviderID,
			profile.BaseURL,
			profile.Model,
			profile.ClientModel,
		),
	}, nil
}
