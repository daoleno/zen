# Repository Guidelines

## Project Structure & Module Organization

This repository is a monorepo for `zen`, a mobile-native agent control plane. The Go daemon lives in `daemon/`; key packages include `daemon/cmd/zen`, `daemon/server`, `daemon/auth`, `daemon/work`, `daemon/terminal`, and `daemon/watcher`. The Expo/React Native app lives in `app/`; screens are under `app/app`, shared UI under `app/components`, services under `app/services`, state under `app/store`, constants under `app/constants`, and assets under `app/assets`. Design and architecture notes live in `docs/`.

## Build, Test, and Development Commands

- `bun install`: install workspace dependencies from the repo root.
- `bun run app:start`: start the Expo dev server.
- `bun run app:android`: run the Android app with the expected Java 17 environment.
- `cd app && bunx tsc --noEmit`: type-check the React Native app.
- `bun run app:doctor`: run Expo project diagnostics.
- `bun run daemon:build`: build `bin/zen`.
- `bun run daemon:test`: run all Go daemon tests.
- `cd daemon && go run ./cmd/zen-dev`: rebuild and restart the daemon during development (then `zen pair https://your-host.example` when the origin is reachable).

## Coding Style & Naming Conventions

Use TypeScript for app code and Go for daemon code. Match local style: two-space indentation in TS/TSX, `gofmt` for Go, PascalCase React components, `use...` React hooks, and camelCase TypeScript variables/functions. Keep terminal/Codex UI code in `app/components/terminal`. Prefer typed service boundaries over ad hoc JSON handling.

## Cross-Platform Product Contract

All product features, fixes, architecture, and shared infrastructure must be designed for Android and iOS by default. Web is outside the required product contract unless explicitly scoped. The first platform to reproduce a bug sets validation priority, not the solution boundary. Prefer the framework/provider’s shared mobile owner and one shared truth owner. Platform-specific code is allowed only when the underlying capability is genuinely platform-exclusive; isolate it behind a shared contract, and give the counterpart mobile platform explicit behavior rather than accidental omission.

Zen may remember multiple server configurations, but exactly one server is current at a time. Sessions, Brain, Terminal, Calendar, Work, Skills, and adjacent presentation must consume the canonical current-server owner and show data only for that server. Feature screens must not enumerate or aggregate servers, invent fallback server selection, or add feature-local server pickers. Switching the current server must rebind or clear visible state so data from the previous server cannot remain on screen.

## Testing Guidelines

Daemon tests use Go’s standard `testing` package and follow `*_test.go` naming. Run `cd daemon && go test ./...` before daemon, protocol, auth, terminal, or work/session changes. The app currently relies on TypeScript checks and Expo diagnostics; run `cd app && bunx tsc --noEmit` after TS/TSX changes and use Expo locally for UI verification.

`app/app/` is the Expo Router runtime route tree. Never place unit-test modules such as `*.test.*` or `*.spec.*` there, and never import `bun:test` from anything under that tree. Put tests in an appropriate non-route location such as `app/services/` or `app/components/`, even when the production hook or module lives under `app/app/`. Before finishing changes that add or move files under `app/app/`, require residual checks for test/spec filenames and `bun:test` imports (`rg --files app/app | rg '\.(test|spec)\.'` and `rg -n 'bun:test' app/app`); when route membership changed, include Expo Android and iOS bundle/export proofs (`bunx expo export --platform android` and `bunx expo export --platform ios`).

## Commit & Pull Request Guidelines

Recent commits use short imperative titles, for example `Polish Brain and Codex chat UI` or `Fix Codex chat thread refresh after /new`. Keep commits scoped and avoid mixing daemon, app, and docs changes unless the behavior requires it. Pull requests should include a concise summary, verification commands run, linked issue or context, and screenshots or recordings for visible mobile UI changes.

## Security & Configuration Tips

Do not commit local pairing links, daemon state, secrets, tunnel URLs, or `.env.local` values. The project is self-hosted and has no hosted relay; keep auth, WebSocket, upload, and pairing changes explicit and documented.
