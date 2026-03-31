# zen

Mobile-native agent control plane. Manage your AI coding agents from your phone.

## Architecture

```
[Phone: Expo App]
    ↕ WSS
[Your Own LAN / Tailnet / Reverse Proxy / Tunnel]
    ↕
[Homelab: zen-daemon (Go)]
    ↕ tmux session scraping
[Claude Code] [Codex] [Other CLI Agents]
```

## Quick Start

### 1. Start zen-daemon on your homelab

```bash
cd daemon
go build -o bin/zen-daemon ./cmd/zen-daemon/
./bin/zen-daemon
```

The daemon listens on `:9876`. On startup it now prints:

- reachable LAN or tailnet endpoints when it can detect them
- a `zen://...` import link for the primary endpoint
- an ASCII QR you can scan from your phone to import that endpoint directly in a dev build or installed app

Optional: protect a public or shared endpoint with a fixed secret.

```bash
SECRET=$(./bin/zen-daemon -gen-secret)
./bin/zen-daemon -secret "$SECRET"
```

If you already have your own public or tunneled URL, advertise it explicitly so the QR/deep link points at that endpoint:

```bash
./bin/zen-daemon -advertise-url https://zen.example.com
```

### 2. Expose it however you want

`zen` does not require an official relay. Any endpoint that reaches the daemon works.

**Same LAN / reverse proxy:**
Point the mobile app at your own `ws://host:9876/ws` or `wss://.../ws` endpoint.

**Tailscale / Tailnet:**
Use your tailnet address directly or add Funnel if you want public reachability.

**Tailscale Funnel:**
```bash
tailscale funnel 9876
```

**Cloudflare Tunnel:**
```bash
cloudflared tunnel --url http://localhost:9876
```

### 3. Run the mobile app

```bash
cd app
npm install
npx expo start
```

Scan the QR code with Expo Go on your phone. In Settings, enter your endpoint URL, for example:

- `ws://192.168.1.10:9876/ws`
- `wss://your-machine.ts.net/ws`
- `wss://zen.example.com/ws`

If you started `zen-daemon` with `-secret`, the generated deep link / QR already contains the same 64-character hex secret. You can still enter it manually in Settings if needed.

If you are running the app inside Expo Go, add the endpoint manually in Settings. Custom `zen://...` import links require a dev build or a shipped app binary.
You can also paste the generated `zen://...` link or JSON payload directly into Settings -> Import Link.
The app also includes an in-app QR scanner in Settings -> Scan QR.

## Project Structure

```
zen/
├── daemon/                    Go, runs on homelab
│   ├── cmd/zen-daemon/        Main entry point
│   ├── classifier/            tmux output → agent state
│   ├── watcher/               tmux polling + send-keys
│   ├── server/                WebSocket server + message protocol
│   ├── auth/                  Optional shared-secret auth
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
```

### App
```bash
cd app
npx expo start          # Dev server
npx expo export --platform android  # Verify build
```

## Design Doc

Full design document with architecture decisions, competitive analysis, and review history:
`~/.gstack/projects/zen/daoleno-unknown-design-20260325-190643.md`
