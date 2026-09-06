# Brain and the Work Lifecycle

Brain is the sole master orchestrator above delegated Sessions. Given a user
goal and boundaries, it decomposes and orders the work, chooses or reuses
scoped Sessions, reviews their outputs, and advances the next runnable concern
without requiring the user to type `continue`. Delegated Agents execute scoped
concerns; they do not own the overall plan.

Brain reacts to persisted results and exceptions. Runtime delivers them to the
current Host and records delivery. No acknowledgement or resolve ceremony is
required: Brain decides whether to continue, accept, cancel or wait.

While a Work has a due Wake, Brain has no open polling turn. The
daemon's lifecycle timer wakes at the exact due instant or `claim_expires_at` and
re-enters normal event delivery. A daemon restart reconstructs that timer from
durable lifecycle state.

A discoverable external condition uses a source-specific wake or `due_retry`
with a durable `next_attempt_at`. The daemon timer creates one actionable wake
for one bounded check. Generic Brain-thread `user_input` cannot match that wait,
so unrelated conversation does not revise the Work or create a card.

The visible Brain timeline is a projection. One Work owns one replaceable card;
repeated delivery of its stable Event cannot append duplicate cards. Session,
provider, tmux, transcript, and process observations help decide whether an
Attempt is viable, but none can mark Work done.

Continue with `zen worker send -id <session> --work-id <work> -text <follow-up>`.
Runtime mints the Turn token and atomically binds accepted input as execution.
There is no separate accepted-but-non-owning state or typed continue step.
Accept with `zen brain work update -id <work> -status done`.
Worker reports and provider termination never implicitly accept Work.

An unknown send may have arrived. Brain decides whether to reconcile or retry;
a new attempt is permitted while the original unknown receipt remains evidence.
Source-write coordination is contextual, with isolation only when needed.
Exact receipts and stale-turn checks remain persistence correctness, not a
mandatory exclusive-writer product mechanism.

Delivered facts are not repeatedly admitted when a Host ends without resolving
them. They remain discoverable, explicit redelivery is possible, and independent
Work results proceed. New terminal evidence replaces an earlier provisional
exception and its current card; it cannot be stranded behind an old handling.
See [Work Lifecycle](work-lifecycle.md) for responsibilities and transitions.

Fresh Brain homes receive the provider-neutral lifecycle and delegated
Worker protocol from the versioned templates under `daemon/brain/templates/`.
Managed-block repair refreshes those product-owned blocks while preserving
user-authored text and the private `soul.md`, `profile.md`, `memory.md`,
`current.md`, and worklogs. `soul.md` owns stable expression and judgment
principles. `profile.md` owns user background and preferences. `memory.md` owns
durable facts and decisions. Fresh and upgraded homes receive a missing or
empty `soul.md` with mode `0600`; reconciliation and `zen brain gc` preserve
every nonempty `soul.md` byte for byte. Brain Host startup and managed
`AGENTS.md` require the Host to read `soul.md` once when each Brain Host Session
starts or is replaced, before its first response or work. The Host follows the
loaded principles for that Session and re-reads the file only if it changes.
The bootstrap prompt references the file but never copies its private contents.

Brain Worklog boundary: internal audits, handoffs, and delegated reports belong
under the configured Brain workspace's `worklog/` directory (normally
`~/.zen/brain/workspace/worklog`). They must not be written to a project
repository, a Worker cwd, or `cwd/docs/worklog`. Delegated reports should be
returned in the Worker result unless persistence is explicitly requested. Product
documentation is separate and must name its repository path explicitly.

The shipped `soul.md` asks for concise, direct prose in the user's language,
useful structure, and a clear distinction between facts, assumptions and
recommendations. It does not impose a technical-writing standard or a fixed
response template. Prompt ownership and model guidance are in [prompting.md](prompting.md).
