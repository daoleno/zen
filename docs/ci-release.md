# CI release pipeline (signed Android + daemons)

Automated build lives in [`.github/workflows/release-artifacts.yml`](../.github/workflows/release-artifacts.yml).

The separate [`native-libs.yml`](../.github/workflows/native-libs.yml) workflow builds the same pinned `libghostty-vt` C ABI for Android on Linux and as an iOS device/simulator XCFramework on macOS. Normal CI compiles and links an unsigned iOS Simulator app on macOS. Signed iOS archive/export and optional TestFlight upload are isolated in [`ios-release.yml`](../.github/workflows/ios-release.yml); see [iOS CI and release automation](ios-ci-release.md).

## What it does

| Step | Published prerelease or manual build dispatch | Published prerelease only |
| --- | --- | --- |
| Checkout selected ref | yes | — |
| `verify-release-identity.sh` | yes | — |
| Linux amd64/arm64 + macOS arm64 daemon archives | yes | — |
| Build release-grade `libghostty` arm64 | yes | — |
| Clean Expo prebuild + signed arm64 APK | yes | — |
| Verify package / version / ABI / Ghostty notice / cert fingerprint | yes | — |
| Stage tree + checksums + signed updater manifest | yes | — |
| Upload workflow artifact | yes | — |
| Upload/replace assets on **existing** GitHub prerelease | **no** | yes |

Publishing a GitHub prerelease is the one canonical build-and-publish event. It checks out the immutable tag, proves the tag version and SHA, builds once, and attaches the verified assets. A tag push alone does nothing, eliminating the former duplicate tag-push build plus manual publish rebuild. Manual dispatch is deliberately artifact-only. The workflow never creates tags/releases, converts drafts, or publishes from an ordinary branch push.

## Repository secrets (names only)

Configure under GitHub → Settings → Secrets and variables → Actions. Values are **not** documented and must never be committed or printed in logs.

| Secret name | Purpose |
| --- | --- |
| `ZEN_ANDROID_KEYSTORE_BASE64` | Base64 encoding of the PKCS#12 (or JKS) release keystore binary |
| `ZEN_ANDROID_KEYSTORE_PASSWORD` | Keystore password |
| `ZEN_ANDROID_KEY_ALIAS` | Key alias inside the keystore |
| `ZEN_ANDROID_KEY_PASSWORD` | Key password |
| `ZEN_UPDATE_SIGNING_KEY_BASE64` | Base64 encoding of the Ed25519 private PEM matching `release/zen-update-public-key.pem` |

Runtime mapping (CI materializes the file, mode `0600`, then shreds it):

| CI env | Source |
| --- | --- |
| `ZEN_ANDROID_KEYSTORE` | Temp path from `scripts/materialize-android-keystore.sh` |
| `ZEN_ANDROID_KEYSTORE_PASSWORD` | secret of same name |
| `ZEN_ANDROID_KEY_ALIAS` | secret of same name |
| `ZEN_ANDROID_KEY_PASSWORD` | secret of same name |

Local maintainer builds continue to use a filesystem path via `ZEN_ANDROID_KEYSTORE` (see [android.md](android.md)); agents must not read `~/.zen/release-keys`.

## Public certificate identity

Release APKs must match the fingerprint published in the current version's tracked release notes (public metadata, not a secret):

```
C2:FC:5B:09:B3:86:92:EE:70:59:71:1F:E7:ED:B8:79:4C:E3:65:FE:1C:7A:06:AB:95:4E:5D:D1:BD:CD:A4:FD
```

`scripts/verify-apk-release.sh` enforces this after the build.

## workflow_dispatch inputs

| Input | Type | Default | Meaning |
| --- | --- | --- | --- |
| `ref` | string | empty | Git tag/branch/SHA to build; empty = the branch/tag selected in the Actions UI |

Permissions: build job `contents: read`; publish job `contents: write` only for the published-prerelease event.

## Build and publish paths

Prerequisites:

1. The release tag already exists and points at the reviewed commit.
2. The matching draft is explicitly published as a **prerelease** on GitHub; that publication event is the canonical build/publish request.
3. The four `ZEN_ANDROID_*` secrets above are configured on the repository.
4. `ZEN_UPDATE_SIGNING_KEY_BASE64` is configured with the updater manifest key.

Optional build-only validation (workflow artifact; never mutates the release):

```bash
gh workflow run release-artifacts.yml \
  --ref main \
  -f ref=vX.Y.Z
```

To publish, use GitHub's explicit draft-to-prerelease publication operation. Do not dispatch a second build for the same tag. The workflow refuses a final release, draft, version mismatch, or tag/SHA mismatch before building and uses `gh release upload --clobber` only on the already-published prerelease.

Watch:

```bash
gh run list --workflow=release-artifacts.yml --limit 5
gh run watch
```

## Staged asset names

Under `dist-download/v<version>/` (and the workflow artifact):

- `zen-linux-amd64.tar.gz`
- `zen-linux-arm64.tar.gz`
- `zen-darwin-arm64.tar.gz`
- `zen-android-arm64-v<version>.apk`
- `SHA256SUMS`
- `release-manifest.json`
- `release-manifest.json.sig`

Daemon archives contain `zen`, `LICENSE`, `NOTICE`, and `TRADEMARKS.md`. Release notes stay in the GitHub Release body; Android third-party notices remain embedded in the APK. This keeps the download list focused without dropping required attribution.

## Local helper scripts

| Script | Role |
| --- | --- |
| `scripts/materialize-android-keystore.sh` | Base64 → temp keystore file (0600); prints path only |
| `scripts/verify-apk-release.sh` | Package/version/ABI/notice/certificate checks |
| `scripts/android-release-apk.sh` | Clean prebuild + signed assembleRelease |
| `scripts/stage-release.sh` | Deterministic stage tree |
| `scripts/sign-release-manifest.sh` | Validate the updater key and sign the exact manifest bytes |
| `scripts/verify-release-identity.sh` | Tracked identity + stage layout |

## Safety

- Secrets never appear in docs, commits, or intentional log lines.
- The updater accepts one public release stream, including prereleases by semantic-version precedence; it has no channel setting.
- The detached Ed25519 signature authenticates manifest version, platform mapping, archive size, and SHA-256 before download installation.
- CI does not read developer home directories (including `~/.zen/release-keys`).
- Manual dispatch cannot publish, while a published-prerelease event refuses final releases, drafts, version mismatches, and tag/SHA mismatches.
- Gradle caches only dependency/build state under the runner's Gradle home. Zig/Ghostty source and unsigned native-output keys include the native lock and build/verification inputs; every cache hit still runs release-grade manifest, checksum, ABI, and notice verification.
- No cache path includes a keystore, updater signing key, signed APK, or staged release output.
- Pinned Zig archives are SHA-256 verified on both cache misses and hits and extracted under `$RUNNER_TEMP/zen-tools` (not `/usr/local`). No `sudo` or unverified installers.
