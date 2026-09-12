# Zen verification map

This map describes the read-only control-plane paths that a Worker can verify without invoking a provider or touching personal desktop state. Read the matching feature file before running the lever.

## Baseline

- Run from a Zen checkout with `scripts/verify-zen-orchestration.sh` present.
- Pass the exact existing, non-symlink `--state-dir` for the daemon being checked.
- Run `zen doctor --json` before reading Brain or Worker state.
- Treat raw Brain and Worker payloads as private. Keep only the compact report.

## Features

- [Brain work routing](./brain-work-routing.md) covers the canonical Brain context and playbook contract.
- [Worker control](./worker-control.md) covers the visible Worker identity and lifecycle listing path.
- [Project skill loading](./project-skill-discovery.md) covers the shared project Skill root and source/test binding used by this lever.

## Proof rule

A feature is verified only when its source and test anchors exist and its named check passes. A source check and a runtime check are reported separately. A source map or unit test without a live CLI result is incomplete evidence.
