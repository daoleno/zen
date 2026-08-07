package modelprofiles

import (
	"fmt"
	"strings"
)

// Compile builds a secret-free launch plan that targets opts.LoopbackRouteURL when
// the profile uses a Zen route. Upstream base URL/credential stay on Draft for
// RouteTable — never in command/env values beyond the loopback URL and model id.
//
// Client/upstream contract requires opts.VerifiedProfileContract or opts.Verifier.
// Profile TOML provenance/capability/history claims are never sufficient.
func Compile(baseCommand string, profile Profile, opts CompileOptions) (ResolvedLaunch, error) {
	admitted, err := AuthorizeProfileContract(profile, ContractAuth{
		Verifier: opts.Verifier,
		Verified: opts.VerifiedProfileContract,
	})
	if err != nil {
		return ResolvedLaunch{}, err
	}
	profile = normalizeProfile(profile)
	if err := RequireAuth(profile.AuthMode, profile.CredentialEnv, opts.Lookup); err != nil {
		return ResolvedLaunch{}, err
	}
	baseCommand = strings.TrimSpace(baseCommand)
	if baseCommand == "" {
		baseCommand = profile.ExecutorID
	}

	ready := AuthReady(profile.AuthMode, profile.CredentialEnv, opts.Lookup)
	draft, err := BindingDraftFromProfile(profile, opts.CatalogRevision, ActivationLaunch, ready, admitted)
	if err != nil {
		return ResolvedLaunch{}, err
	}

	routeProtocol, needsRoute := RouteProtocolFor(profile.Protocol)
	loopbackRouteURL := normalizeSpace(opts.LoopbackRouteURL)
	if needsRoute {
		if err := ValidateLoopbackRouteURL(loopbackRouteURL); err != nil {
			return ResolvedLaunch{}, err
		}
		if strings.Contains(loopbackRouteURL, "@") {
			return ResolvedLaunch{}, fmt.Errorf("%w: route url must not contain userinfo marker", ErrInvalid)
		}
	} else if loopbackRouteURL != "" {
		return ResolvedLaunch{}, fmt.Errorf("%w: loopback route url is not used for protocol %s", ErrInvalid, profile.Protocol)
	}

	switch profile.ExecutorID {
	case ExecutorCodex:
		command, env, note, err := compileCodex(baseCommand, admitted.ClientModelID, profile, loopbackRouteURL)
		if err != nil {
			return ResolvedLaunch{}, err
		}
		if err := assertNoUpstreamLeak(command, env, profile); err != nil {
			return ResolvedLaunch{}, err
		}
		if err := assertNoPlaceholderLeak(command, profile); err != nil {
			return ResolvedLaunch{}, err
		}
		return ResolvedLaunch{
			Command:            command,
			Env:                env,
			NeedsRoute:         needsRoute,
			RouteProtocol:      routeProtocol,
			Draft:              draft,
			Wire:               draft.ToWire(),
			CodexWebSocketNote: note,
		}, nil
	case ExecutorClaude:
		command, env, err := compileClaude(baseCommand, admitted.ClientModelID, profile, loopbackRouteURL)
		if err != nil {
			return ResolvedLaunch{}, err
		}
		if err := assertNoUpstreamLeak(command, env, profile); err != nil {
			return ResolvedLaunch{}, err
		}
		if err := assertNoPlaceholderLeak(command, profile); err != nil {
			return ResolvedLaunch{}, err
		}
		return ResolvedLaunch{
			Command:       command,
			Env:           env,
			NeedsRoute:    true,
			RouteProtocol: routeProtocol,
			Draft:         draft,
			Wire:          draft.ToWire(),
		}, nil
	default:
		return ResolvedLaunch{}, fmt.Errorf("%w: %s", ErrUnsupportedExecutor, profile.ExecutorID)
	}
}

