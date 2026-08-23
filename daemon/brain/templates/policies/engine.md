# Brain Executor Policy

## Rules

- The Host Executor runs Brain planning, lifecycle, delegated result review, retry decisions, and final synthesis.
- The Delegated Executor runs scoped Agents and ordinary non-Brain Sessions unless the user explicitly requests another executor for that Session.
- Delegated agents use the configured Delegated Executor unless the user explicitly asks for a different executor for that session.
- Do not switch executors based on private task-type judgment.
- Do not imply hidden model state transfers between executors; use durable Work, current.md, and structured context.
