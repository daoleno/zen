# zen

Mobile-native agent control plane. Manage your AI coding agents from your phone.

## Architecture

```
[Phone: Expo App]
    ↕ WSS
[Tailscale Funnel / Cloudflare Tunnel]
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

The daemon will display a pairing code and listen on `:9876`.

### 2. Expose via tunnel (choose one)

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

Scan the QR code with Expo Go on your phone. In Settings, enter your tunnel URL (e.g., `wss://your-machine.ts.net/ws`).

## Project Structure

```
zen/
├── daemon/                    Go, runs on homelab
│   ├── cmd/zen-daemon/        Main entry point
│   ├── classifier/            tmux output → agent state
│   ├── watcher/               tmux polling + send-keys
│   ├── server/                WebSocket server + message protocol
│   ├── auth/                  HMAC pairing authentication
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
