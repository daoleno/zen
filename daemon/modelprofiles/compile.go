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
	if err := requireAuthReady(profile, opts.Credentials, opts.Lookup); err != nil {
		return ResolvedLaunch{}, err
	}
	baseCommand = strings.TrimSpace(baseCommand)
	if baseCommand == "" {
		baseCommand = profile.ExecutorID
	}

	ready := connectionAuthReady(profile, opts.Credentials, opts.Lookup)
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
		command, env, note, err := compileCodex(baseCommand, admitted.ClientModelID, profile, loopbackRouteURL, opts.CodexModelCatalogPath)
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

func compileCodex(baseCommand, clientModel string, profile Profile, loopbackRouteURL, modelCatalogPath string) (command string, env map[string]string, wsNote string, err error) {
	switch profile.Protocol {
	case ProtocolOpenAINative:
		return appendArgv(baseCommand, "--model", clientModel), nil, "", nil
	case ProtocolOpenAIResponses:
		command := appendArgv(baseCommand, "--model", clientModel)
		command = appendConfig(command, `model_provider="openai"`)
		command = appendConfig(command, fmt.Sprintf("openai_base_url=%s", tomlString(loopbackRouteURL)))
		// Deterministic per-connection Codex model catalog (ModelsResponse
		// shape, daemon-owned metadata + discovery availability): the Codex
		// thread model picker and metadata resolve the exact models of this
		// route instead of a provider-agnostic (possibly poisoned) cache.
		if path := normalizeSpace(modelCatalogPath); path != "" {
			command = appendConfig(command, fmt.Sprintf("model_catalog_json=%s", tomlString(path)))
		}
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

// CompileCodexResume builds the managed-handoff resume command: the Codex TUI
// exposes NO external mutation protocol for a running thread, so a
// Zen-initiated model/effort change on a live Session is applied truthfully by
// resuming the SAME thread with the new identity (`codex resume <thread>
// -c model=... -c model_reasoning_effort=...`). History is preserved (the
// thread/rollout continues); the Zen Session/route is not recreated.
func CompileCodexResume(threadID, modelID, effortOverride, loopbackRouteURL, modelCatalogPath string) (string, error) {
	threadID = strings.TrimSpace(threadID)
	modelID = normalizeSpace(modelID)
	effortOverride = normalizeID(effortOverride)
	if threadID == "" {
		return "", fmt.Errorf("%w: codex thread id is required for resume", ErrInvalid)
	}
	if !codexModelKnown(modelID) {
		return "", errUnknownCodexModel(modelID)
	}
	if effortOverride != "" && !codexEffortSupported(modelID, effortOverride) {
		return "", fmt.Errorf("%w: client model %s does not support effort %q", ErrReasoningEffortUnsupported, modelID, effortOverride)
	}
	command := appendArgv("codex", "resume", threadID)
	command = appendConfig(command, fmt.Sprintf("model=%s", tomlString(modelID)))
	if effortOverride != "" {
		command = appendConfig(command, fmt.Sprintf("model_reasoning_effort=%s", tomlString(effortOverride)))
	}
	command = appendConfig(command, `model_provider="openai"`)
	command = appendConfig(command, fmt.Sprintf("openai_base_url=%s", tomlString(loopbackRouteURL)))
	if path := normalizeSpace(modelCatalogPath); path != "" {
		command = appendConfig(command, fmt.Sprintf("model_catalog_json=%s", tomlString(path)))
	}
	return command, nil
}

// CodexResumeCommand builds the managed-handoff resume command for a live
// Session (same route, new model/effort). Fails closed when the Session is not
// a routed Codex Session or the identity is not daemon-admitted.
func (o *Owner) CodexResumeCommand(sessionID, threadID, modelID, effortOverride string) (string, error) {
	if o == nil || !o.started {
		return "", fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	state, ok := o.table.Get(strings.TrimSpace(sessionID))
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrBindingNotFound, sessionID)
	}
	if normalizeID(state.Binding.ExecutorID) != ExecutorCodex || normalizeID(state.Binding.RouteProtocol) != RouteProtocolResponses {
		return "", fmt.Errorf("%w: session is not a routed managed Codex Session", ErrInvalid)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	loopbackURL := ""
	if state.Binding.RouteID != "" {
		var err error
		loopbackURL, err = LoopbackCodexBaseURL(o.addr, state.Binding.RouteID)
		if err != nil {
			return "", err
		}
	}
	if err := o.writeCodexModelCatalogLocked(profileFromBinding(state.Binding)); err != nil {
		return "", err
	}
	return CompileCodexResume(threadID, modelID, effortOverride, loopbackURL, o.CodexModelCatalogPath(state.Binding.ProfileID))
}

// CodexResumeCommandsForRuntime builds target and compensation commands for a
// prepared runtime transaction without mutating the acknowledged route.
func (o *Owner) CodexResumeCommandsForRuntime(plan PreparedThreadRuntime, threadID string) (target string, previous string, err error) {
	if o == nil || !o.started {
		return "", "", fmt.Errorf("%w: owner not started", ErrInvalid)
	}
	state, ok := o.table.Get(strings.TrimSpace(plan.SessionID))
	if !ok || state.Generation != plan.expectedGeneration || state.Binding.RouteID != plan.RouteID {
		return "", "", fmt.Errorf("%w: runtime changed while preparing handoff", ErrBindingConflict)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	loopbackURL, err := LoopbackCodexBaseURL(o.addr, plan.RouteID)
	if err != nil {
		return "", "", err
	}
	if err := o.writeCodexModelCatalogLocked(plan.targetProfile); err != nil {
		return "", "", err
	}
	previousProfile := profileFromBinding(state.Binding)
	if err := o.writeCodexModelCatalogLocked(previousProfile); err != nil {
		return "", "", err
	}
	target, err = CompileCodexResume(threadID, plan.Target.ModelID, plan.Target.Effect, loopbackURL, o.CodexModelCatalogPath(plan.Target.ConnectionID))
	if err != nil {
		return "", "", err
	}
	previous, err = CompileCodexResume(threadID, plan.Previous.ModelID, plan.Previous.Effect, loopbackURL, o.CodexModelCatalogPath(plan.Previous.ConnectionID))
	if err != nil {
		return "", "", err
	}
	return target, previous, nil
}

// RouteIDForSession returns the route id for a Session (empty when unbounded).
func (o *Owner) RouteIDForSession(sessionID string) string {
	if o == nil || o.table == nil {
		return ""
	}
	state, ok := o.table.Get(strings.TrimSpace(sessionID))
	if !ok {
		return ""
	}
	return state.Binding.RouteID
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
