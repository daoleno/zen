# Verify Zen control-plane changes

Zen's reusable verification entry point is the project skill at `.agents/skills/zen-verification/SKILL.md`. The skill is discovered through the Codex project root that the daemon's Skills adapter defines. Its feature map binds each named user path to source files, tests, and a live CLI check.

## Run the control-plane check

Run the check from the repository root:

```sh
scripts/verify-zen-orchestration.sh --json
```

The check validates the feature manifest and then runs these read-only commands against the canonical daemon:

- `zen doctor --json`
- `zen brain playbooks --json`
- `zen brain context --json`
- `zen worker list --json`

The output is compact JSON with the result, source and test anchor counts, runtime check IDs, host and delegated executor names, and Worker count. Raw Brain, Work, transcript, and provider payloads are not persisted.

To target an isolated daemon, pass its exact state directory:

```sh
scripts/verify-zen-orchestration.sh --json --state-dir /absolute/path/to/zen-state
```

The check does not start a server, call an AI provider, create a Work item, or touch desktop input. A missing or unhealthy daemon is a failed prerequisite. Do not start a second daemon to make the check pass.

## Keep the map current

Update `features/manifest.json` and the matching feature file when a Brain, Worker, or project skill path changes. Add a source anchor and a test anchor for every new feature. Run the check after the source change.

The check proves the listed read-only control-plane paths at the daemon revision it reaches. It does not prove provider quality, mobile behavior, desktop behavior, or physical user interaction. Use an owned inert fixture and a feature-specific proof for those surfaces.

## Failure handling

The check exits with status `1` when an anchor or runtime check fails. It exits with status `2` for invalid arguments or an invalid root. The report names each failed check and does not turn a partial result into a pass.
