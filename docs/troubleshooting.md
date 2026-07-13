# Troubleshooting

## Daemon will not start

- If you installed a release binary, confirm it is executable and on `PATH`: `command -v zen && zen doctor`.
- If you built from source, confirm the build succeeds: `cd daemon && go build -o bin/zen ./cmd/zen/`.
- Ensure nothing else is bound to `127.0.0.1:9876`; use `--lan` for direct trusted-network access or `-addr` for an advanced explicit bind.
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
- Native terminal requires a platform-specific Ghostty binary: Android uses `libghostty_vt.so`; iOS uses `GhosttyVt.xcframework`. See [android.md](android.md) and [ios.md](ios.md).
- If the Android terminal is missing after clone: run `./scripts/build-libghostty.sh` then `./scripts/verify-libghostty.sh`, or install a maintainer APK that embeds the arm64 library.
- If the iOS Pod install reports a missing `GhosttyVt.xcframework`: run `bun run native:build:ios` and `bun run native:verify:ios` from the repository root, then regenerate/install the native project.
- If iOS launches with a dyld/Swift symbol error in an Expo module, keep `EXPO_USE_PRECOMPILED_MODULES=false` through `withZenIOSBuild` and reinstall Pods. Do not mix precompiled Expo frameworks with locally resolved ExpoModulesCore sources.
- ABI/ELF mismatches: do not drop `.so` files for `armeabi-v7a` / `x86`; only lockfile ABIs are valid.

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
- Default state is `~/.zen`.

## Diagnostics

Run `zen doctor` (or `zen doctor --json`) for machine readiness. If doctor is clean enough to choose an executor, `zen setup` can write `~/.zen/executors.toml` and print pair next steps.
