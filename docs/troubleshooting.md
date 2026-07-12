# Troubleshooting

## Daemon will not start

- Confirm Go build succeeded: `cd daemon && go build -o bin/zen ./cmd/zen/`.
- Ensure nothing else is bound to `127.0.0.1:9876`, or pass `-addr`.
- Check state dir permissions on `~/.zen` (or your `-state-dir`).

## Phone cannot connect

1. From a machine that can reach the public origin, open `/health` on the **same host** you pair with (not only `/ws`).
2. Confirm the proxy forwards `/health`, `/auth-check`, `/pair`, `/upload`, and `/ws`.
3. Regenerate pairing: `zen pair https://your-origin` (tokens expire).
4. Re-import the fresh `zen://` link in Settings.
5. If you customized `-state-dir`, use the same value for `zen` and `zen pair`.

## “It paired but sessions are empty”

- Install and authenticate at least one AI CLI on the daemon host.
- Confirm `tmux` works for your user.
- Check `~/.zen/executors.toml` if present; a bad command string will fail launches.
- One executor is enough—missing Claude does not matter if you only use Codex.

## Chat missing / terminal unsupported

- Structured Chat needs agent transcript files for that tool; a brand-new empty session may show little until the CLI writes history.
- Native terminal requires Android + built `libghostty_vt.so` ([android.md](android.md)). Other platforms show an unsupported surface by design in this beta.

## Quiet Mode / World Window

- Mokugyo hit audio is bundled.
- World Window streams Mixkit CDN MP4s and needs network; offline will not load scenes.

## Tests fail on a clean checkout

Default `go test ./...` must not require `~/.grok`. Maintainer real-session tests are opt-in:

```bash
ZEN_GROK_REAL_SESSION=1 go test ./work -run 'Grok(Goal|Real)'
```

## Stale docs / removed flags

- There is **no** `-advertise-url`. Use `zen` then `zen pair <origin>`.
- Default state is `~/.zen`, not `~/.config/zen` (legacy is migrated).

## Diagnostics

Run `zen doctor` (or `zen doctor --json`) for machine readiness. If doctor is clean enough to choose an executor, `zen setup` can write `~/.zen/executors.toml` and print pair next steps.
