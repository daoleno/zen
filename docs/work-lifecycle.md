# Zen Work Lifecycle

Zen's durable coordination model has one `Work` aggregate with at most one
`Attempt`, one `Wake`, one `Review`, and append-only `Event` records. They are persisted by one lifecycle authority under
`state/lifecycle/`; current rows and append-only scheduler events commit in one
transaction. Provider, tmux, PID, transcript, and UI state are evidence or
projections, never lifecycle truth.

This schema is intentionally breaking. Normal startup accepts only the current
transaction-image schema; it never falls back to an earlier scheduler directory
or reconstructs execution state from presentation. There is no supported
old-schema migration or offline upgrade command; see Worker Upgrade below for
the fresh-state maintenance handoff. This lifecycle candidate adds no migration.

## Work

A Work is the aggregate root. Its current row contains objective, completion
policy, status, monotonic revision, monotonic fence, durable context, the
optional active Attempt, and its Events. A Work can be terminal only after an
exact lifecycle command establishes completion or cancellation. Process
silence, an idle pane, and provider disappearance cannot complete it.

Work revision changes only for lifecycle changes. Attempt heartbeat renewal is
excluded: identical or coalesced heartbeats update no Work revision and create
no Event, notification, or card.

## Attempt

A Work has at most one active Attempt. There is no separate lifecycle Owner.
The Attempt's authority is the exact tuple:

```text
(session_id, turn_token, fence)
```

`fence` increases monotonically whenever execution authority changes. Every
progress, completion, failure, and liveness command must match the active
Attempt's session, token, and fence before state changes. A duplicate current
input is idempotent; an old token, wrong session, or wrong fence is rejected.

Attempt loss releases execution authority but never completes Work. A later
Attempt can reuse the viable Session or start anew from the Work's
durable objective, completion criteria, context, and workspace facts.
The last lost execution remains eligible for exact authoritative terminal
evidence after an explicit wait and restart. Eligibility follows its durable
loss fact and execution fence, not whether the provisional Review is still
displayed. A newer execution or closed Work invalidates that authority.

### Supervision And Decisions

Execution, supervision, and Brain decisions have separate responsibilities:
the Attempt owns execution, the daemon supervises its progress deadline, and
Brain receives result and exception facts through a durable delivery record. A check-in deadline is not a
terminal result and does not create a Review, notification, or Brain turn.

| Current state | Evidence or command | Result | Brain admission |
| --- | --- | --- | --- |
| Owned Attempt | Exact check-in or recent bound provider progress | Renew the same Attempt deadline | None |
| Owned Attempt | Deadline reached | Record one expiry fact; retain ownership | None |
| Expired Attempt | Same unchanged expiry | No new state; next timer is loss-grace deadline | None |
| Expired Attempt | Fresh exact progress | Renew the same Attempt | None |
| Owned Attempt | Exact completion or failure | Release ownership and persist result | One result for Brain |
| Expired Attempt | Loss-grace deadline with no new progress | Release ownership as lost, never as completed | One loss decision |
| Owned Attempt | Producer disappears | Record uncertain/lost outcome, never fabricated success | One loss decision |
| Active decision | Exact session_terminal wait naming its Attempt | Close decision, retain Attempt; do not add a Wake beside it | None |
| Delivered decision | Handling ends without a valid disposition | Retain ended capability and visible unresolved decision | None until explicit recovery |
| Ended decision | Explicit actor replay | Clear handler; allow a fresh handling | One authorized delivery |
| Ended decision | Genuinely new actionable fact | Replace decision and invalidate old capability | One new decision; unchanged content cannot reopen it |
| Any producer decision | New exact terminal evidence | Supersede prior decision and capability | Fresh result for Brain |

Provider `running` is an activity status, not proof of continuing progress.
Only recent source-timestamped events bound to the exact Turn renew its lease;
rereading a cached status cannot keep a hung producer owned indefinitely. The
deadline scheduler reconciles due producers before sweeping. After an expiry
fact is recorded it schedules the loss-grace deadline, not the expired timer.
Normal Worker check-ins remain useful when a long tool call emits no structured
provider activity. Silence remains uncertain until the existing loss boundary.

For an owned wait, the reference is `session:<session-id>:turn:<turn-token>`.
Bare Session names and another producer's Turn do not authorize changing the
active Attempt. Without an Attempt, wait retains its usual typed external Wake.

### Model-Led Continuation

Normal operation is one command per decision:

