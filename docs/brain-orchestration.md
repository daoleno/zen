# Brain Orchestration

Zen Brain uses a host/delegation model.

Brain is the host and orchestrator. It owns decomposition, ordering, judgment,
delegated result review, final synthesis, durable memory, and user-facing
decisions.

The orchestration behavior is expressed as durable natural-language agent
instructions in Brain bootstrap and workspace policy files, similar to
`AGENTS.md` or Cursor rules. These files are not the primary end-user
configuration surface. A normal install should work without editing them; users
configure executor routing through `executors.toml`, while advanced users can
still inspect or edit Brain workspace policy files when they want custom
behavior.

Delegated agents are scoped execution sessions. They are best for bounded execution:
reading a constrained area, making a scoped change, running verification,
reproducing a bug, or comparing alternatives. A delegated agent should receive
one concern, the workspace, enough context to avoid re-exploring the whole repo,
acceptance criteria, safety constraints, feasible verification, and a short
expected report.

Independent delegated tasks may run in parallel when that reduces elapsed time
without creating shared-state risk. Brain should keep the work in one coherent
thread when the hard part is product judgment, ambiguous design, or a gnarly bug
that cannot be split cleanly.

Delegated output is not automatically accepted. Brain captures and reviews the
session, compares the result with the acceptance criteria, checks verification,
and then decides whether to integrate, send a focused follow-up, spawn another
delegated agent, ask the user, or stop.

Executor choice stays provider-neutral. The Host Executor runs the Brain
orchestrator and falls back to Codex when no Brain host is stored. The Delegated
Executor runs scoped execution sessions; `delegated_executor` controls both
Brain delegated agents and ordinary non-Brain session creation. A different
executor is used for a session only when the user explicitly asks for one, such
as `@codex`, `@grok`, or `@claude`.

Brain only manages sessions that are marked `delegated=true`. User-owned or
external sessions must not be closed, renamed, repurposed, or controlled unless
the user explicitly asks for that specific session.
