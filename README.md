# zen

Mobile-native agent control plane. Manage your AI coding agents from your phone.

## Architecture

```
[Phone: Expo App]
    ↕ signed HTTP / WebSocket
[Your Own Tailnet / Reverse Proxy / Tunnel]
    ↕
[Homelab: zen-daemon (Go)]
    ↕ tmux session scraping
[Claude Code] [Codex] [Other CLI Agents]
```

`zen` does not ship a hosted relay. Network reachability is delegated to whatever you already trust: Cloudflare Tunnel, Tailscale, your own reverse proxy, or a private mesh. `zen` itself is responsible for daemon identity, device pairing, and request authentication.

## Quick Start

### 1. Start zen-daemon on your homelab

```bash
cd daemon
go build -o bin/zen-daemon ./cmd/zen-daemon/
./bin/zen-daemon -advertise-url https://your-host.example/ws
```

The daemon listens on `127.0.0.1:9876` by default. On startup it now prints:

- its persistent daemon identity
- `Mode: LOCAL-ONLY` when it has no advertised URL yet
- or `Mode: PAIRABLE`, plus a one-time compact `zen://settings?p=...` pairing link and QR code, when `-advertise-url` is set

If you do not pass `-advertise-url`, the daemon still starts in `LOCAL-ONLY` mode, but it cannot pair a phone yet. Expose `http://127.0.0.1:9876` through your network layer first, then either restart with `-advertise-url` or generate a fresh link later with:

```bash
./bin/zen-daemon pair -advertise-url https://zen.example.com/ws
```

```bash
./bin/zen-daemon \
  -addr 127.0.0.1:9876 \
  -advertise-url https://zen.example.com/ws \
  -state-dir ~/.config/zen
```

### 2. Expose it however you want

`zen` does not require an official relay. Any endpoint that reaches the daemon works.

Important: forward the full daemon origin, not just `/ws`. The app also uses `/health`, `/auth-check`, `/pair`, and `/upload`.

**Tailscale / Tailnet:**
Expose the daemon through your tailnet, Funnel, or an HTTPS reverse proxy that ultimately reaches `http://127.0.0.1:9876`.

**Tailscale Funnel:**
```bash
tailscale funnel --https=443 http://127.0.0.1:9876
```

**Cloudflare Tunnel:**
```bash
cloudflared tunnel --url http://127.0.0.1:9876
```

### 3. Run the mobile app

```bash
bun install
cd app
npx expo start
```

Import the pairing link from `zen-daemon`:

- paste the printed `zen://...` link into Settings
- scan the QR code from Settings
- import a screenshot or photo of the QR from Settings
- or use the clipboard import button

There is no shared secret. Pairing is one-time enrollment: the app presents its own device key, the daemon stores that device as trusted, and subsequent HTTP/WebSocket requests are signed by the device identity and bound to the daemon identity you paired with.

If you are running the app inside Expo Go, use Settings to paste or scan the pairing link. Custom `zen://...` deep links require a dev build or a shipped app binary.

Remote push registration is optional in OSS builds. To test Expo push with your own EAS project, set `ZEN_EXPO_PROJECT_ID` in `app/.env.local` or your shell before starting Expo. See `app/.env.example`.

## Project Structure

```
zen/
├── daemon/                    Go, runs on homelab
│   ├── cmd/zen-daemon/        Main entry point
│   ├── classifier/            tmux output → agent state
│   ├── watcher/               tmux polling + send-keys
│   ├── server/                WebSocket server + message protocol
│   ├── auth/                  Daemon identity + trusted device auth
│   ├── push/                  Expo Push API notifications
│   └── Dockerfile
│
└── app/                       Expo (React Native), runs on phone
    ├── app/                   Screens (expo-router)
    │   ├── (tabs)/index.tsx   Inbox (dense interrupt list)
    │   ├── (tabs)/settings.tsx Settings + connection
    │   ├── terminal/[id].tsx  Terminal (ANSI output + input)
    │   └── onboarding.tsx     First-run setup
    ├── constants/tokens.ts    Design tokens
    ├── services/              WebSocket client, ANSI parser, storage
    └── store/agents.tsx       State management
```

## Development

### Daemon
```bash
cd daemon
go test ./...           # Run tests
go build -o bin/zen-daemon ./cmd/zen-daemon/  # Build
go run ./cmd/zen-dev -advertise-url https://your-host.example/ws  # Watch, rebuild, restart
```

`zen-dev` is a development watcher for the Go daemon. It uses `fsnotify` to watch `*.go`, `go.mod`, and `go.sum`, rebuilds `zen-daemon`, and restarts it automatically after each save. This is reload, not in-process hot patching, so the process restarts, but daemon identity and persisted state stay stable as long as you keep the same `-state-dir`.

### App
```bash
bun install              # Workspace install from repo root
cd app
npx expo start          # Dev server
npx expo export --platform android  # Verify build
```

## Architecture Doc

The current OSS-core architecture document lives in:

- `docs/architecture.md`

It covers:

- external networking boundaries
- daemon identity and trusted device pairing
- compact `zen://settings?p=...` pairing links
- signed HTTP / WebSocket authentication
- storage and runtime responsibilities across the app and daemon
