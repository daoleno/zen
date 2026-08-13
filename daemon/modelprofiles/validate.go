package modelprofiles

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

var (
	profileIDRE     = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	providerIDRE    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	credentialEnvRE = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func normalizeID(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeSpace(value string) string {
	return strings.TrimSpace(value)
}

// ValidateProfile checks durable profile fields without reading secret values.
func ValidateProfile(profile Profile) error {
	if isAccountConnection(profile) {
		return validateAccountConnection(profile)
	}
	id := normalizeID(profile.ID)
	if !profileIDRE.MatchString(id) {
		return fmt.Errorf("%w: profile id must match %s", ErrInvalid, profileIDRE.String())
	}
	if normalizeSpace(profile.Name) == "" {
		return fmt.Errorf("%w: profile name is required", ErrInvalid)
	}
	executorID := normalizeID(profile.ExecutorID)
	if !SupportsExecutor(executorID) {
		return fmt.Errorf("%w: %s", ErrUnsupportedExecutor, firstNonEmpty(executorID, profile.ExecutorID))
	}
	protocol := normalizeID(profile.Protocol)
	if !protocolAllowed(executorID, protocol) {
		return fmt.Errorf("%w: protocol %q is not supported for executor %q", ErrUnsupportedProtocol, protocol, executorID)
	}
	providerID := normalizeID(profile.ProviderID)
	if providerID == "" {
		return fmt.Errorf("%w: provider_id is required", ErrInvalid)
	}
	if !providerIDRE.MatchString(providerID) {
		return fmt.Errorf("%w: provider_id must match %s", ErrInvalid, providerIDRE.String())
	}
	if normalizeSpace(profile.ProviderLabel) == "" {
		return fmt.Errorf("%w: provider_label is required", ErrInvalid)
	}
	if err := ValidateModelID(profile.ClientModel); err != nil {
		return fmt.Errorf("%w: client_model: %v", ErrInvalid, err)
	}
	// Provenance on the profile is descriptive durable input only — never authorization.
	if err := validateProvenanceLabel(profile.ClientModelProvenance); err != nil {
		return err
	}
	if err := ValidateModelID(profile.Model); err != nil {
		return fmt.Errorf("%w: model: %v", ErrInvalid, err)
	}

	authMode := normalizeID(profile.AuthMode)
	if authMode == "" {
		authMode = AuthModeNone
	}
	if err := ValidateAuthMode(authMode, profile.CredentialEnv, profile.BaseURL, protocol); err != nil {
		return err
	}

	switch protocol {
	case ProtocolOpenAINative:
		if normalizeSpace(profile.BaseURL) != "" {
			return fmt.Errorf("%w: openai_native profiles must not set base_url", ErrInvalid)
		}
		if authMode != AuthModeNone || normalizeSpace(profile.CredentialEnv) != "" {
			return fmt.Errorf("%w: openai_native profiles must use auth_mode=none without credential_env", ErrInvalid)
		}
	case ProtocolOpenAIResponses, ProtocolAnthropicMessages:
		if err := ValidateUpstreamBaseURL(profile.BaseURL); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unknown protocol %q", ErrInvalid, protocol)
	}

	if d := normalizeID(profile.HistoryDomain); d != "" {
		// Descriptive only — vocabulary is free-form opaque domain text; empty OK.
		// Authorization of history domain comes solely from VerifiedProfileContract.
		if len(d) > MaxModelIDLength {
			return fmt.Errorf("%w: history_domain exceeds %d characters", ErrInvalid, MaxModelIDLength)
		}
	}
	return nil
}

// validateProvenanceLabel accepts empty or known descriptive provenance labels.
// Presence of a known label is not authorization.
func validateProvenanceLabel(provenance string) error {
	switch normalizeID(provenance) {
	case "", ContractProvenanceBuiltinCatalog, ContractProvenanceVerifiedAlias, ContractProvenanceConfiguredCompatibility:
		return nil
	default:
		return fmt.Errorf("%w: unknown client_model_provenance %q", ErrInvalid, provenance)
	}
}

// ValidateAuthMode checks auth_mode + credential_env pairing.
func ValidateAuthMode(authMode, credentialEnv, baseURL, protocol string) error {
	authMode = normalizeID(authMode)
	if authMode == "" {
		authMode = AuthModeNone
	}
	env := normalizeSpace(credentialEnv)
	switch authMode {
	case AuthModeNone:
		if env != "" {
			return fmt.Errorf("%w: credential_env must be empty when auth_mode=none", ErrInvalid)
		}
	case AuthModeBearerEnv, AuthModeXAPIKeyEnv:
		if env == "" {
			return fmt.Errorf("%w: credential_env required for auth_mode=%s", ErrInvalid, authMode)
		}
		if err := ValidateCredentialEnv(env); err != nil {
			return err
		}
	case AuthModeNativePassthrough:
		if env != "" {
			return fmt.Errorf("%w: credential_env must be empty when auth_mode=native_passthrough", ErrInvalid)
		}
		if protocol == ProtocolOpenAINative {
			return fmt.Errorf("%w: native_passthrough is not valid for openai_native", ErrInvalid)
		}
		if err := ValidateUpstreamBaseURL(baseURL); err != nil {
			return err
		}
		host, err := upstreamHostname(baseURL)
		if err != nil {
			return err
		}
		if !isNativePassthroughHost(host) {
			return fmt.Errorf("%w: native_passthrough only allowed for official hosts", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown auth_mode %q", ErrInvalid, authMode)
	}
	return nil
}

func upstreamHostname(raw string) (string, error) {
	parsed, err := url.Parse(normalizeSpace(raw))
	if err != nil {
		return "", fmt.Errorf("%w: base_url: %v", ErrInvalid, err)
	}
	host := parsed.Hostname()
	if host == "" {
		return "", fmt.Errorf("%w: base_url host is required", ErrInvalid)
	}
	return host, nil
}

// ValidateModelID accepts opaque model identifiers including org/model forms.
func ValidateModelID(model string) error {
	if strings.TrimSpace(model) == "" {
		return fmt.Errorf("%w: model is required", ErrInvalid)
	}
	if model != strings.TrimSpace(model) {
		return fmt.Errorf("%w: model must not contain leading or trailing whitespace", ErrInvalid)
	}
	if len(model) > MaxModelIDLength {
		return fmt.Errorf("%w: model exceeds %d characters", ErrInvalid, MaxModelIDLength)
	}
	if strings.ContainsAny(model, " \t\r\n") {
		return fmt.Errorf("%w: model must not contain whitespace", ErrInvalid)
	}
	for _, r := range model {
		if r == 0 || unicode.IsControl(r) {
			return fmt.Errorf("%w: model must not contain control characters", ErrInvalid)
		}
	}
	return nil
}

func protocolAllowed(executorID, protocol string) bool {
	for _, allowed := range SupportedProtocols(executorID) {
		if allowed == protocol {
			return true
		}
	}
	return false
}

// ValidateCredentialEnv accepts only POSIX-like environment variable names.
func ValidateCredentialEnv(name string) error {
	name = normalizeSpace(name)
	if !credentialEnvRE.MatchString(name) {
		return fmt.Errorf("%w: credential_env must match %s", ErrInvalid, credentialEnvRE.String())
	}
	return nil
}

// ValidateUpstreamBaseURL accepts explicitly configured HTTP(S) gateways while
// rejecting malformed URLs and infrastructure metadata targets.
func ValidateUpstreamBaseURL(raw string) error {
	return validateHTTPBaseURL(raw, true)
}

// ValidateLoopbackRouteURL requires an HTTP(S) loopback URL with no userinfo,
// query, or fragment. Secrets must never appear in the route URL.
func ValidateLoopbackRouteURL(raw string) error {
	raw = normalizeSpace(raw)
	if raw == "" {
		return fmt.Errorf("%w: loopback route url is required", ErrRouteRequired)
	}
	return validateHTTPBaseURL(raw, false)
}

func validateHTTPBaseURL(raw string, allowRemote bool) error {
	raw = normalizeSpace(raw)
	if raw == "" {
		return fmt.Errorf("%w: base_url is required", ErrInvalid)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: base_url: %v", ErrInvalid, err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%w: base_url scheme must be https or http", ErrInvalid)
	}
	if parsed.User != nil {
		return fmt.Errorf("%w: base_url must not include userinfo", ErrInvalid)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%w: base_url must not include query or fragment", ErrInvalid)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%w: base_url host is required", ErrInvalid)
	}
	if parsed.Opaque != "" {
		return fmt.Errorf("%w: base_url must be a hierarchical URL", ErrInvalid)
	}
	loopback := isLoopbackHost(parsed.Hostname())
	if !loopback {
		if !allowRemote {
			return fmt.Errorf("%w: zen route url must be loopback-only", ErrInvalid)
		}
		if ip, err := netip.ParseAddr(parsed.Hostname()); err == nil {
			ip = ip.Unmap()
			if !ip.IsValid() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || isMetadataIP(ip) {
				return fmt.Errorf("%w: base_url address is not a model endpoint", ErrUpstreamSSRF)
			}
		}
	}
	return nil
}

func isLoopbackHost(host string) bool {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CredentialReady reports whether a named credential environment variable is
// present and non-empty. The value is never returned.
func CredentialReady(envName string, lookup func(string) (string, bool)) bool {
	envName = normalizeSpace(envName)
	if envName == "" {
		return true
	}
	if lookup == nil {
		lookup = lookupEnv
	}
	value, ok := lookup(envName)
	return ok && strings.TrimSpace(value) != ""
}

// AuthReady reports whether the auth mode can be satisfied from process env.
// Launch/bind/compile must use connectionAuthReady so a stored secret counts.
func AuthReady(authMode, envName string, lookup func(string) (string, bool)) bool {
	return connectionAuthReady(Profile{AuthMode: authMode, CredentialEnv: envName}, nil, lookup)
}

func connectionAuthReady(profile Profile, store CredentialStore, lookup func(string) (string, bool)) bool {
	switch normalizeID(profile.AuthMode) {
	case "", AuthModeNone, AuthModeNativePassthrough:
		return true
	case AuthModeBearerEnv, AuthModeXAPIKeyEnv:
		return providerCredentialReady(profile.ID, profile.CredentialEnv, store, lookup)
	default:
		return false
	}
}

// RequireAuth fails closed when the auth mode needs an env that is missing/empty.
func RequireAuth(authMode, envName string, lookup func(string) (string, bool)) error {
	return requireAuthReady(Profile{AuthMode: authMode, CredentialEnv: envName}, nil, lookup)
}

func requireAuthReady(profile Profile, store CredentialStore, lookup func(string) (string, bool)) error {
	authMode := normalizeID(profile.AuthMode)
	if authMode == "" {
		authMode = AuthModeNone
	}
	switch authMode {
	case AuthModeNone, AuthModeNativePassthrough:
		return nil
	case AuthModeBearerEnv, AuthModeXAPIKeyEnv:
		if err := ValidateCredentialEnv(profile.CredentialEnv); err != nil {
			return err
		}
		if !providerCredentialReady(profile.ID, profile.CredentialEnv, store, lookup) {
			return fmt.Errorf("%w: %s", ErrCredentialNotReady, profile.CredentialEnv)
		}
		return nil
	default:
		return fmt.Errorf("%w: unknown auth_mode %q", ErrInvalid, authMode)
	}
}

func normalizeProfile(profile Profile) Profile {
	profile.ID = normalizeID(profile.ID)
	profile.Name = normalizeSpace(profile.Name)
	profile.Scope = normalizeID(profile.Scope)
	profile.Client = clientFromExecutor(profile.Client)
	profile.ExecutorID = normalizeID(profile.ExecutorID)
	profile.ProviderID = normalizeID(profile.ProviderID)
	profile.ProviderLabel = normalizeSpace(profile.ProviderLabel)
	profile.Protocol = normalizeID(profile.Protocol)
	profile.ClientModel = normalizeSpace(profile.ClientModel)
	profile.ClientModelProvenance = normalizeID(profile.ClientModelProvenance)
	profile.Model = normalizeSpace(profile.Model)
	profile.BaseURL = normalizeSpace(profile.BaseURL)
	profile.AuthMode = normalizeID(profile.AuthMode)
	if profile.AuthMode == "" {
		profile.AuthMode = AuthModeNone
	}
	profile.CredentialEnv = normalizeSpace(profile.CredentialEnv)
	profile.HistoryDomain = normalizeID(profile.HistoryDomain)
	return profile
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func trimHistory(events []RouteActivationEvent) []RouteActivationEvent {
	if len(events) <= MaxRouteHistoryEvents {
		return events
	}
	return append([]RouteActivationEvent{}, events[len(events)-MaxRouteHistoryEvents:]...)
}
