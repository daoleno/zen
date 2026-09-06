# Repository Guidelines

Zen is a mobile-native control plane. Go code is in daemon/ (cmd/zen, server, auth, work, terminal, watcher); Expo/React Native code is in app/ (app routes, components, services, store, constants, assets). Product documentation is in docs/.

## Execution

Read the relevant code and directory instructions before editing. Complete authorized work, preserve unrelated changes, and use existing conventions and typed boundaries. Ask only for a consequential missing decision or new authority; finish independent authorized preparation first. Keep changes scoped and avoid compatibility layers unless requested.

## Product Invariants

- Android and iOS share every product feature by default; web is outside scope unless requested. The first reproducing platform sets test priority, not the solution boundary. Isolate genuinely platform-exclusive capabilities behind a shared contract with explicit counterpart behavior.
- Exactly one server is current. Sessions, Brain, Terminal, Calendar, Work, Skills and related UI use that canonical owner. Do not aggregate servers, add feature-local selection or invent fallbacks. On a server switch, rebind or clear old-server state.
- app/app/ contains runtime routes only. Keep test/spec files and bun:test imports outside it.
- Preserve auth, pairing, WebSocket, upload and filesystem boundaries. Never commit secrets, pairing links, daemon state, tunnel URLs or .env.local.

## Commands And Verification

- Install: bun install.
- App: bun run app:start; bun run app:android (Java 17); bun run app:doctor.
- Typecheck: cd app && bunx tsc --noEmit after TS/TSX edits.
- Daemon: bun run daemon:build; cd daemon && go test ./... before daemon/protocol/auth/terminal/work/session changes.
- cd daemon && go run ./cmd/zen-dev starts a hot-reloading daemon. Editing watched Go files can restart it; check live watchers before changes.
- For route additions/moves, check rg --files app/app | rg '\.(test|spec)\.' and rg -n 'bun:test' app/app. Verify Android and iOS bundles with bunx expo export --platform android and bunx expo export --platform ios.
- Test changed behavior and required repository gates. Broaden or repeat only after edits, failures or unresolved concerns. Use device/UI evidence for visible changes; report platform limitations.

## Style And Delivery

Use TypeScript, two-space indentation in TS/TSX, gofmt for Go, PascalCase components, use-prefixed hooks and camelCase functions. Keep terminal/provider UI in app/components/terminal. Use structured APIs instead of string parsing when available.

Update relevant product docs. When authorized to commit, use short imperative messages and scoped commits. Report behavior changes, verification and remaining risks; include screenshots for visible UI changes. Do not publish, deploy or restart live services without authorization.
