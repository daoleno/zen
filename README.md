<p align="center">
  <img src="app/assets/branding/zen-logo-mark-transparent.png" width="128" alt="Zen logo">
</p>

<h1 align="center">Zen</h1>

<p align="center">Use your AI coding agents from your phone.</p>

<p align="center">
  <a href="https://github.com/daoleno/zen/releases"><img alt="GitHub release" src="https://img.shields.io/github/v/release/daoleno/zen?include_prereleases&sort=semver"></a>
  <a href="https://github.com/daoleno/zen/actions/workflows/release-artifacts.yml"><img alt="Release build" src="https://github.com/daoleno/zen/actions/workflows/release-artifacts.yml/badge.svg"></a>
  <a href="LICENSE"><img alt="License: Apache-2.0" src="https://img.shields.io/badge/license-Apache--2.0-blue.svg"></a>
  <img alt="Platforms: Linux, macOS, and Android" src="https://img.shields.io/badge/platforms-Linux%20%7C%20macOS%20%7C%20Android-6f8f79">
</p>

Zen connects an Android app to coding agents running on your own Linux or Apple Silicon Mac. It gives you mobile chat, terminal access, session management, and a persistent Brain workspace without moving your repositories or provider credentials into a hosted service.

## What it supports

- Codex, Claude Code, Cursor Agent, Grok, and custom tmux-backed commands
- Structured chat when the provider exposes a transcript
- Full terminal access when chat is not enough
- Brain for persistent context and delegated work
- One-time QR/link pairing with signed requests afterward
- Linux `amd64`/`arm64` and macOS Apple Silicon hosts
- Android `arm64-v8a` sideload builds

Zen does not currently provide an iOS app, Play Store distribution, a hosted relay, or automatic NAT traversal.

## Quick start

You need a supported Linux machine or Apple Silicon Mac with `tmux`, one supported AI CLI already signed in, an Android arm64 phone, and a network path between them.

1. Open [GitHub Releases](https://github.com/daoleno/zen/releases) and download the daemon archive for your host, the Android APK, and `SHA256SUMS`.
2. Verify the downloads, install the daemon as `zen`, then run:

   ```bash
   zen doctor
   zen
   ```

3. Make the full daemon origin reachable from your phone. A private Tailnet is the recommended starting point.
4. Generate a short-lived pairing QR/link:

   ```bash
   zen pair https://your-zen-origin.example
   ```

5. Install the APK, open Zen, and import the pairing QR/link in Settings.

Detailed instructions:

- [Install or upgrade the daemon](docs/install-daemon.md)
- [Connect and pair a phone](docs/connect-and-pair.md)
- [Install the Android app](docs/android.md)

## If agents do not appear

The same host user running `zen` must be able to run `tmux` and at least one authenticated AI CLI. Run `zen doctor`, then use `zen setup` if you need to create an executor configuration.

Some built-in executor profiles disable approval prompts or sandboxes. Read [Executors and permissions](docs/executors.md) before using autonomous profiles on a machine with sensitive data.

## How it works

```text
Android app
    │ signed HTTP and WebSocket requests
    ▼
your Tailnet or HTTPS endpoint
    ▼
Zen daemon on your Linux machine or Apple Silicon Mac
    │ local tmux sessions and provider transcripts
    ▼
Codex / Claude Code / Cursor Agent / Grok
```

Zen does not operate a hosted relay or store provider API keys. See [Security and privacy](docs/security-and-privacy.md) and [Architecture](docs/architecture.md) for the trust model.

## Documentation

The [documentation guide](docs/README.md) separates installation, product concepts, troubleshooting, development, and release maintenance.

Contributor information lives in [CONTRIBUTING.md](CONTRIBUTING.md). Zen is licensed under the [Apache License 2.0](LICENSE); see [NOTICE](NOTICE) and [TRADEMARKS.md](TRADEMARKS.md) for attribution and brand policy.
