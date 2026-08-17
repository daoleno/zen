# Skills Manager V5 Device QA

Date: 2026-08-18

Environment:

- Production Android release on Android 35 x86_64 emulator
- Narrow viewport: 1080 x 2400 at 420 dpi
- Wide viewport: 1200 x 1600 logical pixels at 160 dpi
- Real daemon at `10.0.2.2:9876`
- Installed x86_64 APK SHA-256: `2954cf941bf17c8ee1d006932bf6430b6812fe68a3ecf4d43bda35da4d6fe7ce`
- Final arm64 APK SHA-256: `76a74059b35c7fc27fd3d6f092a8954610152bfa35d3fb8a3ecb320bf99cbfd1`

## Evidence

- `default-list.png`: automatically discovered Skills, compact row deletion actions, duplicate count, and no deletion action for the built-in fixture.
- `single-confirm.png`: exact single-copy confirmation naming the Skill, Codex, its simplified location, and permanence.
- `multi-selector.png`: two same-name copies selectable independently from Codex and Pi locations.
- `multi-confirm.png`: the selected Codex copy produces a Codex-only exact-copy confirmation.
- `readonly-detail.png`: built-in copy shows one provider-owned read-only explanation and no deletion action.
- `detail-bottom.png`: mobile detail sheet preserves Overview, Files, Available to, and an unobscured bottom Delete action.
- `wide-inspector.png`: wide list and inspector remain visible together without overlap.
- `post-delete.png`: successful UI deletion notice, removed logical row, retained duplicate and read-only neighbors.
- `post-delete-wide.png`: retained evidence of the first-pass transport error described below; it is not the final success state.
- `current.png`: final narrow-layout device state after all QA fixtures were cleaned.
- `live-delete-proof.json`: real daemon protocol proof for temporary Codex, Pi, and Codex/Pi shared-root copies.

## Exact Delete Proof

The production app deleted only the task-owned `aa-zen-v5-single` Codex copy. Its root disappeared, its logical row disappeared after refreshed inventory, and the success notice was displayed. SHA-256 hashes for both `ab-zen-v5-multi` copies and `ac-zen-v5-readonly` were identical before and after deletion.

The first device pass exposed a stale app transport assumption: the daemon's successful `skills_mutation_result` is a top-level wire payload, while the app tried to parse a nested `result`. `post-delete-wide.png` retains that observed failure state. The transport parser and regression test were corrected, a new production APK was installed, and the final successful path is captured in `post-delete.png`.

All task-owned Skill fixtures, the temporary pairing token, and the temporary paired emulator device were removed after capture.