```sh
zen worker send -id <session-id> --work-id <work-id> -text '<scoped follow-up>'
zen brain work update -id <work-id> -status done
```

Sending a follow-up mints an internal Turn identity. Exact acceptance records the
receipt, retires the previous result/Attempt if present, and starts the new Attempt
in one transaction. There is no accepted-but-non-owning Worker stage and no second
`resolve continue` call. Host delivery receipts remain separate from Worker
execution; delivering a notification cannot take Worker ownership.

Unknown delivery is a fact: the input may have arrived. Brain decides whether to
inspect a receipt, reconcile effects or send another attempt. A new Worker send
is allowed after an unknown outcome, including after restart. The previous
receipt remains evidence. Repeating an identical transport receipt is idempotent;
a newer model-directed submission has precedence over late old acceptance.
The persisted submission sequence determines this order, not timestamps or
lexicographic random tokens.

A notification contains result data, not a generated command program.
`resolution_required`, `resolve_command`, the Worker send handling/event/revision
flags and the `AcceptReviewFollowUp` transition are removed. Explicit Work
updates can record acceptance during a Host turn without acquiring a capability.
The optional typed wait/recovery APIs remain available for explicit scheduling
and redelivery decisions; they are not prerequisites to normal delegation.

### Responsibility Change

| Concern | Before | Now |
| --- | --- | --- |
| Decomposition, ordering, coordination | Brain constrained by runtime choreography | Brain decides from context and user boundaries |
| Follow-up | Send with review capability, then resolve to activate | One accepted send activates execution atomically |
| Completion | Bounded provider end or criteria flag could complete Work | All reports are results; only Brain/operator decision accepts Work |
| Unknown input | Unresolved admission prevented another mutation | Preserve uncertainty; Brain may submit a new attempt |
| Notification | Resolve instruction and frozen metadata gate | Factual result; no mandatory acknowledgement |
| Missed check-in | Clock exception woke Brain | Runtime supervises progress and reports actual loss |
| Host interruption | Unchanged decision could be re-admitted | Delivered fact remains visible, independent facts proceed |
| Source writes | Blanket writer/worktree rules in earlier design brief | Contextual model coordination; no exclusive-writer mechanism |
| Persistence and permissions | Exact receipts, atomic store, owned Sessions | Retained implementation correctness and actual permission boundaries |

The retained Attempt is execution correlation, not a source-writer lock.
Review/handler fields are internal notification bookkeeping, not a model-managed
lease. Wake represents a requested future input, not an invented program plan.
Completion policies remain descriptive Work metadata; neither policy starts
follow-ups or accepts Worker reports. No compatibility or migration layer is added.

### Completion Loop

The canonical path is Worker report, durable Turn/result, Work-change event,
serialized Host admission, receipt-confirmed delivery, Brain decision, durable
Work closure, then removal of the exact completed owned Session. The server
subscribes before its startup snapshot and reconciles durable Work changes;
heartbeat polling and client snapshot reads are not required producer wakes.

Only current provider execution defers internal result input. Delivered review
metadata is history, not evidence that Brain is running. There is no global
delivered-review gate or global single-unfinished-handler validation. Exact
terminal or superseding Turn evidence retires interrupted handlers; accepted
delivery remains consumed across restart. Unresolved results remain available
for ordinary model decisions without a mandatory receipt-resolution command.
Provider transcript probe errors defer mutation until evidence is readable.
An incomplete discovery inventory also cannot prove process death: restart
reconciliation requires an authoritative absent Session probe before recording
loss. Present or unreadable Session transport preserves the original Attempt.

Terminal summaries come from the canonical lifecycle result, with the Session
reference matched by exact Work and Turn identity, never the first running
progress row. Repeated terminal Control/provider reports are idempotent for the
current Turn; superseded identities cannot mutate a reused Session.

`work update`, typed disposition and explicit Work close share cleanup. Cleanup
is derived from closed Work and current terminal delegated Turns rather than a
second persisted workflow. Startup and durable Work-change events recover a
decision saved before teardown. Under the Session input lock, teardown rechecks
the exact Turn, pending admissions, delegated ownership, and process/pane
generation. A newer execution, user Session, Host or uncertain result is not
eligible. Resource/route release failures remain errors and can be retried
without undoing acceptance or replaying a notification. This guarantees durable
deduped results and idempotent effects, not exactly-once model execution.
Control reports a post-decision cleanup failure as `brain_work_cleanup_pending`
and includes the saved Work (and resolved Event for a typed disposition). It
does not imply acceptance failed. Cleanup during a decision is scoped to its
Work; unrelated historical cleanup conflicts remain visible in reconciliation.

