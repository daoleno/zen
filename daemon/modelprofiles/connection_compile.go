package modelprofiles

import (
	"fmt"
	"strings"
)

// ConnectionScopeAccount marks a durable Provider account connection. Protocol,
// client model, auth mode, and executor are compiled per client at
// launch/activate — never stored as the public connection identity.
const ConnectionScopeAccount = "account"

// Product client ids (App-visible). Map 1:1 to internal executors today but are
// named as clients on the public wire.
const (
	ClientCodex  = "codex"
	ClientClaude = "claude"
)

func isAccountConnection(profile Profile) bool {
	return normalizeID(profile.Scope) == ConnectionScopeAccount
}

func clientFromExecutor(executorID string) string {
	switch normalizeID(executorID) {
	case ExecutorCodex:
		return ClientCodex
	case ExecutorClaude:
		return ClientClaude
	default:
		return normalizeID(executorID)
	}
}

func executorFromClient(client string) string {
	switch normalizeID(client) {
	case ClientCodex:
		return ExecutorCodex
	case ClientClaude:
		return ExecutorClaude
	default:
		return normalizeID(client)
	}
}

// CompileConnectionTarget builds an ephemeral internal Profile for one client
// from a durable account connection or internal executor profile. modelOverride
// is session-only and never written back to the catalog.
func CompileConnectionTarget(conn Profile, clientOrExecutor, modelOverride string) (Profile, error) {
	conn = normalizeProfile(conn)
	client := executorFromClient(clientOrExecutor)
	if client == "" {
		return Profile{}, fmt.Errorf("%w: client is required", ErrInvalid)
	}
	if !SupportsExecutor(client) {
		return Profile{}, fmt.Errorf("%w: %s", ErrUnsupportedExecutor, client)
	}

	if !isAccountConnection(conn) {
		if normalizeID(conn.ExecutorID) != client {
			return Profile{}, fmt.Errorf("%w: current %s next %s", ErrBindingExecutorMismatch, conn.ExecutorID, client)
		}
		out := conn
		if modelOverride = normalizeSpace(modelOverride); modelOverride != "" {
			out.Model = modelOverride
		}
		if err := ValidateProfile(out); err != nil {
			return Profile{}, err
		}
		return out, nil
	}
	if scoped := clientFromExecutor(conn.Client); scoped != clientFromExecutor(client) {
		return Profile{}, fmt.Errorf("%w: connection is scoped to %s", ErrBindingExecutorMismatch, scoped)
	}

	presetID := inferPresetID(conn)
	in := ProviderConnectionInput{
		ID:       conn.ID,
		Name:     conn.Name,
		Client:   clientFromExecutor(client),
		PresetID: presetID,
		ModelID:  firstNonEmpty(normalizeSpace(modelOverride), conn.Model),
		BaseURL:  conn.BaseURL,
		Advanced: normalizeID(presetID) == ProviderPresetCustom || accountLooksAdvanced(conn, presetID),
	}
	// Force account compile into a concrete client Profile (not re-stored).
	target, err := compileProviderConnectionForClient(in, client)
	if err != nil {
		return Profile{}, err
	}
	// Preserve durable connection identity fields.
	target.ID = conn.ID
	target.Name = conn.Name
	target.Scope = "" // ephemeral target is executor-scoped for routing
	target.Client = ""
	target.CredentialEnv = conn.CredentialEnv
	return target, nil
}

func accountLooksAdvanced(conn Profile, presetID string) bool {
	spec, ok := lookupPreset(presetID)
	if !ok {
		return true
	}
	def := strings.TrimRight(spec.DefaultBaseURL, "/")
	got := strings.TrimRight(conn.BaseURL, "/")
	if def == "" {
		return got != ""
	}
	// DeepSeek stores canonical root; Claude compile may use /anthropic — durable is root.
	if normalizeID(presetID) == ProviderPresetDeepSeek {
		return got != "https://api.deepseek.com" && got != ""
	}
	return got != "" && got != def
}

// validateAccountConnection validates durable account-scoped connection records.
func validateAccountConnection(profile Profile) error {
	id := normalizeID(profile.ID)
	if !profileIDRE.MatchString(id) {
		return fmt.Errorf("%w: profile id must match %s", ErrInvalid, profileIDRE.String())
	}
	if normalizeSpace(profile.Name) == "" {
		return fmt.Errorf("%w: profile name is required", ErrInvalid)
	}
	if err := ValidateProviderName(profile.Name); err != nil {
		return fmt.Errorf("%w: name: %v", ErrInvalid, err)
	}
	if normalizeID(profile.ExecutorID) != "" || normalizeID(profile.Protocol) != "" || normalizeSpace(profile.ClientModel) != "" {
		return fmt.Errorf("%w: account connections must not store executor/protocol/client_model", ErrInvalid)
	}
	client := clientFromExecutor(profile.Client)
	if client != ClientCodex && client != ClientClaude {
		return fmt.Errorf("%w: account connection client must be codex or claude", ErrInvalid)
	}
	providerID := normalizeID(profile.ProviderID)
	if providerID == "" || !providerIDRE.MatchString(providerID) {
		return fmt.Errorf("%w: provider_id is required", ErrInvalid)
	}
	if normalizeSpace(profile.ProviderLabel) == "" {
		return fmt.Errorf("%w: provider_label is required", ErrInvalid)
	}
	if err := ValidateUpstreamBaseURL(profile.BaseURL); err != nil {
		return err
	}
	model := normalizeSpace(profile.Model)
	if model != "" {
		if err := ValidateModelID(model); err != nil {
			return fmt.Errorf("%w: model: %v", ErrInvalid, err)
		}
	}
	if env := normalizeSpace(profile.CredentialEnv); env != "" {
		if err := ValidateCredentialEnv(env); err != nil {
			return err
		}
	}
	auth := normalizeID(profile.AuthMode)
	if auth != "" && auth != AuthModeNone {
		return fmt.Errorf("%w: account connections compile auth_mode per client", ErrInvalid)
	}
	return nil
}
