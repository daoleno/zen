# Verify Zen control-plane changes

Zen's reusable verification entry point is the project skill at `.agents/skills/zen-verification/SKILL.md`. Zen's native Skill loaders and shared inventory discover this project root for supported local agents, including Pi. Its feature map binds each named user path to source files, tests, and a separately classified source or runtime check.

## Run the control-plane check

Run the check from the repository root with the exact existing state directory of the daemon you intend to check:

```sh
scripts/verify-zen-orchestration.sh --json --state-dir /absolute/path/to/existing-zen-state
```

The check validates the feature manifest and then runs these bounded commands against the canonical daemon:

- `zen doctor --json`
- `zen brain playbooks --json`
- `zen brain context --json`
- `zen worker list --json`

The output is compact JSON with separate source and runtime identities, feature IDs, source and test anchor counts, check IDs, host and delegated executor IDs, and Worker count. Raw Brain, Work, transcript, and provider payloads are held only in an unpredictable private `0700` directory with `0600` files during the bounded run, then removed by exact-file cleanup.

To target a daemon explicitly, pass its exact state directory:

```sh
scripts/verify-zen-orchestration.sh --json --state-dir /absolute/path/to/zen-state
```

The check does not start a server, call an AI provider, create a Work item, or touch desktop input. `zen doctor` is not read-only. It may write its state probe and start an ephemeral tmux probe. Executor probes may have their own side effects. The script rejects an absent or symlink state directory before invoking doctor. The script does not claim that those tmux or executor probes are isolated by the state directory. Do not start a second daemon to make the check pass.

## Keep the map current

Update `features/manifest.json` and the matching feature file when a Brain, Worker, or project skill path changes. Add a source anchor and a test anchor for every new feature. Run the check after the source change.

The check proves the listed source and control-plane checks at separately reported identities. It does not detect stale source, prove that source and daemon revisions match, prove native Skill loading, or prove provider quality, mobile behavior, desktop behavior, or physical user interaction. Use an owned inert fixture and a feature-specific proof for those surfaces.

## Failure handling

The check exits with status `1` when an anchor or check fails. It exits with status `2` for invalid arguments or an invalid root or state directory. Each subprocess has a bounded timeout. The report names each failed check and does not turn a partial result into a pass.

The lever uses GNU `timeout` without `--foreground`. Each command runs through an owned wrapper that forwards interrupt and termination signals to its exact child, and the parent waits for that wrapper. This keeps child probes inside timeout's process group without guessing a PGID.

Run the committed inert regressions before changing this lever:

```sh
scripts/tests/test_verify_zen_orchestration.sh
```

The regressions use an owned process group and assert that a child process is no longer running after timeout and signal cleanup. They do not use process-name cleanup or a global supervisor.