func compileCodex(baseCommand, clientModel string, profile Profile, loopbackRouteURL string) (command string, env map[string]string, wsNote string, err error) {
	switch profile.Protocol {
	case ProtocolOpenAINative:
		return appendArgv(baseCommand, "--model", clientModel), nil, "", nil
	case ProtocolOpenAIResponses:
		command := appendArgv(baseCommand, "--model", clientModel)
		command = appendConfig(command, `model_provider="openai"`)
		command = appendConfig(command, fmt.Sprintf("openai_base_url=%s", tomlString(loopbackRouteURL)))
		// Do not emit no-op --disable flags. Router rejects WebSocket Upgrade with
		// 501; installed Codex falls back to POST /v1/responses (see package report).
		wsNote = "codex_ws_reject_501_then_post_responses_fallback_observed"
		env = map[string]string{}
		if normalizeID(profile.AuthMode) != AuthModeNativePassthrough {
			env[EnvOpenAIAPIKey] = LoopbackAuthPlaceholder
		}
		return command, env, wsNote, nil
	default:
		return "", nil, "", fmt.Errorf("%w: protocol %q", ErrUnsupportedProtocol, profile.Protocol)
	}
}

func compileClaude(baseCommand, clientModel string, profile Profile, loopbackRouteURL string) (command string, env map[string]string, err error) {
	if profile.Protocol != ProtocolAnthropicMessages {
		return "", nil, fmt.Errorf("%w: protocol %q", ErrUnsupportedProtocol, profile.Protocol)
	}
	if err := ValidateLoopbackRouteURL(loopbackRouteURL); err != nil {
		return "", nil, err
	}
	if strings.HasSuffix(strings.TrimSuffix(loopbackRouteURL, "/"), "/v1") {
		return "", nil, fmt.Errorf("%w: claude loopback url must be route root without /v1", ErrInvalid)
	}
	command = appendArgv(baseCommand, "--model", clientModel)
	env = map[string]string{EnvAnthropicBaseURL: loopbackRouteURL}
	if normalizeID(profile.AuthMode) != AuthModeNativePassthrough {
		env[EnvAnthropicAuthToken] = LoopbackAuthPlaceholder
	}
	return command, env, nil
}

func assertNoUpstreamLeak(command string, env map[string]string, profile Profile) error {
	upstream := normalizeSpace(profile.BaseURL)
	if upstream != "" {
		if strings.Contains(command, upstream) {
			return fmt.Errorf("%w: upstream base_url leaked into launch command", ErrInvalid)
		}
		for key, value := range env {
			if key == EnvOpenAIAPIKey || key == EnvAnthropicAuthToken {
				continue
			}
			if strings.Contains(value, upstream) && key != EnvAnthropicBaseURL {
				return fmt.Errorf("%w: upstream base_url leaked into env", ErrInvalid)
			}
		}
	}
	if envName := normalizeSpace(profile.CredentialEnv); envName != "" {
		if strings.Contains(command, envName+"=") || strings.Contains(command, "$"+envName) {
			return fmt.Errorf("%w: credential env leaked into launch command", ErrInvalid)
		}
	}
	return nil
}

func assertNoPlaceholderLeak(command string, profile Profile) error {
	if strings.Contains(command, LoopbackAuthPlaceholder) {
		return fmt.Errorf("%w: loopback auth placeholder leaked into command", ErrInvalid)
	}
	if normalizeSpace(profile.CredentialEnv) != "" && strings.Contains(command, LoopbackAuthPlaceholder) {
		return fmt.Errorf("%w: placeholder leak", ErrInvalid)
	}
	return nil
}

func appendArgv(command string, flag, value string) string {
	command = strings.TrimSpace(command)
	return strings.TrimSpace(command + " " + flag + " " + shellQuote(value))
}

func appendConfig(command, assignment string) string {
	command = strings.TrimSpace(command)
	return strings.TrimSpace(command + " --config " + shellQuote(assignment))
}

func tomlString(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
	return `"` + escaped + `"`
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n\"'\\$`|&;()<>!*?[]{}~#") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
