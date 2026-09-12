---
name: zen-verification
description: "Verify Zen's provider-neutral Brain and Worker control plane from the real CLI. Use when a change needs an owned, read-only runtime proof tied to source and tests."
disable-model-invocation: false
---

# Zen verification

Use this skill for changes to Brain routing, Worker lifecycle, executor selection, or project skill discovery. The verification surface is the Zen CLI and the canonical running daemon. It does not call an AI provider, start a desktop client, drive a personal display, or mutate Work, Session, or skill state.

Read `features/README.md` before choosing a feature. Each feature file names the user path, runtime command, source anchors, test anchors, and failure cases. Keep the feature map aligned with the current code.

## Launch

This skill does not start Zen. Use the repository's existing daemon lifecycle. A verification run must target the daemon already owned by the current Zen session or an explicitly isolated daemon state directory.

Run the lever from the repository root:

```sh
scripts/verify-zen-orchestration.sh --json
```

Pass `--state-dir <absolute-path>` only when the daemon was started with that exact state directory. Do not guess a state directory or attach to another user's daemon.

## Doctor

The lever runs `zen doctor --json` first. It requires a supported platform, a writable state directory, a functional tmux transport, and a running Zen daemon. A doctor failure ends the run before Brain or Worker output is interpreted.

The lever records only readiness and stable ownership fields. It does not save raw doctor, Brain, or Worker payloads because those payloads can contain private current work and session text.

## Drive

Run the real read-only commands through the lever:

- `zen brain playbooks --json` proves that Brain's discoverable workflow catalog is available.
- `zen brain context --json` proves the current host and delegated executor contract is readable.
- `zen worker list --json` proves that the Worker control path returns canonical session identities.

The command output is a compact JSON report. It includes feature IDs, source and test paths, runtime check IDs, executor names, and Worker count. It never treats a mock provider, a unit test, or a helper-only output as proof of a live product flow.

## Evidence

Capture the command's stdout, stderr, and exit code in the review record. The JSON report is the primary artifact. A passing report proves only the listed read-only control-plane paths at that daemon revision.

For a mutation or provider flow, add a feature-specific run that uses an owned inert fixture and records the exact Work, Session, or resource identity. Do not extend this skill by using broad process cleanup, personal desktop input, real provider calls, or shared user state.

The source and test anchors are completeness checks. They do not prove behavior by themselves. The runtime checks must also pass.

## Cleanup

The default run creates no resources and needs no teardown. If an isolated daemon or fixture is used by a feature-specific extension, the extension owns its exact temporary directory and child process. Stop only the recorded process or session identity. Preserve evidence after teardown.

## Helpers

The executable helper is `scripts/verify-zen-orchestration.sh`. Use `--help` for the short interface. Use `--json` for agent assertions. Use `--root <absolute-path>` to validate a checkout other than the current repository.

## Maintenance

When Brain, Worker, or skill-discovery paths change, update the manifest and the matching feature file in the same change. Run the lever after the source wave. A missing source or test anchor is drift and must fail the check. A daemon failure is a runtime prerequisite failure, not a successful verification.
