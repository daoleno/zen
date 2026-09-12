# Project skill discovery

Zen's Codex adapter discovers project skills from `.agents/skills`. This feature verifies that the Zen verification skill is present at the standard project root and that its feature map is bound to real source and test files.

## Sub-features

- `codex-project-root` uses the repository's `.agents/skills` path.
- `skill-frontmatter` exposes the `zen-verification` name and description.
- `feature-map` links each feature to runtime, source, and test anchors.

## How to get to it (user POV)

- Open `.agents/skills/zen-verification/SKILL.md` in a Zen checkout.
- Run `scripts/verify-zen-orchestration.sh --json` to validate the map against the checkout.

## Driving it with the Zen CLI

Preconditions:

- Run from the checkout whose source paths are being verified.
- Keep the manifest and feature files in the same skill directory.

- **Skill root.** The lever checks the project skill path and the frontmatter file. The result contains `project-skill-discovery: pass` when both exist.
- **Anchors.** The lever resolves every source and test path in `manifest.json`. Missing paths produce a named failure and exit code 1.
- **Runtime binding.** The lever checks `brain_playbooks` and reports the exact runtime check ID alongside the feature ID.

## Gotchas

- A global copy of the skill does not satisfy the project-root check.
- A markdown-only feature list is not machine-verifiable. Keep `manifest.json` in sync.
- Do not copy provider-specific pstack commands into this skill. Zen keeps executor choice provider-neutral.
