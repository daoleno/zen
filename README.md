# Zen

Use your AI coding agents from your phone.

Zen connects an Android app to coding agents running on your own Linux machine. It gives you mobile chat, terminal access, session management, and a persistent Brain workspace without moving your repositories or provider credentials into a hosted service.

## What it supports

- Codex, Claude Code, Cursor Agent, Grok, and custom tmux-backed commands
- Structured chat when the provider exposes a transcript
- Full terminal access when chat is not enough
- Brain for persistent context and delegated work
- One-time QR/link pairing with signed requests afterward
- Linux `amd64` and `arm64` hosts
- Android `arm64-v8a` sideload builds

Zen does not currently provide an iOS app, Play Store distribution, a hosted relay, or automatic NAT traversal.

## Quick start

You need a Linux machine with `tmux`, one supported AI CLI already signed in, an Android arm64 phone, and a network path between them.

1. Open [GitHub Releases](https://github.com/daoleno/zen/releases) and download the daemon for your Linux architecture, the Android APK, and `SHA256SUMS`.
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

The same Linux user running `zen` must be able to run `tmux` and at least one authenticated AI CLI. Run `zen doctor`, then use `zen setup` if you need to create an executor configuration.

Some built-in executor profiles disable approval prompts or sandboxes. Read [Executors and permissions](docs/executors.md) before using autonomous profiles on a machine with sensitive data.

## How it works

```text
Android app
    │ signed HTTP and WebSocket requests
    ▼
your Tailnet or HTTPS endpoint
    ▼
Zen daemon on your Linux machine
    │ local tmux sessions and provider transcripts
    ▼
Codex / Claude Code / Cursor Agent / Grok
```

Zen does not operate a hosted relay or store provider API keys. See [Security and privacy](docs/security-and-privacy.md) and [Architecture](docs/architecture.md) for the trust model.

## Documentation

The [documentation guide](docs/README.md) separates installation, product concepts, troubleshooting, development, and release maintenance.

Contributor information lives in [CONTRIBUTING.md](CONTRIBUTING.md). Zen is licensed under the [Apache License 2.0](LICENSE); see [NOTICE](NOTICE) and [TRADEMARKS.md](TRADEMARKS.md) for attribution and brand policy.
