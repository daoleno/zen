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

For `continue`, Brain first sends one scoped follow-up to an explicitly chosen
or created Session using one random Turn identity and the exact Review
capability. A definite pre-mutation failure may retry that same Turn; ambiguous
or unknown submission is no-replay. Only after exact provider acceptance does
Brain resolve the Review with typed `continue`, which activates that exact Turn
as the Attempt. Old Attempt tokens and fences remain stale after the transfer.

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

Provider ambiguity is retained only on the exact input admission. It never
creates a lifecycle status, fallback Session, automatic continuation, or
scheduler retry.

Fresh Brain homes receive the provider-neutral lifecycle and delegated
Agent protocol from the versioned templates under `daemon/brain/templates/`.
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

The shipped `soul.md` uses ASD-STE100 Simplified Technical English as a
practical English style baseline: short direct sentences, one instruction or
idea per sentence where practical, active voice, explicit actors and
conditions, consistent terms, defined abbreviations, and no decorative idioms
or vague references. Zen does not claim full ASD-STE100 conformance without
formal dictionary and document validation. Chinese uses analogous clarity
rules; Zen does not claim that the English standard governs Chinese.
