<p align="center">
  <img src="app/assets/branding/zen-logo-mark-transparent.png" width="96" alt="Zen logo">
</p>

<h1 align="center">Zen</h1>

<p align="center">
  <strong>One Brain, many coding agents, on your own computer. Steer them from your phone.</strong>
</p>

<p align="center">
  <a href="https://github.com/daoleno/zen/releases"><img alt="GitHub release" src="https://img.shields.io/github/v/release/daoleno/zen?sort=semver&include_prereleases"></a>
  <a href="https://github.com/daoleno/zen/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/daoleno/zen/actions/workflows/ci.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="License: Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue.svg"></a>
</p>

<p align="center">
  <img src="docs/assets/zen-overview.webp" width="880" alt="Zen mobile app: a structured agent chat, the session list, the Brain workspace and usage stats">
</p>

Zen is a self-hosted control plane for coding agents. A Go daemon runs on your
Linux or macOS machine next to your repositories, `tmux` and agent CLIs. An
Android and iOS app connects to that daemon.

- **Brain** holds the context and owns the plan: it decomposes a goal, delegates
  scoped concerns, reviews the results and decides when Work is done.
- **Workers** are visible `tmux` Sessions running an agent CLI (Codex, Claude
  Code, Cursor Agent, Grok, Pi or OpenCode). Open any of them as structured Chat
  or as the live Terminal, and take over at any time.
- **Work** is durable. Work, Attempt, Wake, Review and append-only Event records
  live in the daemon and survive restarts. A process exiting or going quiet never
  marks Work done; only Brain or you can accept it.

