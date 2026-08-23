# Zen Work Lifecycle

Zen's durable coordination model has three records: `Work`, `Attempt`, and
`Event`. They are persisted by one lifecycle authority under
`state/lifecycle/`; current rows and append-only scheduler events commit in one
transaction. Provider, tmux, PID, transcript, and UI state are evidence or
projections, never lifecycle truth.

This schema is intentionally breaking. Zen does not read, migrate, archive, or
fall back to an earlier scheduler directory or document.

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

Resolving an Event with `continue` and admitting the next Attempt is one
transaction. A restart therefore sees either the unresolved Event and old
Attempt, or the resolved Event and exact new Attempt, never an intermediate
next-Attempt reservation.

The exact provider mutation is first recorded as an `AdmissionState`. A
review-bound follow-up is tagged with the exact handling identity. It remains
non-owning until provider acceptance and the typed `continue` disposition
atomically promote it to the active Attempt.

`until_done` changes only completion authority: an unaffirmed terminal result
cannot complete the Work. It does not queue a continuation, schedule a retry,
select an executor, create a Session, submit a generic prompt, or admit another
Attempt. The terminal result opens one actionable Event for Brain. Brain names
the next scoped concern and explicitly creates or reuses its Session before
resolving that exact Event with `continue` and the accepted Attempt identity.

## Event

An actionable Event has one stable `event_id`. That ID is its scheduler,
review, claim, notification, delivery, card, and resolution identity. Claimant
session/turn and expiry are lease metadata on the Event, not another action
identity.

Events are delivered at least once. A handler atomically claims an unresolved,
due Event. If it disappears before confirmed delivery, `claim_expires_at`
releases the claim and schedules the same `event_id` again. Repeated delivery
and repeated resolution are idempotent. The timeline projection keys the card
by Work and replaces it, so repeated handling produces exactly one card.

The first unresolved actionable Event remains the review/card identity when
evidence strengthens. In particular, a provisional lease-expired/lost fact may
later be upgraded by exact terminal evidence for the same latest Attempt. The
existing `event_id`, claim, and notification stay in place while its current
reason and Attempt result reflect the stronger evidence. Brain dispositions
therefore target the same Event; no second card or automatic Attempt is made.

External delivery records exactly one outcome:

| Outcome | Meaning | Automatic action |
| --- | --- | --- |
| `success` | Effect is confirmed | Resolve delivery |
| `retryable` | Effect provably did not happen | Increment `attempt_count`, set `last_error` and `next_attempt_at` |
| `unknown_side_effect` | Effect may have happened | Keep visible; never retry automatically |
| `terminal` | Delivery cannot succeed without judgment/change | Keep visible; never retry automatically |

The daemon calculates the earliest `next_attempt_at`, claim expiry, or Attempt
liveness deadline and waits on one timer plus lifecycle commit wakeups. On
restart it reconstructs the next timer from durable rows. Brain never sleeps,
polls Sessions, or holds an LLM turn open while waiting.

`due_retry` is the generic durable wait for a discoverable external condition.
Its source reference and timestamp are exact: unrelated `user_input` is a no-op,
and repeated timer sweeps create one stable actionable Event.

## Invariants

- One lifecycle store and transaction boundary owns Work, Attempt, Event,
  claims, resolutions, retry scheduling, and next-Attempt creation.
- At most one active Attempt exists per Work.
- Exact `session_id + turn_token + fence` gates all Attempt mutation.
- One `event_id` names an actionable fact from creation through resolution.
- Internal transitions and repeated resolution are idempotent.
- Only `retryable` receives `next_attempt_at`.
- `unknown_side_effect` is never replayed automatically.
- Heartbeats affect Attempt liveness only.
- Execution evidence cannot imply Work completion.
- Lifecycle timers never create Sessions or infer delegated prompts.
- `until_done` gates completion only.
- Current rows are usable directly; no second event-log replay is required to
  repair a separate authority.
