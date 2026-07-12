# Zen

Use your AI coding agents from your phone.

Zen connects an Android app to the coding agents already running on your own Linux machine. Start and follow sessions, use structured chat when the provider supports it, open the full terminal when you need it, and let Brain coordinate longer work without moving your repositories or credentials into a hosted service.

Your machine remains the host. Zen provides the mobile interface, pairing, authenticated transport, and local agent/session management.

## What you can do

- Start and resume Codex, Claude Code, Cursor Agent, and Grok sessions.
- Read and reply through a mobile chat interface.
- Open the underlying terminal for full CLI control.
- Use Brain as a persistent workspace and agent orchestrator.
- Pair your phone with your own daemon using a one-time QR code or link.
- Keep provider credentials, repositories, transcripts, and agent processes on your machine.

## Current beta

Zen `v0.1.0-beta.1` currently supports:

| Component | Supported |
| --- | --- |
| Host | Linux, `amd64` or `arm64` |
| Mobile app | Android `arm64-v8a` sideload APK |
| Agents | Codex, Claude Code, Cursor Agent, Grok, and custom tmux-backed commands |
| Connection | Tailscale, a private network, or your own HTTPS tunnel/reverse proxy |
| Terminal | Native Android terminal powered by Ghostty VT |

This beta does not include an iOS app, Play Store distribution, a hosted Zen relay, or automatic NAT traversal.

## Install

You need:

- a Linux machine with `tmux`;
- at least one supported AI CLI installed and already signed in;
- an Android arm64 phone;
- a network path from the phone to the daemon, preferably Tailscale or another private network.

You do **not** need every supported AI CLI. One working executor is enough.

### 1. Install the daemon

Download the binary for your Linux machine from the [`v0.1.0-beta.1` GitHub release](https://github.com/daoleno/zen/releases/tag/v0.1.0-beta.1):

```bash
# Choose one:
curl -LO https://github.com/daoleno/zen/releases/download/v0.1.0-beta.1/zen-linux-amd64
# curl -LO https://github.com/daoleno/zen/releases/download/v0.1.0-beta.1/zen-linux-arm64

chmod +x zen-linux-*
install -m 755 zen-linux-* ~/.local/bin/zen
zen doctor
zen
```

Zen listens on `127.0.0.1:9876` by default and stores its state in `~/.zen`.

### 2. Make the daemon reachable

Expose the **entire daemon origin**, not only its WebSocket route. Tailscale is the recommended starting point because it keeps Zen off the public internet.

If you deliberately use a public HTTPS tunnel, for example:

```bash
tailscale funnel --https=443 http://127.0.0.1:9876
# or
cloudflared tunnel --url http://127.0.0.1:9876
```

Use the resulting HTTPS origin in the next step. See [Connect and pair](docs/connect-and-pair.md) for private Tailnet and reverse-proxy setups.

### 3. Pair your phone

With the daemon still running:

```bash
zen pair https://your-zen-origin.example
```

Zen prints a one-time pairing link and QR code. The token expires after 15 minutes.

### 4. Install the Android app

Download [`zen-android-arm64-v0.1.0-beta.1.apk`](https://github.com/daoleno/zen/releases/download/v0.1.0-beta.1/zen-android-arm64-v0.1.0-beta.1.apk) from the GitHub release and install it on an arm64 Android device.

Android will ask you to allow installation from the browser or file manager you used. Because this is a sideloaded beta, Play Protect may also show a warning. Verify the release checksum and signing certificate before installing:

```text
C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:
4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD
```

Open Zen, import the pairing QR/link in Settings, then choose an existing session or create a new one.

## After installation

- If no agents appear, confirm that `tmux` and one authenticated AI CLI are available to the same Linux user running `zen`.
- Run `zen doctor` for host, port, state-directory, and executor diagnostics.
- Run `zen setup` to create an executor configuration interactively.
- Read [Executors and permissions](docs/executors.md) before enabling approval-bypass flags on a machine containing sensitive data.

## Documentation

Start with the [documentation guide](docs/README.md), or go directly to:

- [Install or upgrade the daemon](docs/install-daemon.md)
- [Connect and pair a phone](docs/connect-and-pair.md)
- [Configure executors](docs/executors.md)
- [Install or build the Android app](docs/android.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Security and privacy](docs/security-and-privacy.md)

## How it works

```text
Android app
    │ signed HTTP and WebSocket requests
    ▼
your Tailnet / HTTPS endpoint
    │
    ▼
Zen daemon on your Linux machine
    │ local tmux sessions and provider transcripts
    ▼
Codex / Claude Code / Cursor Agent / Grok
```

Zen does not operate a hosted relay. It does not store your provider API keys. Each phone is enrolled with its own device key, and normal requests are signed against the daemon identity created on your machine.

For the complete trust model, see [Architecture](docs/architecture.md) and [Security and privacy](docs/security-and-privacy.md).

## Development

Contributor setup, source builds, tests, native Android details, and release automation are documented separately:

- [Development and repository guide](docs/README.md#development-and-maintenance)
- [Contributing](CONTRIBUTING.md)
- [CI release pipeline](docs/ci-release.md)
- [Third-party assets and notices](docs/third-party-assets.md)

Zen is licensed under the [Apache License 2.0](LICENSE). See [NOTICE](NOTICE) for attribution and [TRADEMARKS.md](TRADEMARKS.md) for the project name and logo policy.
