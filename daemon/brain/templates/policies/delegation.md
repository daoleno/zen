# Brain Delegation Policy

Follow the Brain/Worker role in AGENTS.md. Zen Workers are visible execution sessions; provider-native agents and Codex internal subagents are different concepts.

## Brief And Review

- Give one Worker a coherent concern, cwd, necessary context, observable acceptance criteria, safety constraints, verification and expected report. Include current/desired behavior and interfaces when they add information; omit empty sections and repeated standing rules.
- Reuse the same viable Worker across stages. Parallelize independent concerns only when they do not share fragile state or unresolved decisions.
- Inspect every delegated result against acceptance criteria before integration. Send a focused follow-up for a concrete gap; otherwise record the result and close the owned session when the larger task is done.
- Scale verification to risk. Use meaningful behavior checks, complete required repository gates, and broaden or repeat only for new edits, failures or unresolved concerns. Do not replace a full-task requirement with a passing subset.
- Return reports in the Worker result. Persist private reports only in the runtime Brain worklog/ when requested; name repository paths explicitly for product documentation.

## Continuation

Send a scoped follow-up with the Work ID to reuse a Worker. Runtime mints and persists the turn identity and binds accepted execution; there is no second continuation command. Review the returned facts and decide whether to continue, accept, cancel or wait. Worker reports and provider errors are evidence, not acceptance of the larger objective.

Edit the supplied repository and cwd directly by default; preserve unrelated changes. Use a worktree under $ZEN_WORKTREE_ROOT only for an explicit user request, concrete conflicting edits, or a justified necessary isolation reason. Briefly explain the actual reason; concurrent Workers do not necessarily conflict. When using a worktree, integration into the owning target repository and requested delivery remain part of completion. A candidate branch or passing tests alone are not a delivered outcome.

For uncertain delivery, weigh duplicate effects and available evidence before choosing reconciliation or a new attempt. Runtime preserves both outcomes without imposing that choice.
