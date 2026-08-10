# Release blockers

For the iOS build, signing, artifact, and Apple distribution gates, see [iOS CI and release automation](ios-ci-release.md).

Machine-readable companion: [`release-blockers.json`](release-blockers.json).

These items block calling the tree an honest redistributable public beta until resolved or explicitly accepted by the maintainer. They are **not** invented attributions.

## Open blockers

### `android-native-terminal-artifacts`

- **Summary:** `libghostty_vt.so` is required for the Android terminal and is gitignored; a bare clone still has no terminal binaries until build or release artifacts exist.
- **Acceptance:** Documented prebuilt APK/libs with MIT notice, or a reproducible CI artifact pipeline.
- **Pipeline status (partial):** pin + ABI + Zig download SHA-256 pins in `native.lock.json`; proven-git release-grade builds; Expo plugin packages MIT into APK assets; env-based signing hook; CI contract job + optional `native-libs` workflow (checksum-verified Zig). **Still blocking** until a maintainer publishes checksummed arm64 libs/APK (or green `native-libs` artifacts on a release) so strangers are not forced to install Zig + Ghostty.
- **User/CI commands:** `./scripts/verify-libghostty.sh --contract`; `./scripts/build-libghostty.sh` then `./scripts/verify-libghostty.sh --release`; APK `./scripts/android-release-apk.sh` + `./scripts/verify-apk-notice.sh <apk>`.

### `ios-distribution-artifacts`

- **Summary:** The iOS source build and Simulator runtime path work. `GhosttyVt.xcframework` remains generated/gitignored. A signed Preview IPA can be uploaded to App Store Connect via the protected release workflow, but that is not the same as public TestFlight/App Store installability.
- **Acceptance:** Reproducible CI produces and verifies the pinned XCFramework, packages the Ghostty MIT notice into the app bundle, archives/signs the app, and publishes a supported installation path with checksummed artifacts where applicable.
- **Pipeline status (partial):** arm64 device + Apple Silicon Simulator build script, Zig/Ghostty pins, checksums, build manifest, CocoaPods bridge, Expo config (including app-bundle notice packaging), macOS unsigned CI, and credential-gated archive/IPA/TestFlight automation exist. Preview IPA upload and App Store Connect presence do **not** by themselves clear this blocker. **Still blocking** for general end-user distribution until Apple Beta App Review / public TestFlight or App Store delivery is actually installable by intended testers, physical-device acceptance is recorded, and the supported channel is published.
- **User/CI commands:** `bun run native:build:ios`; `bun run native:verify:ios`; Expo prebuild/Pods; the `CI` macOS job; or a protected manual `iOS signed release` dispatch.

## Resolved (media provenance)

### `theme-image-provenance-unknown` (resolved)

- All unknown `app/assets/theme/*.webp` rasters removed.
- `SkyNatureBackdrop` is first-party gradients only (no stock images).

## Non-blockers recorded for honesty

- Fonts (Source Han Sans SC, Maple Mono CN, Sarasa Gothic/Term SC): upstream OFL evidence recorded in `third-party-assets.md`.
- Ghostty: MIT; redistribution of built `.so`/APK and iOS app/IPA needs notice packaging (Android + iOS verifiers).