Executable Given/When/Then contracts, deterministic CI commands and the bounded
opt-in provider gate are documented in [Behavior Testing](behavior-testing.md).

## Worker Upgrade

The Worker release changes CLI/control and mobile wire names together. Use
`zen worker`; `zen agent` and the old control aliases are not supported. Native
provider Agent names and Codex subagents are separate concepts and are not
renamed. Mobile and daemon versions must be upgraded together.

Lifecycle and Calendar use schema 2. Work Markdown uses `worker_session` instead
of `agent_session`. Old records are rejected rather than silently losing their
Session links. This development release does not provide old-format migration.
Use fresh state for the new release; do not point it at an active old deployment.
Existing data can be archived separately without importing it into the new state.

Live process environments, tmux ownership markers, resource supervisors, provider
transcripts and active Brain instruction overlays are not upgraded by source edits.
Plan an authorized maintenance handoff: finish and review active Work normally,
preserve native resume identities/transcripts and configuration, then stop the
old daemon/hot watcher and retire old managed Sessions through their existing
owner. Do not cancel or mark unfinished Work complete merely to permit an
upgrade. Deploy the new daemon/mobile pair and create or resume Sessions through
the new owner. Keep archived data until the new state and native resume paths are
verified. Do not integrate source into a running `zen-dev` tree as a shortcut.

Both completion policies leave acceptance to Brain. Neither queues a
continuation, selects an executor or manufactures a prompt. Worker/provider
success is not proof that the user's larger objective was accepted.

## Event

An actionable Event has one stable `event_id`. That ID is its scheduler,
review, claim, notification, delivery, card, and resolution identity. Claimant
session/turn and expiry are lease metadata on the Event, not another action
identity.

An open Review is the durable delivery obligation. A handler atomically claims
it by `Review.EventID`. If it disappears before confirmed delivery,
`claim_expires_at` releases the claim and the same Review becomes claimable.
Confirmed delivery is consumed even if the handling fails to resolve. Ending
that handling does not release it for automatic redelivery. The unresolved
Review remains visible and does not hold the Host lane; the existing explicit
actor lease-recovery command can authorize replay. Its ended state is canonical
and survives restart. An ended or superseded capability cannot disposition a
new decision. Repeated ending, claim attempts, and resolution are idempotent.

Unknown submission outcomes remain durable evidence across model-directed
retries and restart. Only one prepared transport transaction may exist per
Work; older ambiguous outcomes do not prevent reopening the store.

Exact terminal evidence supersedes a producer's earlier decision, including an
input request or provisional lost outcome. It gets a new decision identity and
invalidates the earlier handling; the prior card becomes history, never another
active obligation. A delivered or ended provisional decision cannot swallow a
required result notification. Exact delegated terminal signals use the same
canonical result path as provider evidence, including after lease expiry.
There is no separate audit-only completion path selected by lease attention.

Provider failures, including rate limits and unavailable evidence, are factual
results, never success. Bound provider terminal results are delivered even when
the Worker could not emit its own completion command. Unknown input stays on
its receipt; it does not invent a Session, retry or fallback. Brain can request
redelivery explicitly or proceed with ordinary Work/send commands.

The daemon calculates the earliest due Wake, claim expiry, or Attempt
liveness deadline and waits on one timer plus lifecycle commit wakeups. On
restart it reconstructs the next timer from durable rows. Brain never sleeps,
polls Sessions, or holds an LLM turn open while waiting.

`due_retry` is the generic durable wait for a discoverable external condition.
Its source reference and timestamp are exact: unrelated `user_input` is a no-op,
and repeated timer sweeps create one stable actionable Event.

## Invariants

- One lifecycle store and transaction boundary owns Work, Attempt, Wake,
  Review, Events, claims, and resolutions.
- At most one active Attempt exists per Work.
- Exact `session_id + turn_token + fence` gates all Attempt mutation.
- One `event_id` names an actionable fact from creation through resolution.
- Internal transitions and repeated resolution are idempotent.
- A definite no-submit can reuse its receipt; a new model decision can use a new receipt.
- Ambiguous or unknown admission is never replayed automatically.
- Heartbeats affect Attempt liveness only.
- Execution evidence cannot imply Work completion.
- Lifecycle timers never create Sessions or infer delegated prompts.
- Completion policies do not perform orchestration or accept results.
- Current rows are usable directly; no second event-log replay is required to
  repair a separate authority.
