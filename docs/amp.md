# Amp: External CLI And BYOK Boundaries

Contract checked on 2026-09-13 against Amp's official documentation and CLI
`0.0.1789257698-g25321c`. Amp is an external coding agent, not a Zen model
provider. This document does not install an adapter, register an executor, change
the delegated executor or model defaults, or transfer credentials.

## Free Agent Is A Billing Policy

The [Free Agent announcement](https://ampcode.com/news/free-agent), published
2026-09-13, says that local compute plus your own model subscription or API key
can use Amp without a monthly Amp plan. Non-Enterprise BYOK has no Amp token fees
or limits. This is not a free model endpoint, a `free` CLI mode, unlimited
upstream tokens, or an OpenCode Go entitlement.

The same announcement limits early access to additional routers, including
OpenCode Go and custom URLs, to Megawatt and Gigawatt members with **More AI
Routers & Subscriptions** enabled. It says wider rollout is forthcoming; do not
infer account eligibility from an installed CLI accepting a router name.

Provider quotas and charges still apply. Amp-served inference and non-model
tools can consume Amp credits; orbs are separately billed compute. The older
[pricing documentation](https://ampcode.com/docs/pricing) predates this
announcement. Use the announcement for the new BYOK policy and
[current pricing](https://ampcode.com/pricing) for plan details, not historical
Amp Free daily-credit or ad-supported allowances.

## Official Integration Surfaces

| Surface | Supported contract | Boundary |
| --- | --- | --- |
| [CLI](https://ampcode.com/docs/cli) | Interactive `amp`; local `-x` / `--execute`; `--visibility private` | Amp login stays on the daemon host; local tools do not imply local model inference |
| [Execute mode](https://ampcode.com/docs/cli/execute-mode) | `amp -x` returns the final answer; `-ox` starts remote orb execution | Do not substitute an orb for a local Worker or start billable remote compute implicitly |
| [Streaming JSON](https://ampcode.com/docs/cli/streaming-json) | `--execute --stream-json`, optional thinking and JSONL input; assistant, tool-use, tool-result, result/error records | Message/event streaming is not proof of token-delta streaming or a Zen structured-chat adapter |
| [SDK](https://ampcode.com/docs/sdk/typescript) | `@ampcode/sdk` `execute({prompt, options, signal})`; `AsyncIterable<StreamMessage>`; `AbortSignal` cancellation | CLI-backed options include `cwd`, `mode`, `settingsFile`, `enabledTools`, `env`; no documented local BYOK credential resolver |
| [Configuration](https://ampcode.com/docs/cli/settings) | User and workspace `.amp/settings.json` / `.jsonc`; custom user `--settings-file`; `amp.permissions`, tool and MCP controls | Not an arbitrary provider/model/base-URL/key schema; `AMP_URL` is the Amp service URL and `amp.proxy` is a transport proxy |
| [Model routing and Dial](https://ampcode.com/docs/the-dial#use-your-own-key-or-subscription) | Account/workspace provider connections choose billing; mode tuning selects models and effort | These are different settings. Pinning main agent, Oracle and subagents does not reroute every supporting system model |
| [Plugin API](https://ampcode.com/docs/plugin-api) | `createAgent`, `registerAgentMode`, `PluginAgentModel` in `provider/model` form; account routers serve external models | A custom mode is not a provider transport or credential callback. Do not install a plugin to pretend it is one |
| [Projects](https://ampcode.com/docs/projects) | CLI project commands map repositories and configure orb settings | Project configuration does not attach a local Zen credential reference to model routing |
| [Secrets](https://ampcode.com/docs/orbs/handling-secrets) | `amp secrets` manages environment variables for orbs and Apps; secret values are not returned | Orb secrets and OIDC service access are not local model-provider auth references |
| [Cross-client access](https://ampcode.com/docs/cli/remote-control) | Amp's own web/mobile clients can continue CLI threads; workspace/passkey controls apply | It is not a documented Zen remote-control protocol. `--no-remote-control-terminal` disables terminal access, not thread sync or all remote agent interaction |

For unattended Amp authentication, the execute-mode docs specify an Amp access
token, not an upstream provider key. Do not extract the short-lived login session
token into `AMP_API_KEY`; the CLI rejects that use. Let Amp manage its native
login, and never send either token to Zen mobile clients.

## OpenCode Go Contract

The official installed CLI exposes these discoverable interfaces:

```sh
amp config model-providers add-router --help
amp config model-providers setup-guide opencode-go
amp config model-providers list --json
amp config model-providers check-access --help
amp plugins show-agent-options --json
```

`add-router` explicitly accepts `opencode-go` and `custom-url`. It accepts
`--api-key-file <path>` (or `-` for stdin), personal/workspace scope, activation
controls, and `--model-mapping`. Mappings support exact `provider/model` IDs,
patterns, exclusions, and `exact/model -> provider-model-id` rewrites. Omitting
the mapping selects all supported models; that is too broad for a single-model
integration. Activation may deactivate another active key of the same provider.

For `custom-url`, documented fields are `--base-url` using HTTPS,
`--api-format` (`chat-completions`, `responses`, `anthropic-messages`), headers and
query parameters. Dedicated gateway options are typed. Do not invent settings,
environment variables, hidden HTTP endpoints or request fields.

The intended narrow route is:

| Field | Value |
| --- | --- |
| Billing provider / Amp router | OpenCode Go / `opencode-go`, not a DeepSeek direct account |
| Upstream base | `https://opencode.ai/zen/go/v1` |
| Protocol and resource | OpenAI-compatible chat completions, `/chat/completions` |
| Amp model identity | `deepseek/deepseek-v4.1-flash` |
| Upstream model identity | `deepseek-v4.1-flash` |
| Exact explicit mapping | `deepseek/deepseek-v4.1-flash -> deepseek-v4.1-flash` |
| Canonical host auth source | OpenCode data directory `auth.json`, `opencode-go` entry; on standard Linux installs `~/.local/share/opencode/auth.json` |
| Secret handling | Reference only in integration metadata; never a key value, prefix, suffix, command argument or client payload |

[OpenCode Go documentation](https://opencode.ai/docs/go/) publishes this model
and endpoint. Clients must identify themselves as coding agents and preserve a
stable session ID per conversation (`x-opencode-session` or a recognized native
header). A deployment-wide static UUID does not establish conversation identity.
Neither Amp's model catalog nor a text completion proves that Amp preserves this
header over initial, tool-result and auxiliary requests.

As checked on 2026-09-13, Go publishes a $15 monthly model-usage limit for
DeepSeek V4.1 Flash, with 20% per five-hour window ($3) and 50% weekly ($7.50).
These are model-usage dollars, not the $10 subscription price, request-count
guarantees or the account's remaining balance. Limits may change. OpenCode's
**Use balance** option can incur additional charges after the included limits;
do not enable it implicitly or confuse OpenCode's Zen balance with this Zen
control plane. Zen's [local usage statistics](opencode-local-usage.md) do not
establish remaining subscription quota.

## Why A Host Credential Reference Is Not Enough

Amp has a programmatic BYOK surface. The blocker is not a blanket absence of an
API: `add-router` takes the credential material to configure an Amp account-side
connection. `--api-key-file` is an input mechanism, not a persistent reference to
OpenCode's JSON auth source. Sending a parsed key through stdin would still copy
it into Amp's credential custody. No documented local resolver, credential
callback or existing-Zen-reference option was found in the interfaces above.

Zen's provider owner and credential store resolve secrets behind local
Codex/Claude routing boundaries. They do not provide an Amp account-side
credential bridge. Amp's SDK and plugin model IDs do not change that boundary.
Pointing `AMP_URL`, `amp.proxy`, or an OpenAI environment variable at Go would
not implement the documented BYOK contract.

A separately authorized HTTPS gateway could keep the Go key on the host and
give Amp a restricted gateway credential. That is a new authenticated network
service, not a reference to Zen's existing loopback router. It requires an
Amp-reachable endpoint, explicit authority, and its own routing, session,
redaction, quota, streaming and cancellation verification. Do not expose the
daemon, publish a tunnel, reuse pairing credentials, or weaken auth to create it.

When credential copying and new network exposure are forbidden and no suitable
Amp-side connection already exists, stop with this blocker. Do not add a
nonfunctional provider, silently switch to Amp credits or a DeepSeek direct
key, or treat a mock/direct upstream completion as Amp acceptance.

## External Launch Handoff

An authenticated Amp CLI can run on the current daemon host from an ordinary
terminal using its native configuration:

```sh
amp --visibility private --no-ide --no-remote-control-terminal
```

This keeps native mode and billing selection; it does not enable Go or guarantee
free inference. Amp owns permissions, tool execution, cancellation, thread
storage and cross-client access. It may sync private threads to Amp.

For a later explicitly authorized catalog change, Zen's existing custom-executor
mechanism can append a terminal-only entry:

```toml
[[executors]]
name = "amp"
command = "amp --visibility private --no-ide --no-remote-control-terminal"
```

Do not replace the executor file or change `delegated_executor`. New definitions
require a separately authorized daemon restart; this document does not perform
one. A custom tmux entry has no Amp structured events, native resume or quota
integration. Do not set `kind = "pi"`, `"claude"` or `"opencode"` to manufacture
those capabilities. The same terminal-only boundary applies to Android and iOS.

## Verification And Fallback

An eventual connected route must pass a bounded completion through Amp itself,
not merely Go's HTTP API. `check-access` is a real, potentially billable inference
on an owned Amp thread and returns `modelRouting`; it is not a read-only quota
check. Inspect routing attribution before claiming the Go subscription was used.

After that, separately verify an allowed read of an owned scratch marker and a
matching tool result/final answer, permission denial, cancellation and process
cleanup, stream lifecycle, route reload and model unavailability. Restrict tool
and MCP permissions; do not bypass approval or enable arbitrary plugins for a
smoke test. Catalog `tools: true` is metadata, not executed-tool evidence.

Keep missing Amp login, missing BYOK connection, router entitlement denial,
provider authentication failure, quota exhaustion, model unavailability and
transport failure distinct. Do not invent remaining quota or retry a failed
subscription request through paid credits. The stream contract exposes terminal
error results and permission denials, but does not establish a universal typed
quota-error taxonomy for Zen.

Amp documents that an unavailable/restricted pinned model falls back to Auto.
That can change billing; it is not a fail-closed OpenCode-only guarantee. With
BYOK absent, preserve ordinary Amp behavior unchanged and label it honestly as
Amp-native routing. With an explicit Go-only request, an unproven route remains
unavailable, rather than silently substituting ordinary Amp behavior.
