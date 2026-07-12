# Android app

## Honest scope

| Claim | Status |
| --- | --- |
| Pair, reconnect, session list, structured Chat | Intended beta surface |
| Native terminal (Ghostty VT) | Android only; requires built `libghostty_vt.so` for **arm64-v8a** (devices) and optionally **x86_64** (emulator) |
| iOS | Not a supported distribution target in this beta |
| Expo Go | Can paste/scan pairing links; custom `zen://` deep links need a dev build/APK |
| Play Store | Not part of this release foundation |

## Architecture / ABI contract

Canonical machine-readable contract:

[`app/modules/zen-terminal-vt/native.lock.json`](../app/modules/zen-terminal-vt/native.lock.json)

| Android ABI | Zig target | Role | Sideload required |
| --- | --- | --- | --- |
| `arm64-v8a` | `aarch64-linux-android` | Physical devices / release APK | **Yes** |
| `x86_64` | `x86_64-linux-android` | Emulator / host-side native debug | No |

**Unsupported (invalid for zen terminal):** `armeabi-v7a`, `x86`, and any other ABI.

Invariants:

1. Only the ABIs listed above may appear under `app/modules/zen-terminal-vt/libs/android/`.
2. Release sideload APKs are **arm64-v8a only** (`-PreactNativeArchitectures=arm64-v8a`).
3. `libghostty_vt.so` and Ghostty C headers are **gitignored**. Strangers do not get terminal binaries from a bare clone until they build or consume release artifacts.
4. Toolchain pins live in `native.lock.json` (Zig version + Ghostty git commit). Dirty Ghostty trees are refused by default.
5. Redistributed APKs must embed Ghostty MIT at `assets/notices/GHOSTTY-MIT.txt` (source: `app/assets/notices/GHOSTTY-MIT.txt`), packaged by Expo plugin `app/plugins/withZenAndroidRelease.js` during prebuild. Verify with `./scripts/verify-apk-notice.sh <apk>`.
6. Release-grade native builds require a **proven** Ghostty git commit equal to the pin (`release_grade: true` in `build-manifest.json`). Dirty or no-git trees may build only with explicit developer overrides and **fail** `./scripts/verify-libghostty.sh --release`.

Module wiring (source of truth for packaging):

- `app/modules/zen-terminal-vt/android/build.gradle` — `abiFilters 'arm64-v8a', 'x86_64'`
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

1. Run `zen pair https://your-origin` on the host.
2. Open Settings in the app.
3. Paste the `zen://...` link, scan the QR, or import a QR photo.

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

Build outputs (all gitignored except the tracked lock/notice):

| Path | Purpose |
| --- | --- |
| `libs/android/<abi>/libghostty_vt.so` | Imported by CMake / jniLibs |
| `libs/android/SHA256SUMS` | Per-ABI digests for release notes |
| `libs/android/build-manifest.json` | Pin + zig version + digests |
| `libs/android/GHOSTTY-MIT.txt` | Copy of MIT notice for tarballs |
| `android/src/main/cpp/ghostty/` | Headers for JNI bridge |

### Clean-clone expectations

| Path | Bare `git clone` | After `build-libghostty.sh` | Maintainer release artifact |
| --- | --- | --- | --- |
| Chat / pairing JS | Works | Works | Works |
| Native terminal | Unavailable | Works (local) | Works if APK includes arm64 `.so` |
| CI default (`ci.yml`) | Contract check only | n/a | Optional `native-libs` workflow uploads `.so` + SUMS |

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

| Field | Value |
| --- | --- |
| `expo.version` | `0.1.0-beta.1` |
| `android.package` | `com.daoleno.zen` |
| `android.versionCode` | `1` |

Verify with `./scripts/verify-release-identity.sh` (also `bun run release:identity`).

**Sideload note:** changing `android.package` from the old `com.anonymous.zen` means Android treats this as a different app. Uninstall the previous package before installing a `com.daoleno.zen` APK if both were installed on the same device.

Sideload APKs remain **maintainer/beta artifacts** until a maintainer publishes a signed build. Agents and CI must not read `~/.zen/release-keys` or commit `ZEN_ANDROID_*` secrets. Local/default release builds without signing env use the debug keystore (personal sideload only).

### Staging (local, not a GitHub Release)

```bash
# Clean stage each run: top-level Linux binaries + legal notices + notes (no APK):
./scripts/stage-release.sh
# → dist-download/v0.1.0-beta.1/
#    zen-linux-amd64  zen-linux-arm64
#    LICENSE  NOTICE  TRADEMARKS.md  GHOSTTY-MIT.txt
#    RELEASE_NOTES.md  SHA256SUMS  identity.json

# Optional: include a prebuilt/signed APK path
./scripts/stage-release.sh --apk path/to/app-release.apk
```

Release notes template: [`docs/releases/v0.1.0-beta.1.md`](releases/v0.1.0-beta.1.md) (includes official APK certificate fingerprint and sideload warnings).

`stage-release.sh` never creates tags or GitHub Releases. Publishing is a separate Brain/maintainer step.

### CI (GitHub Actions)

Signed arm64 APK + Linux binaries are built by [`.github/workflows/release-artifacts.yml`](../.github/workflows/release-artifacts.yml). Tag pushes build/verify only; asset upload to an **existing** prerelease requires explicit `workflow_dispatch` with `publish=true`. Required secret **names** and dispatch examples: [ci-release.md](ci-release.md).

## Related docs

- [CI release pipeline](ci-release.md)
- [Connect and pair](connect-and-pair.md)
- [Third-party assets (Ghostty MIT)](third-party-assets.md)
- [Release blockers](release-blockers.md)
- [Troubleshooting](troubleshooting.md)
