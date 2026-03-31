# zen - Mobile Agent Control Plane

## Project
- Monorepo: `daemon/` (Go) + `app/` (Expo/React Native)
- Daemon polls tmux sessions, classifies agent state, serves WebSocket
- App connects to any user-provided endpoint: LAN, tailnet, reverse proxy, or tunnel

## Testing
- Daemon: `cd daemon && go test ./...`
- App: `cd app && npx expo export --platform android` (build check)

## Architecture
- Connection: self-hosted endpoint chosen by the user, no hosted relay required
- Pairing: daemon can export `zen://...` deep links and QR codes for OSS onboarding
- Auth: optional shared secret on WebSocket and upload endpoints
- Push: Expo Push API (HTTP POST from daemon)
- State classification: regex pattern matching on tmux capture-pane output
- Terminal rendering: React Native FlatList + ANSI color parser

## Key Decisions
- Dense interrupt list (NOT cards) for Inbox
- 2-tab navigation: Inbox + Settings
- Zen state replaces Inbox when all agents are running + connected
- Quick actions have state version safety checks
- v1 defers: voice input, image upload, shared-element transitions, libghostty
