# zen - Mobile Agent Control Plane

## Project
- Monorepo: `daemon/` (Go) + `app/` (Expo/React Native)
- Daemon polls tmux sessions, classifies agent state, serves WebSocket
- App connects via WSS through Tailscale Funnel or Cloudflare Tunnel

## Testing
- Daemon: `cd daemon && go test ./...`
- App: `cd app && npx expo export --platform android` (build check)

## Architecture
- Connection: WSS via Tailscale Funnel or Cloudflare Tunnel (no custom relay)
- Auth: HMAC-SHA256 with pairing secret
- Push: Expo Push API (HTTP POST from daemon)
- State classification: regex pattern matching on tmux capture-pane output
- Terminal rendering: React Native FlatList + ANSI color parser

## Key Decisions
- Dense interrupt list (NOT cards) for Inbox
- 2-tab navigation: Inbox + Settings
- Zen state replaces Inbox when all agents are running + connected
- Quick actions have state version safety checks
- v1 defers: voice input, image upload, shared-element transitions, libghostty
