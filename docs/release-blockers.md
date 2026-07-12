# Release blockers

Machine-readable companion: [`release-blockers.json`](release-blockers.json).

These items block calling the tree an honest redistributable public beta until resolved or explicitly accepted by the maintainer. They are **not** invented attributions.

## Open blockers

### `license-pending`

- **Severity:** blocker
- **Summary:** No root `LICENSE` file; license choice is pending a maintainer decision (intentionally out of scope for the release-foundation docs slice).
- **Acceptance:** Commit a chosen SPDX license and link it from the README.

### `audio-provenance-unknown`

Bundled files without verified upstream license evidence:

| Path | Used by app? |
| --- | --- |
| `app/assets/audio/meditation-ambient.m4a` | yes (Quiet Mode meditation) |
| `app/assets/audio/meditation-focus.m4a` | no (orphaned in tree) |
| `app/assets/audio/meditation-unwind.m4a` | no (orphaned in tree) |
| `app/assets/audio/mokugyo-hit.m4a` | no (orphaned; code uses `mokugyo-hit-jono.m4a`) |

**Acceptance:** Replace with attributed sources, remove from the tree, or document defensible provenance with URLs.

### `theme-image-provenance-unknown`

| Path | Used by app? |
| --- | --- |
| `app/assets/theme/sky-meadow-ambient.webp` | yes (`SkyNatureBackdrop`) |
| `app/assets/theme/moonlit-meadow-ambient.webp` | yes (`SkyNatureBackdrop`) |
| `app/assets/theme/meditation-focus-dawn.webp` | no |
| `app/assets/theme/meditation-mokugyo-room.webp` | no |
| `app/assets/theme/meditation-sky-garden.webp` | no |
| `app/assets/theme/meditation-unwind-aurora.webp` | no |

**Acceptance:** Same as audio—prove, replace, or delete.

### `android-native-terminal-artifacts`

- **Summary:** `libghostty_vt.so` is required for the Android terminal and is gitignored; strangers cannot build terminal support from clone alone without Ghostty + Zig.
- **Acceptance:** Documented prebuilt APK/libs with MIT notice, or a reproducible CI artifact pipeline.

## Non-blockers recorded for honesty

- Fonts (Source Han Sans SC, Maple Mono CN, Sarasa Gothic/Term SC): upstream OFL evidence recorded in `third-party-assets.md`.
- `mokugyo-hit-jono.m4a`: CC0 Freesound 607215.
- Mixkit World Window: streamed, not bundled; Mixkit Free License.
- Ghostty: MIT; only redistribution of built `.so`/APK needs notice packaging.
