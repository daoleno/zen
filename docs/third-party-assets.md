# Third-party assets

Inclusion does not imply endorsement. Prefer verifiable upstream license text over guesswork. Assets without defensible provenance are listed in [release-blockers.md](release-blockers.md) and `release-blockers.json`.

## Fonts (bundled under `app/assets/fonts/`)

| In-app file                   | Upstream                                                                      | Evidence                                                                                                                      | License                                        |
| ----------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| `SourceHanSansSC-Regular.otf` | [adobe-fonts/source-han-sans](https://github.com/adobe-fonts/source-han-sans) | Name table: Adobe copyright + SIL OFL 1.1 URL (`nameID` 0/13/14)                                                              | SIL OFL 1.1                                    |
| `SourceHanSansSC-Medium.otf`  | same                                                                          | same                                                                                                                          | SIL OFL 1.1                                    |
| `MapleMono-CN-Regular.ttf`    | [subframe7536/maple-font](https://github.com/subframe7536/maple-font)         | Name table: Maple Mono Project Authors + SIL OFL 1.1; upstream `OFL.txt`                                                      | SIL OFL 1.1                                    |
| `MapleMono-CN-SemiBold.ttf`   | same                                                                          | same                                                                                                                          | SIL OFL 1.1                                    |
| `SarasaGothicSC-Regular.ttf`  | [be5invis/Sarasa-Gothic](https://github.com/be5invis/Sarasa-Gothic)           | Name table copyright (Renzhi Li / Inter / Adobe / Google); upstream `LICENSE` is SIL OFL 1.1 (bundled files omit `nameID` 13) | SIL OFL 1.1 (via upstream LICENSE + copyright) |
| `SarasaGothicSC-Bold.ttf`     | same                                                                          | same                                                                                                                          | SIL OFL 1.1                                    |
| `SarasaTermSC-Regular.ttf`    | same                                                                          | same                                                                                                                          | SIL OFL 1.1                                    |
| `SarasaTermSC-Bold.ttf`       | same                                                                          | same                                                                                                                          | SIL OFL 1.1                                    |

OFL redistribution still expects copyright/license notice availability to recipients; keep this file (and upstream LICENSE links) with source releases.

## Images / branding

| Path                                                                          | Provenance                              | License notes                                                              |
| ----------------------------------------------------------------------------- | --------------------------------------- | -------------------------------------------------------------------------- |
| `app/assets/branding/*` (logos, rings, SVGs)                                  | First-party Zen artwork                 | Product assets; not third-party.                                           |
| `app/assets/icon.png`, `splash-icon.png`, `favicon.png`, `android-icon-*.png` | First-party / derived from Zen branding | Product assets.                                                            |
| Shell sky backdrop                                                            | First-party code                        | `SkyNatureBackdrop` uses only `LinearGradient` colors (no bundled raster). |
| `app/assets/theme/`                                                           | No bundled rasters                      | README only; do not add unattributed stock.                                |

Removed from the tree (unknown provenance): former `sky-meadow-ambient.webp` and `moonlit-meadow-ambient.webp`.

## Native dependency: Ghostty VT

| Artifact                                                       | Upstream                                                      | License                                                         | Notes                                                                                                                                                                                                                                                              |
| -------------------------------------------------------------- | ------------------------------------------------------------- | --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `libghostty_vt.so` (Android) / `GhosttyVt.xcframework` (Apple) | [ghostty-org/ghostty](https://github.com/ghostty-org/ghostty) | [MIT](https://github.com/ghostty-org/ghostty/blob/main/LICENSE) | Built from the immutable pin in `app/modules/zen-terminal-vt/native.lock.json` via `scripts/build-libghostty.sh` (Android) and `scripts/build-libghostty-ios.sh` (iOS). Binaries are gitignored; both mobile bridges link the generated artifacts.                 |
| Notice source                                                  | same                                                          | MIT                                                             | `app/assets/notices/GHOSTTY-MIT.txt` embeds the pinned Ghostty `LICENSE` body (`license_sha256` in `native.lock.json`). Module pointer: `NOTICE.Ghostty`.                                                                                                          |
| Notice in APK                                                  | same                                                          | MIT                                                             | Expo plugin `withZenAndroidRelease` copies the notice to `android/app/src/main/assets/notices/GHOSTTY-MIT.txt` → APK path `assets/notices/GHOSTTY-MIT.txt`. Verify: `./scripts/verify-apk-notice.sh <apk>`.                                                        |
| Notice in iOS app / IPA                                        | same                                                          | MIT                                                             | Expo plugin `withZenIOSBuild` copies the notice into the Xcode app resources → bundle path `GHOSTTY-MIT.txt` at the app root (Xcode flattens ordinary files; see `native.lock.json` `ios.notice_bundle_path`). Verify: `./scripts/verify-ios-artifact.sh simulator | ipa <artifact>`. |

**ABI contract:** only `arm64-v8a` (device/sideload) and `x86_64` (emulator). See [android.md](android.md).

**Redistribution:** APKs and iOS app bundles/IPAs must embed the MIT notice (paths above). Prebuilt `.so` / XCFramework archives should include an adjacent `GHOSTTY-MIT.txt` (written by the platform build scripts).

## npm / Go dependencies

Application and daemon library licenses are those of their respective packages (`bun.lock`, `daemon/go.mod`). This document focuses on **bundled fonts and vendored native binaries**, which are easy to miss in automated SCA.
