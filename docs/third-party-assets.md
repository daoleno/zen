# Third-party assets

Inclusion does not imply endorsement. Prefer verifiable upstream license text over guesswork. Assets without defensible provenance are listed in [release-blockers.md](release-blockers.md) and `release-blockers.json`.

## Fonts (bundled under `app/assets/fonts/`)

| In-app file | Upstream | Evidence | License |
| --- | --- | --- | --- |
| `SourceHanSansSC-Regular.otf` | [adobe-fonts/source-han-sans](https://github.com/adobe-fonts/source-han-sans) | Name table: Adobe copyright + SIL OFL 1.1 URL (`nameID` 0/13/14) | SIL OFL 1.1 |
| `SourceHanSansSC-Medium.otf` | same | same | SIL OFL 1.1 |
| `MapleMono-CN-Regular.ttf` | [subframe7536/maple-font](https://github.com/subframe7536/maple-font) | Name table: Maple Mono Project Authors + SIL OFL 1.1; upstream `OFL.txt` | SIL OFL 1.1 |
| `MapleMono-CN-SemiBold.ttf` | same | same | SIL OFL 1.1 |
| `SarasaGothicSC-Regular.ttf` | [be5invis/Sarasa-Gothic](https://github.com/be5invis/Sarasa-Gothic) | Name table copyright (Renzhi Li / Inter / Adobe / Google); upstream `LICENSE` is SIL OFL 1.1 (bundled files omit `nameID` 13) | SIL OFL 1.1 (via upstream LICENSE + copyright) |
| `SarasaGothicSC-Bold.ttf` | same | same | SIL OFL 1.1 |
| `SarasaTermSC-Regular.ttf` | same | same | SIL OFL 1.1 |
| `SarasaTermSC-Bold.ttf` | same | same | SIL OFL 1.1 |

OFL redistribution still expects copyright/license notice availability to recipients; keep this file (and upstream LICENSE links) with source releases.

## Audio (bundled under `app/assets/audio/`)

| In-app file | Source | Author | License | Notes |
| --- | --- | --- | --- | --- |
| `mokugyo-hit-jono.m4a` | [Freesound 607215](https://freesound.org/people/jonopodmore/sounds/607215/) HQ preview | jonopodmore (Jono Podmore) | [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/) | Used by Quiet Mode mokugyo. |
| `meditation-ambient.m4a` | **unknown** | — | — | Used by Quiet Mode meditation bed. **Release blocker.** |
| `meditation-focus.m4a` | **unknown** | — | — | Present in tree; not referenced by current app code. **Release blocker.** |
| `meditation-unwind.m4a` | **unknown** | — | — | Present in tree; not referenced by current app code. **Release blocker.** |
| `mokugyo-hit.m4a` | **unknown** | — | — | Older hit sample; superseded by `mokugyo-hit-jono.m4a` in code. **Release blocker.** |

## Images / branding

| Path | Provenance | License notes |
| --- | --- | --- |
| `app/assets/branding/*` (logos, rings, SVGs) | First-party Zen artwork | Product assets; not third-party. |
| `app/assets/icon.png`, `splash-icon.png`, `favicon.png`, `android-icon-*.png` | First-party / derived from Zen branding | Product assets. |
| `app/assets/theme/sky-meadow-ambient.webp` | **unknown** | Used by `SkyNatureBackdrop`. **Release blocker.** |
| `app/assets/theme/moonlit-meadow-ambient.webp` | **unknown** | Used by `SkyNatureBackdrop`. **Release blocker.** |
| `app/assets/theme/meditation-*.webp` | **unknown** | Present in tree; not referenced by current Quiet Mode UI. **Release blocker.** |

## World Window videos (not bundled)

World Window streams Mixkit 720p MP4s from `assets.mixkit.co` using the curated catalog in `app/components/meditation/windowScenes.ts`. Scenes remain subject to the [Mixkit Free License](https://mixkit.co/license/#videoFree). Requires network; not an offline feature.

## Native dependency: Ghostty VT

| Artifact | Upstream | License | Notes |
| --- | --- | --- | --- |
| `libghostty_vt.so` (Android) | [ghostty-org/ghostty](https://github.com/ghostty-org/ghostty) | [MIT](https://github.com/ghostty-org/ghostty/blob/main/LICENSE) | Built via `scripts/build-libghostty.sh`; binaries are gitignored. Redistributing APKs that include the `.so` must preserve MIT notice. |

## npm / Go dependencies

Application and daemon library licenses are those of their respective packages (`bun.lock`, `daemon/go.mod`). This document focuses on **bundled media and vendored native binaries**, which are easy to miss in automated SCA.
