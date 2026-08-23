# Brain and the Work Lifecycle

Brain is the sole master orchestrator above delegated Sessions. Given a user
goal and boundaries, it decomposes and orders the work, chooses or reuses
scoped Sessions, reviews their outputs, and advances the next runnable concern
without requiring the user to type `continue`. Delegated Agents execute scoped
concerns; they do not own the overall plan.

Brain reacts to lifecycle Events; it does not supervise time. When a Work
needs judgment, lifecycle exposes one due `event_id`. Brain claims it for an
exact Host Session and turn, sends the notification once per delivery attempt,
and resolves it with a typed disposition.

While an Event is scheduled for retry, Brain has no open polling turn. The
daemon's lifecycle timer wakes at `next_attempt_at` or `claim_expires_at` and
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

For `continue`, Brain supplies the exact Event capability and next Attempt
identity. Lifecycle resolves the Event and admits that Attempt atomically. It
reuses the Session when viable; otherwise the new Session receives durable Work
context and workspace facts. Old Attempt tokens and fences remain stale after
the transfer.

`until_done` never starts this path by itself. After any terminal, failed, or
lost Attempt, repeated lifecycle sweeps and daemon restarts only preserve and
redeliver the same actionable Event. Brain reviews its exact evidence, writes
one scoped concern, explicitly creates or reuses a visible delegated Session,
waits for provider input acceptance, and then resolves the Event with the one
accepted next Attempt identity. Lifecycle code neither reads the Delegated
Executor to continue Work nor manufactures a generic continuation prompt.

When exact terminal evidence arrives after a provisional lease-expired/lost
fact for the same latest Attempt, the stronger evidence updates the current
result without replacing the unresolved `event_id`. Brain dispositions the
same stable card from the stronger evidence; no parallel notification or
automatic Attempt is created.

Ambiguous external mutation is recorded as `unknown_side_effect`. Brain keeps
the Event visible for explicit judgment and never asks the daemon to replay it
automatically.

Fresh Brain homes receive the provider-neutral lifecycle and delegated
Agent protocol from the versioned templates under `daemon/brain/templates/`.
Managed-block repair refreshes those product-owned blocks while preserving
user-authored text and the private `profile.md`, `memory.md`, `current.md`, and
worklogs.
