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
  <a href="https://github.com/daoleno/zen/releases"><img alt="GitHub release" src="https://img.shields.io/github/v/release/daoleno/zen?include_prereleases&sort=semver"></a>
  <a href="https://github.com/daoleno/zen/actions/workflows/release-artifacts.yml"><img alt="Release build" src="https://github.com/daoleno/zen/actions/workflows/release-artifacts.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="License: Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue.svg"></a>
</p>

<p align="center">
  <img src="docs/assets/zen-overview.webp" width="920" alt="Zen mobile app showing an active structured coding-agent chat, session list, persistent Brain workspace, and usage stats">
</p>

## What you can do

- **Continue in Chat** — follow structured agent messages, tool calls, plans, and progress.
- **Take over in Terminal** — open the live tmux session when you need the raw interface.
- **Manage Sessions** — see what is running, finished, or waiting for input across projects.
- **Keep context in Brain** — maintain a persistent workspace that can plan and delegate work.

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
    <td align="center"><strong>Brain</strong><br>Carry durable context across chats and delegate focused work.</td>
    <td align="center"><strong>Stats</strong><br>Understand agent activity and usage from your own daemon.</td>
  </tr>
</table>

## Quick start

Install the daemon from [GitHub Releases](https://github.com/daoleno/zen/releases), then on the computer that has `tmux` and an authenticated coding-agent CLI:

```bash
zen doctor
zen --lan
```

Zen prints a `zen pair` command for each detected private address. Keep the daemon running, open another terminal, and run the command for the same trusted network as your phone:

```bash
zen pair http://192.168.1.42:9876
```

Use the address Zen prints—not the example above. Open the mobile app and **scan or import the pairing code**.

For installation details, iOS source builds, or remote HTTPS access, see [Install the daemon](docs/install-daemon.md) and [Connect and pair](docs/connect-and-pair.md).

## How it works

The Zen daemon runs beside your repositories, authenticated agent CLIs, and tmux sessions on your Linux computer or Apple Silicon Mac. Connect the Android or iOS app over the same trusted private network, or provide an HTTPS endpoint for access away from home. You choose and control that network path; Zen then handles one-time enrollment and signed device requests.

Read [Security and privacy](docs/security-and-privacy.md) for the trust model and [Architecture](docs/architecture.md) for protocol details.

## Platform availability

| Component | Availability |
| --- | --- |
| Daemon | Release binaries for Linux `amd64`/`arm64` and Apple Silicon macOS |
| Android | `arm64-v8a` APK from GitHub Releases |
| iOS | Source build for arm64 devices and Apple Silicon Simulator; physical devices require your own Apple signing |

See the [Android](docs/android.md) and [iOS](docs/ios.md) guides for current installation and validation details.

## Learn more

Start with the [documentation guide](docs/README.md). Contributions are welcome—see [CONTRIBUTING.md](CONTRIBUTING.md).

Zen is licensed under the [Apache License 2.0](LICENSE). Attribution and brand guidance are in [NOTICE](NOTICE) and [TRADEMARKS.md](TRADEMARKS.md).
