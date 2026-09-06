# Zen Prompt Design

Zen uses short standing instructions and task-specific context. Model guidance is based on the official [Using GPT-6 Astra](https://developers.openai.com/api/docs/guides/latest-model) guide, retrieved September 6, 2026. The guide identifies the model as `gpt-6-astra`.

## Astra Behavior

| Documented tendency | Zen instruction |
| --- | --- |
| Clarifies when input could materially change the result | Complete routine authorized work and independent preparation; ask for consequential missing decisions or new authority. |
| Sensitive to skills and instruction-file conflicts | Give each rule one owner; user instructions override skill guidelines within platform constraints. Identify the specific rule when a skill blocks work. |
| Detailed, formatted output | Prefer concise prose and useful structure. Report changed results or blockers, not unchanged progress. |
| May delegate less than desired | Define the visible Zen Worker boundary explicitly, including an explicit user request for direct Brain execution. Provider-native agents and internal subagents are not Zen Workers. |
| Can over-test small coding changes | Use meaningful checks proportional to risk and required repository gates. Repeat or broaden only for changes, failures or unresolved concerns. |

Astra fits complex, multistep engineering and tool workflows. This is task-suitability guidance, not a claim about a best time of day or a benchmark of Zen latency. Routine follow-ups may need less reasoning than architecture or difficult debugging; preserve the configured effort until comparative evidence supports a change. Astra does not support `none`; the guide recommends starting at `low` when migrating from `none` or `minimal`.

Async tool calling, mid-turn steering and cached reasoning updates require harness support. Instructions alone cannot enable them. This refactor does not change models, routing, effort, API endpoints or request parameters.

## Instruction Owners

| Surface | Responsibility |
| --- | --- |
| Repository `AGENTS.md` | Code layout, Android/iOS and current-server invariants, commands, repository verification and safety. |
| `daemon/modelprofiles/codex_catalog_instructions.md` | Zen-owned generic coding defaults for managed Codex catalogs; not a verbatim upstream persona. |
| `daemon/brain/templates/AGENTS.md` | Brain context map, authorization, scheduler and event-driven waiting rules. |
| `daemon/brain/delegation_contract.go` | Versioned compact Host/Worker role used for activation and managed role projection. |
| `daemon/brain/templates/soul.md` | Default expression and judgment; existing private soul files remain user-owned. |
| `daemon/brain/templates/policies/` | On-demand delegation, executor routing and Host recovery rules. |
| `daemon/brain/playbooks.go` | Optional short alignment, brief, decomposition and investigation procedures. |
| Host bootstrap in `daemon/brain/service.go` | Paths, actual executors/capabilities, personality and loading instructions; no repeated policy manual. |
| Host handoff in `daemon/brain/service.go` | Thread/executor identities and owned Worker IDs/statuses. Read current.md rather than embedding its history or Worker summaries. |
| Worker builders in `daemon/cmd/zen/control_app.go` | Scoped execution and resource protocol, progress vocabulary and one exact turn argument. Preserve original user bytes. |
| `daemon/work/dispatch.go` | Short Work-file pointer and terminal frontmatter requirement; retained because its file workflow consumes it. |
| `daemon/calendar/work_runner.go` | Scheduled action instructions, bounded deliverable markers and metadata; retained because result extraction validates that contract. |

Work Event resolution commands retain exact identities. They are actionable transaction data, not prose to abbreviate. Provider conversation parsers, terminal permission-prompt UI and historical documentation are not active model instructions. Global installed skills and private runtime overlays are outside repository source ownership.

The compact role is repeated at Host activation intentionally: a resumed provider process needs the current version even if its retained history contains older instructions. Other workflow policies refer to AGENTS.md instead of restating that role.

## Verification And Rollout

Tests cover generated payload preservation, one exact turn identity, mandatory progress fields, prompt size limits, instruction ownership, transcript privacy and preservation of private runtime overlays. These are deterministic contract tests, not model behavior or latency evaluations.

Representative same-configuration measurements: bootstrap 13,145 to 1,604 bytes; initial Worker prompt 4,230 to 2,017 bytes. A handoff fixture with 1,000 historical lines shrank from 21,375 to 548 bytes. Bootstrap comparison uses the same current role constant on both sides. Exact sizes vary with paths and task content; bytes are not token counts.

Managed workspace repair updates product-owned blocks and preserves user-authored content. Nonempty soul.md and seeded custom playbooks are not overwritten. Existing private files can still contain conflicting guidance and need explicit review when adopting the new defaults. A Host already running with old instructions does not retroactively lose its context. Deploy and activate the source changes through the normal authorized service lifecycle; do not rewrite live private overlays or restart services as an incidental prompt edit.
