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
		command, env, note, err := compileCodex(baseCommand, admitted.ClientModelID, profile, loopbackRouteURL, opts.CodexModelCatalogPath, opts.CodexControlSocket)
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
			CodexControlSocket: normalizeSpace(opts.CodexControlSocket),
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

func compileCodex(baseCommand, clientModel string, profile Profile, loopbackRouteURL, modelCatalogPath, controlSocket string) (command string, env map[string]string, wsNote string, err error) {
	switch profile.Protocol {
	case ProtocolOpenAINative:
		if normalizeSpace(controlSocket) != "" {
			return "", nil, "", fmt.Errorf("%w: live control requires the responses route protocol", ErrInvalid)
		}
		return appendArgv(baseCommand, "--model", clientModel), nil, "", nil
	case ProtocolOpenAIResponses:
		tuiCommand := appendArgv(baseCommand, "--model", clientModel)
		tuiCommand = appendConfig(tuiCommand, `model_provider="openai"`)
		tuiCommand = appendConfig(tuiCommand, fmt.Sprintf("openai_base_url=%s", tomlString(loopbackRouteURL)))
		// Deterministic per-connection Codex model catalog (ModelsResponse
		// shape, daemon-owned metadata + discovery availability): the Codex
		// thread model picker and metadata resolve the exact models of this
		// route instead of a provider-agnostic (possibly poisoned) cache.
		if path := normalizeSpace(modelCatalogPath); path != "" {
			tuiCommand = appendConfig(tuiCommand, fmt.Sprintf("model_catalog_json=%s", tomlString(path)))
		}
		// Do not emit no-op --disable flags. Router rejects WebSocket Upgrade with
		// 501; installed Codex falls back to POST /v1/responses (see package report).
		wsNote = "codex_ws_reject_501_then_post_responses_fallback_observed"
		env = map[string]string{}
		if normalizeID(profile.AuthMode) != AuthModeNativePassthrough {
			env[EnvOpenAIAPIKey] = LoopbackAuthPlaceholder
		}
		if socket := normalizeSpace(controlSocket); socket != "" {
			command = compileCodexAppServerLive(baseCommand, clientModel, tuiCommand, socket, loopbackRouteURL, modelCatalogPath)
		} else {
			command = tuiCommand
		}
		return command, env, wsNote, nil
	default:
		return "", nil, "", fmt.Errorf("%w: protocol %q", ErrUnsupportedProtocol, profile.Protocol)
	}
}

// compileCodexAppServerLive wraps the plain TUI launch in Codex's supported
// live-control mode: a headless `codex app-server` owns the thread and exposes
// the native thread/settings/update mutation surface on the control socket,
// while the TUI attaches to it with `--remote`. The app server receives the
// same model/config identity as the TUI (the TUI-only --model flag is mapped
// to the app-server --config model override). Its output is redirected to a
// per-session log so the pane stays TUI-only.
//
// Lifecycle: `set +m` disables shell job control so the app server stays in
// the pane's process group; when the TUI exits or the tmux Session is killed,
// the pty hangup reaches the whole group and the app server dies with the
// pane — it can never be orphaned. The app-server PID is recorded next to the
// socket so the daemon can kill it during Session teardown and sweep stale
// artifacts after a daemon restart. The TUI replaces the pane shell via exec
// so watcher foreground detection keeps seeing `codex`.
func compileCodexAppServerLive(baseCommand, clientModel, tuiCommand, socket, loopbackRouteURL, modelCatalogPath string) string {
	appServer := "codex app-server --listen unix://" + shellQuote(socket)
	appServer = appendConfig(appServer, fmt.Sprintf("model=%s", tomlString(clientModel)))
	appServer = appendConfig(appServer, `model_provider="openai"`)
	appServer = appendConfig(appServer, fmt.Sprintf("openai_base_url=%s", tomlString(loopbackRouteURL)))
	if path := normalizeSpace(modelCatalogPath); path != "" {
		appServer = appendConfig(appServer, fmt.Sprintf("model_catalog_json=%s", tomlString(path)))
	}
	tuiClient := appendArgv(baseCommand, "--remote", "unix://"+socket)
	// The TUI half keeps every model/config identity flag of the plain launch.
	tuiClient += strings.TrimPrefix(tuiCommand, strings.TrimSpace(baseCommand))
	// baseCommand may be an alias path; the app-server half resolves the same
	// installation so both halves agree on the thread owner.
	if alias := normalizeSpace(baseCommand); alias != "" && alias != "codex" {
		appServer = strings.Replace(appServer, "codex app-server", alias+" app-server", 1)
	}
	logPath := CodexControlLogPath(socket)
	pidPath := CodexControlPidPath(socket)
	return "set +m; " + appServer + " > " + shellQuote(logPath) + " 2>&1 & echo $! > " + shellQuote(pidPath) + "; exec " + tuiClient
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
