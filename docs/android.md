# Android app

## Install the beta APK

Open [GitHub Releases](https://github.com/daoleno/zen/releases) and download the newest `zen-android-arm64-v*.apk` together with `SHA256SUMS`.

The APK supports 64-bit ARM Android devices (`arm64-v8a`). It does not support x86 phones or 32-bit ARM devices. iOS uses a separate source build; see [iOS app](ios.md).

Before installing, download `SHA256SUMS` from the same release and verify the APK:

```bash
sha256sum -c SHA256SUMS --ignore-missing
```

The official beta certificate SHA-256 fingerprint is:

```text
C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:
4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD
```

Android will ask you to allow installation from the browser or file manager you used. A Play Protect warning is possible because this beta is distributed outside Play Store.

After installation, follow [Connect and pair](connect-and-pair.md): use `zen --lan` on a trusted private network or keep bare `zen` behind an HTTPS endpoint, then scan or import the generated pairing code in Settings.

## Current scope

| Claim                                          | Status                                                                                              |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| Pair, reconnect, session list, structured Chat | Intended beta surface                                                                               |
| Native terminal (Ghostty VT)                   | Requires built `libghostty_vt.so` for **arm64-v8a** (devices) and optionally **x86_64** (emulator)  |
| Structured agent interfaces                    | Codex, Claude Code, Cursor Agent, Grok, Pi, and OpenCode share the same React Native Chat/Terminal UI used on iOS |
| iOS                                            | Separate source-build target; see [ios.md](ios.md)                                                  |
| Expo Go                                        | Can paste/scan pairing links; custom `zen://` deep links need a dev build/APK                       |
| Play Store                                     | Not part of this release foundation                                                                 |

## Architecture / ABI contract

Canonical machine-readable contract:

[`app/modules/zen-terminal-vt/native.lock.json`](../app/modules/zen-terminal-vt/native.lock.json)

| Android ABI | Zig target              | Role                              | Sideload required |
| ----------- | ----------------------- | --------------------------------- | ----------------- |
| `arm64-v8a` | `aarch64-linux-android` | Physical devices / release APK    | **Yes**           |
| `x86_64`    | `x86_64-linux-android`  | Emulator / host-side native debug | No                |

**Unsupported (invalid for zen terminal):** `armeabi-v7a`, `x86`, and any other ABI.

Invariants:

1. Only the ABIs listed above may appear under `app/modules/zen-terminal-vt/libs/android/`.
2. Release sideload APKs are **arm64-v8a only** (`-PreactNativeArchitectures=arm64-v8a`).
3. `libghostty_vt.so` and Ghostty C headers are **gitignored**. Strangers do not get terminal binaries from a bare clone until they build or consume release artifacts.
4. Toolchain and Android source-derivation pins live in `native.lock.json` (Zig version, Ghostty git commit, and checksummed upstream patches). Dirty Ghostty trees are refused by default.
5. Redistributed APKs must embed Ghostty MIT at `assets/notices/GHOSTTY-MIT.txt` (source: `app/assets/notices/GHOSTTY-MIT.txt`), packaged by Expo plugin `app/plugins/withZenAndroidRelease.js` during prebuild. Verify with `./scripts/verify-apk-notice.sh <apk>`.
6. Release-grade native builds require a **proven** Ghostty git commit equal to the pin plus every exact declared Android patch (`release_grade: true` in `build-manifest.json`). Patches are applied only in a disposable worktree; the shared Ghostty cache must remain clean. Dirty or no-git trees may build only with explicit developer overrides and **fail** `./scripts/verify-libghostty.sh --release`.
7. Raw and APK-packaged `libghostty_vt.so` artifacts must not import symbols listed in `android.forbidden_undefined_symbols`. `verify-libghostty.sh` and the APK verifiers enforce this before distribution.

Module wiring (source of truth for packaging):

- `app/modules/zen-terminal-vt/android/build.gradle` — `abiFilters` from `-PreactNativeArchitectures` (release: `arm64-v8a` only; unset: both supported ABIs); fails if the matching `libghostty_vt.so` is missing
- `app/modules/zen-terminal-vt/android/CMakeLists.txt` — imports `libs/android/${ANDROID_ABI}/libghostty_vt.so`
- `app/plugins/withZenAndroidRelease.js` — copies MIT notice into `android/app/src/main/assets/notices/` and wires optional env-based release signing

## Prerequisites

- Bun (see root `packageManager`)
- JDK 17 for native Android builds (see root `app:android` script)
- Android SDK / device or emulator for `expo run:android`
- For native terminal: Zig **0.15.2** (see lock) + a Ghostty checkout at the **pinned commit**

## Day-to-day JS workflow

From the monorepo root:

```bash
bun install
cd app
npx expo start
```

Typecheck and unit tests:

```bash
cd app
bun test
bunx tsc --noEmit
```

Bundle export check (no Play Store upload; does **not** require `libghostty_vt.so`):

```bash
cd app
npx expo export --platform android
```

## Pairing on device

1. Start `zen --lan` for trusted private-network access, or start bare `zen` behind an HTTPS endpoint.
2. In another terminal, run the complete `zen pair` command Zen or your HTTPS setup provides.
3. Open Settings in the app.
4. Paste the `zen://...` link, scan the QR, or import a QR photo.

Release builds explicitly allow cleartext HTTP so dynamic LAN and Tailscale IPs work. Use HTTP only on a trusted private network; use an HTTPS endpoint on shared or untrusted networks.

Remote Expo push is optional. To test it with your own EAS project, set `ZEN_EXPO_PROJECT_ID` (see `app/.env.example`). OSS builds work without push.

## Native terminal library

`app/modules/zen-terminal-vt/libs/android/*/libghostty_vt.so` is **gitignored**. Without those binaries, the terminal surface is unavailable even if Chat works.

### Build (reproducible path)

```bash
# Optional: set Ghostty source; otherwise clones pin into ~/.cache/zen/ghostty
# GHOSTTY_SRC=/path/to/ghostty
./scripts/build-libghostty.sh

# Device ABI only:
./scripts/build-libghostty.sh --abis arm64-v8a

# Verify ABI/ELF (+ checksums if present)
./scripts/verify-libghostty.sh
./scripts/verify-libghostty.sh --release    # arm64 only
./scripts/verify-libghostty.sh --contract   # CI: lock + notice + scripts, no .so
```

The builder validates each declared patch's upstream metadata, base commit,
and SHA-256, creates a detached disposable worktree at the pinned Ghostty
commit, applies the patch set there, and records the exact metadata under
`applied_patches` in the build manifest. The source cache's HEAD, status, and
worktree registry are checked again during cleanup.

Build outputs (all gitignored except the tracked lock/notice):

| Path                                  | Purpose                                                          |
| ------------------------------------- | ---------------------------------------------------------------- |
| `libs/android/<abi>/libghostty_vt.so` | Imported by CMake / jniLibs                                      |
| `libs/android/SHA256SUMS`             | Per-ABI digests for release notes                                |
| `libs/android/build-manifest.json`    | Pin + exact applied patches + provenance + Zig version + digests |
| `libs/android/GHOSTTY-MIT.txt`        | Copy of MIT notice for tarballs                                  |
| `android/src/main/cpp/ghostty/`       | Headers for JNI bridge                                           |

### Clean-clone expectations

| Path                  | Bare `git clone`                                                                         | After `build-libghostty.sh` | Maintainer release artifact                                    |
| --------------------- | ---------------------------------------------------------------------------------------- | --------------------------- | -------------------------------------------------------------- |
| Chat / pairing JS     | Works                                                                                    | Works                       | Works                                                          |
| Native terminal       | Unavailable                                                                              | Works (local)               | Works if APK includes arm64 `.so`                              |
| CI default (`ci.yml`) | Bounded `android-native` job builds arm64 `libghostty_vt.so` and assembles debug AAR/APK | n/a                         | Optional `native-libs` workflow uploads multi-ABI `.so` + SUMS |

There is **no** committed binary. Honest redistributable terminal support requires either:

1. Building from the pin locally, or
2. Publishing checksummed libs/APK with the MIT notice (GitHub Release), or
3. Running the optional native CI workflow and attaching its artifacts.

## Release APK (sideload beta)

```bash
# 1) Native libs (arm64)
./scripts/build-libghostty.sh --abis arm64-v8a
./scripts/verify-libghostty.sh --release

# 2) APK + local artifact folder under dist-download/
./scripts/android-release-apk.sh
```

The release script runs `expo prebuild --clean` so package identity and
autolinking cannot retain a previous application ID, then asserts generated
`applicationId` / `versionName` / `versionCode` match `app/app.base.json`.
Use `--skip-prebuild` only after a clean prebuild for the current tracked config.

Or via package script after libs exist and `app/android` is prebuilt:

```bash
cd app
bun run build:apk
# → android/app/build/outputs/apk/release/app-release-arm64.apk
```

### Signing (secret-safe)

- **Never** commit keystores, passwords, or `.jks` files.
- Default local builds use the Expo/RN **debug** keystore for release variants (fine for personal sideload only).
- Optional release signing is wired by `withZenAndroidRelease` via **Gradle `System.getenv`** (not `-P` secrets on the command line):

```bash
export ZEN_ANDROID_KEYSTORE=/absolute/path/to/release.keystore
export ZEN_ANDROID_KEYSTORE_PASSWORD='…'   # do not commit; do not echo
export ZEN_ANDROID_KEY_ALIAS='…'
export ZEN_ANDROID_KEY_PASSWORD='…'
./scripts/android-release-apk.sh   # runs prebuild, refuses if plugin wiring missing
```

`android-release-apk.sh` aborts if generated `app/android/app/build.gradle` does not contain the env-based signing hook, and never prints secret values.

## Package identity

Canonical tracked identity is [`app/app.base.json`](../app/app.base.json) (loaded by [`app/app.config.js`](../app/app.config.js)):

| Field                 | Owner                                                                                                |
| --------------------- | ---------------------------------------------------------------------------------------------------- |
| `expo.version`        | [`app/app.base.json`](../app/app.base.json)                                                          |
| `android.package`     | [`app/app.base.json`](../app/app.base.json) (`com.daoleno.zen`)                                      |
| `android.versionCode` | [`app/app.base.json`](../app/app.base.json) (read the live integer; do not copy a stale table value) |

Verify with `./scripts/verify-release-identity.sh` (also `bun run release:identity`).

**Sideload note:** changing `android.package` from the old `com.anonymous.zen` means Android treats this as a different app. Uninstall the previous package before installing a `com.daoleno.zen` APK if both were installed on the same device.

Official release APKs are signed by the release pipeline. Local builds without signing env use the debug keystore and are only suitable for personal testing. Never commit `ZEN_ANDROID_*` secrets or local signing files.

### Staging (local, not a GitHub Release)

```bash
# Clean stage each run: top-level Linux binaries + legal notices + notes (no APK):
./scripts/stage-release.sh
# → dist-download/vVERSION/
#    zen-linux-amd64  zen-linux-arm64
#    LICENSE  NOTICE  TRADEMARKS.md  GHOSTTY-MIT.txt
#    RELEASE_NOTES.md  SHA256SUMS  identity.json

# Optional: include a prebuilt/signed APK path
./scripts/stage-release.sh --apk path/to/app-release.apk
```

Each release has versioned notes under `docs/releases/`; the release page includes the matching notes, certificate fingerprint, and sideload warnings.

`stage-release.sh` never creates tags or GitHub Releases. Publishing is a separate Brain/maintainer step.

### CI (GitHub Actions)

Signed arm64 APK and daemon binaries are built in parallel when a reviewed annotated beta tag is pushed by [`.github/workflows/release-artifacts.yml`](../.github/workflows/release-artifacts.yml). After gated aggregation and verification, the workflow publishes the matching GitHub prerelease. Manual dispatch is build-only by default and requires an explicit reviewed boolean for publication recovery. Required secret **names** and release preparation details: [ci-release.md](ci-release.md).

## Related docs

- [CI release pipeline](ci-release.md)
- [Connect and pair](connect-and-pair.md)
- [Third-party assets (Ghostty MIT)](third-party-assets.md)
- [Release blockers](release-blockers.md)
- [Troubleshooting](troubleshooting.md)