Zen is in **beta**. See [Status](#status) for what is stable, what is preview,
and what is not shipped.

## Quick start

Requirements: Linux (`amd64`/`arm64`), WSL or an Apple Silicon Mac, with `tmux`
and at least one authenticated agent CLI on `PATH`.

```sh
# 1. Install the daemon (checksum-verified, no sudo, no telemetry)
curl -fsSL https://raw.githubusercontent.com/daoleno/zen/main/install.sh | sh

# 2. Check the host, then start on a trusted private network
zen doctor
zen --lan

# 3. In another terminal, run the exact `zen pair ...` command that zen printed
```

4. Install the app: the Android arm64 APK from
   [Releases](https://github.com/daoleno/zen/releases) (see [Android](docs/android.md)),
   or build iOS from source (see [iOS](docs/ios.md)).
5. Scan the pairing code, then open **Brain** or **Sessions**.

To reach the daemon from outside your LAN, use Tailscale
(`zen -addr "$(tailscale ip -4):9876"`), or a Cloudflare Tunnel or reverse
proxy with `zen pair https://your-origin`. See [Connect and pair](docs/connect-and-pair.md).

## How it works

```text
 Android / iOS app ──signed requests / WSS──▶ zen daemon (your machine)
                                              │
                        ┌─────────────────────┼──────────────────────┐
                        ▼                     ▼                      ▼
                   Brain host           Zen Workers            lifecycle store
               (an agent CLI with     (tmux Sessions:        (Work, Attempt, Wake,
                private workspace)     codex, claude, …)      Review, Events)
```

- **Daemon** (`daemon/`, Go). Owns state, pairing identity, tmux sessions,
  executor processes and stored provider credentials. It is the only
  application endpoint. The phone receives the conversation and files you open.
- **App** (`app/`, Expo / React Native). Android and iOS share one product.
  Exactly one server is current at a time; switching servers never mixes data.
- **Brain.** A long-lived agent Session with a private workspace under
  `~/.zen/brain/` (memory, profile, current focus, worklogs). It delegates with
  the same `zen worker` CLI you can use yourself.
- **Workers and executors.** An executor is an agent CLI definition. New Workers
  run on the default delegated executor (`codex` unless changed) unless you ask
  for another; `zen worker spawn -executor` sets it explicitly. A running Worker
  keeps its executor.
- **Trust.** The daemon has an Ed25519 identity. Devices enroll once with a
  short-lived pairing token, then every request is signed. There is no shared
  secret for normal traffic. Paired phones receive what you open; model
  providers receive what agents send them.

Details: [Architecture](docs/architecture.md), [Brain lifecycle](docs/brain-lifecycle.md),
[Work lifecycle](docs/work-lifecycle.md), [Security and privacy](docs/security-and-privacy.md).

## Everyday commands

```sh
zen doctor                         # diagnose tmux, state, port and executors
zen pair [origin]                  # new one-time pairing code
zen devices list                   # paired phones
zen devices revoke -id <device-id>
zen update                         # verify and install the latest release

zen worker list --json             # visible Workers
zen worker spawn -name "Review docs" -executor claude -cwd ~/repo -prompt "Inspect docs"
zen worker capture -id <id> --json # transcript
zen worker receipt -id <id> --work-id <work>  # was an input accepted?
zen worker send -id <id> --work-id <work> -text "follow-up"
zen worker close -id <id>

zen brain executors --json         # Brain host and delegated executors
zen brain use <executor>           # switch the agent that runs Brain
zen brain set-delegated <executor> # change the default for new Workers, live
zen brain work list --json         # durable Work
zen brain work update -id <work> -status done
```

## Configure executors

No configuration is required: built-in defaults cover `codex`, `claude`,
`agent` (`cursor-agent`), `grok`, `pi` and `opencode`. One installed,
authenticated CLI is enough. To customise:

```sh
cp executors.example.toml ~/.zen/executors.toml   # then restart zen
```

> [!WARNING]
> Several defaults bypass approval prompts so agents can work unattended:
> `cursor-agent --force --sandbox disabled`, `grok --permission-mode bypassPermissions`,
> and Brain-delegated Codex adds `--dangerously-bypass-approvals-and-sandbox`.
> On machines with secrets or production access, use the safe profile in
> [`executors.example.toml`](executors.example.toml). See
> [Executors](docs/executors.md#permission-bypass-risks).

Model endpoints and API keys for Codex and Claude are set in the app under
**Settings > Providers**. They are stored on the daemon, never shown back, and are
separate from the Brain executor choice.

## Status

| Area | Status |
| --- | --- |
| Daemon on Linux `amd64`/`arm64`, WSL, Apple Silicon macOS | Beta, released |
| Android app (arm64 APK on Releases) | Beta, released |
| iOS app | Source build. A [TestFlight preview](https://testflight.apple.com/join/rTKCDzMt) is awaiting Apple review |
| Brain, Workers, durable Work lifecycle | Beta |
| Calendar scheduled actions, Telegram channel | Beta ([Calendar](docs/calendar.md), [Telegram](docs/telegram-brain-connection.md)) |
| Zen Link relay | Optional source only. No hosted relay is operated. See [Zen Link Relay](docs/zen-link-relay.md) |
| Remote Desktop | Implemented but hidden in the app. See [Remote Desktop](docs/remote-desktop.md) |
| Web client | Out of scope |

Known release issues: [docs/release-blockers.md](docs/release-blockers.md).

## Development

```sh
bun install                              # workspace deps (Bun 1.3)

# Daemon
bun run daemon:build                     # builds bin/zen
cd daemon && go test ./...
cd daemon && go run ./cmd/zen-dev        # hot-reloading dev daemon

# App
bun run app:start                        # Expo dev server
bun run app:android                      # needs Java 17
bun run app:ios
cd app && bun test && bunx tsc --noEmit

# Landing page (static, in site/)
bun run site:dev                         # prints the local preview URL
```

Layout: `daemon/` Go daemon (`cmd/zen`, `server`, `auth`, `brain`, `work`,
`lifecycle`, `terminal`, `watcher`); `app/` Expo app (routes in `app/app/`,
components, services, store); `docs/` product and operator docs; `site/`
landing page; `scripts/` build and release tooling.

The native terminal uses libghostty; see [Android](docs/android.md#architecture--abi-contract)
and [iOS](docs/ios.md#native-terminal--xcframework-contract) for build contracts.
All documentation starts at [docs/README.md](docs/README.md).

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) and [AGENTS.md](AGENTS.md). Keep
changes small, run the relevant checks, and never commit pairing links, `~/.zen`
state, tunnel URLs or `.env.local`. Report vulnerabilities as described in
[SECURITY.md](SECURITY.md).

## License

Apache License 2.0; see [LICENSE](LICENSE) and [NOTICE](NOTICE). The Zen name
and logos are covered by [TRADEMARKS.md](TRADEMARKS.md).
