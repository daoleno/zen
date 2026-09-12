# Worker control

Zen exposes visible Worker identities through the canonical control socket. This feature verifies the read path that a Brain or Worker uses to inspect lifecycle ownership.

## Sub-features

- `worker-list` returns stable Worker session IDs and statuses.
- `worker-ownership` exposes delegated ownership without selecting a provider.
- `worker-count` reports the observed list size without saving session content.

## How to get to it (user POV)

- Run `scripts/verify-zen-orchestration.sh --json` from the repository root.
- Read the `worker_list` entry and its `runtime.worker_count` value.

## Driving it with the Zen CLI

Preconditions:

- `zen doctor --json` passes for the exact daemon state.
- The control socket is owned by that daemon.

- **List.** Run the lever. The report contains `worker_list: pass` and a numeric Worker count.
- **Identity.** Inspect the runtime summary. Worker IDs are counted from the canonical response and are not recreated from process names.
- **Failure.** Point `--state-dir` at a missing or unrelated state directory. The lever exits nonzero and reports the failed prerequisite instead of claiming a pass.

## Gotchas

- A Worker status is not a completion decision. Brain Work and Event state remain authoritative.
- Do not use `pkill`, `killall`, or process-name matching to clean up a verification run.
- An empty list can be valid. The control path must pass even when no Worker is active.
