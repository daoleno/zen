---
name: zen-verification
description: "Verify Zen's provider-neutral Brain and Worker control plane from the real CLI. Use when a change needs a bounded runtime preflight tied to source and tests."
disable-model-invocation: false
---

# Zen verification

Use this skill for changes to Brain routing, Worker lifecycle, executor selection, or project skill loading. The verification surface is the Zen CLI and the canonical running daemon. It does not call an AI provider, start a desktop client, drive a personal display, or mutate Work, Session, or skill state.

Read `features/README.md` before choosing a feature. Each feature file names the user path, runtime command, source anchors, test anchors, and failure cases. Keep the feature map aligned with the current code.

## Launch

This skill does not start Zen. Every run must pass an explicit, existing, non-symlink state directory. That directory must belong to the daemon being checked. This prevents `zen doctor` from creating a state directory before its write and tmux probes run.

Run the lever from the repository root:

```sh
scripts/verify-zen-orchestration.sh --json --state-dir /absolute/path/to/existing-zen-state
```

Pass `--state-dir <absolute-path>` only when the daemon was started with that exact state directory. Do not omit the flag, guess a state directory, or attach to another user's daemon.

## Doctor

The lever runs `zen doctor --json` first, after it confirms that the explicit state directory already exists. `zen doctor` is not read-only. It writes a state-directory probe, starts and stops an ephemeral tmux probe, and can inspect executor binaries. The script does not claim that tmux or executor effects are isolated by the state directory. A doctor failure ends the run before Brain or Worker output is interpreted.

The lever captures command output only in an unpredictable `mktemp` directory with mode `0700`, file mode `0600`, and a bounded lifetime. It projects only readiness, source identity, runtime identity, executor IDs, and Worker count into its output. It never writes raw payloads to a predictable path or user-facing failure report.

## Drive

Run the real control-plane commands through the lever after the doctor preflight:

- `zen brain playbooks --json` checks that Brain's workflow catalog is available. It does not prove project Skill discovery.
- `zen brain context --json` proves the current host and delegated executor contract is readable.
- `zen worker list --json` proves that the Worker control path returns canonical session identities.

The command output is a compact JSON report. It separates source checks from runtime checks and includes feature IDs, source and test anchor counts, check IDs, source revision, daemon identity, executor IDs, and Worker count. It never treats a mock provider, a unit test, a playbook catalog, or a helper-only output as proof of project Skill loading or a live product flow.

## Evidence

Capture the command's stdout, stderr, and exit code in the review record. The JSON report is the primary artifact. A passing report proves the listed source and control-plane checks at separately reported source and daemon identities. It does not prove that those identities match.

For a mutation or provider flow, add a feature-specific run that uses an owned inert fixture and records the exact Work, Session, or resource identity. Do not extend this skill by using broad process cleanup, personal desktop input, real provider calls, or shared user state.

The source and test anchors are completeness checks. They do not prove behavior by themselves. The runtime checks must also pass.

## Cleanup

The default run creates a private temporary directory and removes only the files and directory it created. It may trigger the bounded prerequisite probes performed by `zen doctor` inside the explicit state directory. The tmux and executor probes are not claimed to be isolated by this script. If an inert fixture is used by a feature-specific extension, the extension owns its exact temporary directory and child process. Stop only the recorded process or session identity. Preserve evidence after teardown.

## Helpers

The executable helper is `scripts/verify-zen-orchestration.sh`. Use `--help` for the short interface. Use `--json` for agent assertions. Use `--root <absolute-path>` to validate a checkout other than the current repository. Use `--zen-bin <absolute-path>` only with an owned inert fixture. Use `--timeout-seconds <1-300>` to bound each command.

## Maintenance

When Brain, Worker, or skill-discovery paths change, update the manifest and the matching feature file in the same change. Run the lever after the source wave. A missing source or test anchor is drift and must fail the check. A daemon failure is a runtime prerequisite failure, not a successful verification.
