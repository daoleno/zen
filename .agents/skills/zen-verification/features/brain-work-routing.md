# Brain work routing

Brain is the canonical owner of current work, executor routing, and discoverable playbooks. This feature verifies that a Worker can read that contract from the live daemon without changing it.

## Sub-features

- `brain-context` returns the host and delegated executor identities.
- `brain-playbooks` returns the discoverable Brain workflow catalog.
- `brain-privacy` emits a compact report instead of persisting raw current work.

## How to get to it (user POV)

- Run `scripts/verify-zen-orchestration.sh --json --state-dir /absolute/path/to/existing-zen-state` from the repository root.
- Read the `brain_context` and `brain_playbooks` entries in the report.

## Driving it with the Zen CLI

Preconditions:

- The exact Zen daemon is running and passes `zen doctor --json`.
- The checkout contains the source and test anchors in `features/manifest.json`.

- **Context.** Run the lever. The report contains a passing `brain_context` runtime check, a separate runtime daemon identity, a host executor name, a delegated executor name, and a Worker count.
- **Playbooks.** Read the same report. The report contains a passing `brain_playbooks` preflight and the `brain-flows` catalog entry. This does not prove Skill loading.
- **Privacy.** Inspect the report. It contains no raw `current`, transcript, or Work objective field.

## Gotchas

- A healthy daemon with stale source is not a pass. The source and test checks must also pass.
- The report does not prove provider quality or a mobile or desktop client flow.
- A missing daemon is a prerequisite failure. Do not start a second server as part of this read-only check.
