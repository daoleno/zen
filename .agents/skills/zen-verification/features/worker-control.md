# Worker control

Zen exposes visible Worker identities through the canonical control socket. This feature verifies the read path that a Brain or Worker uses to inspect lifecycle ownership.

## Sub-features

- `worker-list` returns stable Worker session IDs and statuses.
- `worker-ownership` exposes delegated ownership without selecting a provider.
- `worker-count` reports the observed list size without saving session content.

## How to get to it (user POV)

- Run `scripts/verify-zen-orchestration.sh --json --state-dir /absolute/path/to/existing-zen-state` from the repository root.
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

## DSH and service lifetime evidence

The source map includes the additive DSH executor, exact native Session bridge,
Zstandard event projection and Services tunnel process owner. `worker_list` proves
only the canonical inventory read, not these mutations or provider output.

Use `daemon/work/dsh_conversation_test.go` for source identity, packed streaming,
tool correlation, attachment confinement and exclusive-owner regressions. A bounded
native DSH smoke must use private state and record the exact Session identity;
never infer success from a launch option alone or change saved model choices.

`daemon/watcher/service_tunnels_test.go` checks HTTP/WebSocket proxy behavior,
origin/listener replacement, stop isolation and Linux parent-death cleanup. Its
public fixture test is explicit opt-in and always stops its own tunnel. A URL or
native Cloudflare connection confirmation alone is not public reachability proof.
