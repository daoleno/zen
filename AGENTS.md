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
- `cd daemon && go run ./cmd/zen-dev -advertise-url https://your-host.example/ws`: rebuild and restart the daemon during development.

## Coding Style & Naming Conventions

Use TypeScript for app code and Go for daemon code. Match local style: two-space indentation in TS/TSX, `gofmt` for Go, PascalCase React components, `use...` React hooks, and camelCase TypeScript variables/functions. Keep terminal/Codex UI code in `app/components/terminal`. Prefer typed service boundaries over ad hoc JSON handling.

## Testing Guidelines

Daemon tests use Go’s standard `testing` package and follow `*_test.go` naming. Run `cd daemon && go test ./...` before daemon, protocol, auth, terminal, or work/session changes. The app currently relies on TypeScript checks and Expo diagnostics; run `cd app && bunx tsc --noEmit` after TS/TSX changes and use Expo locally for UI verification.

## Commit & Pull Request Guidelines

Recent commits use short imperative titles, for example `Polish Brain and Codex chat UI` or `Fix Codex chat thread refresh after /new`. Keep commits scoped and avoid mixing daemon, app, and docs changes unless the behavior requires it. Pull requests should include a concise summary, verification commands run, linked issue or context, and screenshots or recordings for visible mobile UI changes.

## Security & Configuration Tips

Do not commit local pairing links, daemon state, secrets, tunnel URLs, or `.env.local` values. The project is self-hosted and has no hosted relay; keep auth, WebSocket, upload, and pairing changes explicit and documented.
