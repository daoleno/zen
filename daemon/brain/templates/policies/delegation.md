# Brain Delegation Policy

Follow the Brain/Worker role in AGENTS.md. Zen Workers are visible execution sessions; provider-native agents and Codex internal subagents are different concepts.

## Brief And Review

- Give one Worker a coherent concern, cwd, necessary context, observable acceptance criteria, safety constraints, verification and expected report. Include current/desired behavior and interfaces when they add information; omit empty sections and repeated standing rules.
- Reuse the same viable Worker across stages. Parallelize independent concerns only when they do not share fragile state or unresolved decisions.
- Inspect every delegated result against acceptance criteria before integration. Send a focused follow-up for a concrete gap; otherwise record the result and close the owned session when the larger task is done.
- Scale verification to risk. Use meaningful behavior checks, complete required repository gates, and broaden or repeat only for new edits, failures or unresolved concerns. Do not replace a full-task requirement with a passing subset.
- Return reports in the Worker result. Persist private reports only in the runtime Brain worklog/ when requested; name repository paths explicitly for product documentation.

## Event Continuation

Use the exact identities and commands supplied by the current Work Event. For same-Session continuation:

1. Mint one random turn:<uuid> and send the scoped follow-up once with the event's Work, handling, provider-turn and revision fields.
2. After exact acceptance, resolve continue with the accepted Session and turn token. Receipt acceptance alone is not active Attempt ownership.
3. A definite pre-mutation failure may retry the same payload and identity. Ambiguous or unknown delivery is no-replay: seek exact receipt evidence; do not resend or create a replacement turn.

Record a typed disposition and durable next action before ending each handling turn. Use event-driven waiting as defined in AGENTS.md.
