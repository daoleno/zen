# Contributing

Thanks for helping with Zen.

## Scope of this beta

Honest support is **Linux daemon + Android app**, with **one** AI CLI enough to be useful. See the root README platform matrix before proposing “add iOS parity” as a small fix.

## Before you start

- Read `README.md` and `docs/architecture.md` for pairing/auth invariants.
- Do not commit pairing links, `~/.zen` state, `.env.local`, tunnel URLs, or APK signing keys.
- License choice for the repository is **pending**; do not assume a specific open-source license until a root `LICENSE` is added.

## Development

```bash
# Daemon
cd daemon && go test ./... && go build -o bin/zen ./cmd/zen/

# App
bun install
cd app && bun test && bunx tsc --noEmit
```

Optional Grok real-session integration tests (maintainer machines only):

```bash
ZEN_GROK_REAL_SESSION=1 go test ./work -run Grok
```

## Style

- TypeScript/TSX: 2-space indent, match nearby files.
- Go: `gofmt`.
- Keep commits focused; prefer short imperative subjects.

## Pull requests

- Summarize user-visible behavior and risk (especially auth, pairing, executors).
- Note verification commands you ran.
- UI changes: include a screenshot or short recording when practical.

## Docs

User docs are plain Markdown under `docs/` linked from the README. Prefer updating those over adding a docs framework.
