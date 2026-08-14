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

// VerifyProfileContract admits a profile when the model identity resolves
// through the daemon-owned catalog and executor/protocol pairing is supported.
//
// Unified identity: for managed Codex the selected model slug is BOTH the
// Codex session model (ClientModelID) and the routed upstream model
// (UpstreamModelID) — the daemon never admits a hidden compatibility model.
// Unknown models fail closed.
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

	switch normalizeID(profile.ExecutorID) {
	case ExecutorCodex:
		// The selected model must have daemon-owned metadata. Fail closed for
		// unknown models: never masquerade under a compatibility identity.
		entry, ok := lookupCodexModelMetadata(profile.Model)
		if !ok {
			return VerifiedProfileContract{}, errUnknownCodexModel(profile.Model)
		}
		if normalizeSpace(profile.ClientModel) != normalizeSpace(profile.Model) {
			return VerifiedProfileContract{}, fmt.Errorf("%w: managed Codex client_model %q must equal the selected model %q (single identity; no hidden compatibility model)", ErrContractUnverified, profile.ClientModel, profile.Model)
		}
		envelope := entry.Envelope
		return VerifiedProfileContract{
			Provenance:       entry.Provenance,
			ClientModelID:    profile.Model,
			UpstreamModelID:  profile.Model,
			ExecutorID:       profile.ExecutorID,
			Protocol:         profile.Protocol,
			RouteProtocol:    route,
			ProviderID:       profile.ProviderID,
			ClientEnvelope:   envelope,
			UpstreamEnvelope: envelope,
			HistoryDomain: DeriveOpaqueHistoryDomain(
				profile.Protocol,
				profile.ProviderID,
				profile.BaseURL,
				profile.Model,
				profile.Model,
			),
		}, nil
	case ExecutorClaude:
		clientEnv, ok := lookupBuiltinClient(ExecutorClaude, profile.ClientModel)
		if !ok {
			return VerifiedProfileContract{}, fmt.Errorf("%w: unknown client compatibility contract %q for executor %s", ErrContractUnverified, profile.ClientModel, profile.ExecutorID)
		}
		// openai_native: CLI talks to the native provider; upstream identity must equal client.
		if normalizeID(profile.Protocol) == ProtocolOpenAINative && profile.ClientModel != profile.Model {
			return VerifiedProfileContract{}, fmt.Errorf("%w: openai_native upstream must equal client model", ErrContractUnverified)
		}
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
	default:
		return VerifiedProfileContract{}, fmt.Errorf("%w: %s", ErrUnsupportedExecutor, profile.ExecutorID)
	}
}
