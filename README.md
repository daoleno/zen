<p align="center">
  <img src="app/assets/branding/zen-logo-mark-transparent.png" width="112" alt="Zen logo">
</p>

<h1 align="center">Zen</h1>

<h3 align="center">Your coding agents, wherever you are.</h3>

<p align="center">
  Zen keeps coding agents running on your own computer and gives you Chat, live Terminal,
  Sessions, and persistent Brain on your phone. Your code and credentials stay on your machine.
</p>

<p align="center">
  <a href="https://github.com/daoleno/zen/releases"><img alt="GitHub release" src="https://img.shields.io/github/v/release/daoleno/zen?sort=semver"></a>
  <a href="https://github.com/daoleno/zen/actions/workflows/release-artifacts.yml"><img alt="Release build" src="https://github.com/daoleno/zen/actions/workflows/release-artifacts.yml/badge.svg"></a>
  <a href="https://testflight.apple.com/join/rTKCDzMt"><img alt="TestFlight Preview" src="https://img.shields.io/badge/TestFlight-Preview-0D96F6.svg"></a>
  <a href="LICENSE"><img alt="License: Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue.svg"></a>
</p>

<p align="center">
  <img src="docs/assets/zen-overview.webp" width="920" alt="Zen mobile app showing an active structured coding-agent chat, session list, persistent Brain workspace, and usage stats">
</p>

## What you can do

- **Continue in Chat** — follow structured agent messages, tool calls, plans, and progress.
- **Take over in Terminal** — open the live tmux session when you need the raw interface.
- **Manage Sessions** — see what is running, finished, or waiting for input across projects.
- **Keep work moving in Brain** — preserve useful context, organize ongoing work, and pick it up later.

<table>
  <tr>
    <td width="50%"><img src="docs/assets/zen-chat.webp" alt="Structured Zen Chat with a fictional agent task, tool progress, and a completed result"></td>
    <td width="50%"><img src="docs/assets/zen-sessions.webp" alt="Zen Sessions overview containing only fictional projects and agent activity"></td>
  </tr>
  <tr>
    <td align="center"><strong>Structured Chat</strong><br>Stay with the agent without losing its working context.</td>
    <td align="center"><strong>Sessions</strong><br>See active, completed, and attention-needed work at a glance.</td>
  </tr>
  <tr>
    <td width="50%"><img src="docs/assets/zen-brain.webp" alt="Zen Brain orchestrating fictional work with a persistent workspace"></td>
    <td width="50%"><img src="docs/assets/zen-stats.webp" alt="Zen Stats showing fictional usage and activity data"></td>
  </tr>
  <tr>
    <td align="center"><strong>Brain</strong><br>Stay oriented across chats while Brain plans and advances ongoing work.</td>
    <td align="center"><strong>Stats</strong><br>Understand agent activity and usage from your own daemon.</td>
  </tr>
</table>

## Brain keeps work moving

Brain keeps objectives, decisions, open threads, and next steps together across chats, devices, and restarts.

## Quick start

Install the daemon on Linux (`amd64`/`arm64`), WSL, or an Apple Silicon Mac:

```sh
curl -fsSL https://raw.githubusercontent.com/daoleno/zen/main/install.sh | sh
```

For other install options, see [Install the daemon](docs/install-daemon.md).

Then, on the computer that has `tmux` and an authenticated coding-agent CLI:

```bash
zen doctor
zen
```

Use the normal self-managed path: `zen --lan` on trusted Wi-Fi/Tailnet and run
the exact pairing command it prints, or run `zen pair
https://your-origin.example` for a Cloudflare Tunnel or reverse-proxy origin.

Zen Link is optional source capability for operators who explicitly configure
`link.json`; only then does bare `zen pair` create a Link pairing code. This
repository does not configure, start, deploy, or claim a live Link service.

Get the mobile app from the [Android guide](docs/android.md) or [public TestFlight](https://testflight.apple.com/join/rTKCDzMt), then open it and scan or import the pairing code.

For Link, LAN, Tailscale, Cloudflare Tunnel, and reverse proxy options, see
[Connect and pair](docs/connect-and-pair.md). Relay operators start with
[Zen Link Relay operations](docs/zen-link-relay.md).

## How it works

The Zen daemon runs beside your repositories, authenticated agent CLIs, and
tmux sessions on your Linux computer or Apple Silicon Mac. Android and iOS
normally connect through your self-managed full origin. An explicitly
configured Zen Link can instead use an opaque relay with daemon-terminated
pinned TLS. Zen handles one-time enrollment and signed device requests; relay
infrastructure does not receive application plaintext.

Read [Security and privacy](docs/security-and-privacy.md) for the trust model and [Architecture](docs/architecture.md) for protocol details.

## Learn more

Start with the [documentation guide](docs/README.md). Contributions are welcome—see [CONTRIBUTING.md](CONTRIBUTING.md).

Zen is licensed under the [Apache License 2.0](LICENSE). Attribution and brand guidance are in [NOTICE](NOTICE) and [TRADEMARKS.md](TRADEMARKS.md).
