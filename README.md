# zen

Mobile-native agent control plane. Manage your AI coding agents from your phone.

## Honest beta scope

| Piece | Supported now | Not claimed yet |
| --- | --- | --- |
| Daemon host | Linux with Go, tmux, and at least one AI CLI on `PATH` | macOS/Windows as first-class hosts |
| Mobile app | Android (dev build / sideload) | iOS app parity, Play Store listing |
| Agents | One of Codex, Claude Code, Cursor Agent, or Grok is enough | “All executors work out of the box” |
| Network | Your Tailnet, reverse proxy, or tunnel | Hosted relay / zero-config NAT |

You do **not** need every executor installed. One authenticated AI CLI is enough for a useful first session.

License choice for the repository is still pending; see [CONTRIBUTING.md](CONTRIBUTING.md) and [docs/release-blockers.md](docs/release-blockers.md).

## Architecture

```
[Phone: Expo App]
    ↕ signed HTTP / WebSocket
    [Your Own Tailnet / Reverse Proxy / Tunnel]
    ↕
[Homelab: zen (Go)]
    ↕ tmux session scraping
[Claude Code] [Codex] [Other CLI Agents]
```

`zen` does not ship a hosted relay. Network reachability is delegated to whatever you already trust. `zen` itself handles daemon identity, device pairing, and request authentication.

## 10-minute path

### Prerequisites

- Linux host with `go` (see `daemon/go.mod`), `tmux`, and `git`
- At least one AI executor already installed and logged in (`codex`, `claude`, `cursor-agent`, or `grok`)
- Android phone for the app (Expo Go can paste/scan pairing links; custom `zen://` deep links need a dev build or APK)

### 1. Build and start the daemon

```bash
cd daemon
go build -o bin/zen ./cmd/zen/
./bin/zen
```

Default listen address: `127.0.0.1:9876`.
Default state directory: `~/.zen` (identity, trusted devices, pairing tokens).
If you previously used `~/.config/zen`, zen copies those files into `~/.zen` on first start (copy, not move).

Optional flags:

```bash
./bin/zen -addr 127.0.0.1:9876 -state-dir ~/.zen
```

### 2. Expose the full origin

Forward the **full daemon origin**, not only `/ws`. The app also uses `/health`, `/auth-check`, `/pair`, and `/upload`.

**Tailscale Funnel:**

```bash
tailscale funnel --https=443 http://127.0.0.1:9876
```

**Cloudflare Tunnel:**

```bash
cloudflared tunnel --url http://127.0.0.1:9876
```

### 3. Pair without restarting

```bash
./bin/zen pair https://zen.example.com
```

That prints a one-time `zen://...` link and QR for the existing daemon identity.

### 4. Run the Android app

```bash
bun install
cd app
npx expo start
```

In Settings: paste the link, scan the QR, or import a QR screenshot. Pairing enrolls the phone’s device key; later requests are signed and bound to the daemon you paired with.

More detail: [docs/install-daemon.md](docs/install-daemon.md), [docs/connect-and-pair.md](docs/connect-and-pair.md), [docs/android.md](docs/android.md).

### 5. Point zen at your executor (if needed)

Built-in defaults assume common CLI names on `PATH`. One executor is enough. Copy and edit [executors.example.toml](executors.example.toml) to `~/.zen/executors.toml` when you want safer or custom launch commands. See [docs/executors.md](docs/executors.md).

## User docs

- [Install the daemon](docs/install-daemon.md)
- [Connect and pair](docs/connect-and-pair.md)
- [Executors](docs/executors.md)
- [Android app](docs/android.md) (ABI contract, Ghostty native build, sideload APK)
- [Security and privacy](docs/security-and-privacy.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Architecture](docs/architecture.md)
- [Third-party assets](docs/third-party-assets.md)
- [Release blockers](docs/release-blockers.md)

Diagnostics: `zen doctor` (and `zen doctor --json`) check tmux, state dir, listen port, and executors without installing packages. Guided first-run config: `zen setup` (also `--non-interactive` for automation).

## Project structure

```
zen/
├── daemon/          Go daemon (tmux watcher, auth, WebSocket)
├── app/             Expo / React Native Android app
├── docs/            User and architecture docs
└── executors.example.toml
```

## Development

### Daemon

```bash
cd daemon
go test ./...
go build -o bin/zen ./cmd/zen/
go run ./cmd/zen-dev   # watch, rebuild, restart (same state-dir keeps identity)
```

Optional maintainer-only Grok fixture tests:

```bash
ZEN_GROK_REAL_SESSION=1 go test ./work -run Grok
```

### App

```bash
bun install
cd app
bun test
bunx tsc --noEmit
npx expo export --platform android
```

### Docker (advanced)

Host CLI + tmux integration is the main path. The [daemon/Dockerfile](daemon/Dockerfile) is for people who already know how to mount state, expose the origin, and reach host agent binaries. Prefer the native Linux binary for beta use.

## Security

See [SECURITY.md](SECURITY.md) and [docs/security-and-privacy.md](docs/security-and-privacy.md).
