# Project skill loading

Zen's native Skill loaders and shared inventory discover the project Skill from `.agents/skills`. Pi's native `PackageManager.resolve` and `loadSkills` path has already loaded `zen-verification` with `diagnostics: []`. This feature verifies the source contract only. It does not claim that the shell lever re-proves native loader behavior.

## Sub-features

- `shared-project-root` uses the repository's `.agents/skills` path for the native project loader and shared inventory.
- `skill-frontmatter` exposes the `zen-verification` name and description.
- `feature-map` links each feature to runtime, source, and test anchors.

## How to get to it (user POV)

- Open `.agents/skills/zen-verification/SKILL.md` in a Zen checkout.
- Run `scripts/verify-zen-orchestration.sh --json --state-dir /absolute/path/to/existing-zen-state` to validate the map against the checkout.

## Driving it with the Zen CLI

Preconditions:

- Run from the checkout whose source paths are being verified.
- Keep the manifest and feature files in the same skill directory.

- **Skill root.** The lever checks the project skill path and the frontmatter file. The result contains a `project-skill-discovery` source check when both exist.
- **Anchors.** The lever resolves every source and test path in `manifest.json`. Missing paths produce a named failure and exit code 1.
- **Native loader.** Pi's installed loader is the runtime evidence for Skill loading. The shell lever reports the source check separately and does not substitute Brain's playbook catalog for loader evidence.

## Gotchas

- A global copy of the skill does not satisfy the project-root source check.
- A markdown-only feature list is not machine-verifiable. Keep `manifest.json` in sync.
- Do not copy provider-specific pstack commands into this skill. Zen keeps executor choice provider-neutral.
