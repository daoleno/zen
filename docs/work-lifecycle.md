# Zen Work Lifecycle

Zen's durable coordination model has one `Work` aggregate with at most one
`Attempt`, one `Wake`, one `Review`, and append-only `Event` records. They are persisted by one lifecycle authority under
`state/lifecycle/`; current rows and append-only scheduler events commit in one
transaction. Provider, tmux, PID, transcript, and UI state are evidence or
projections, never lifecycle truth.

This schema is intentionally breaking. Normal startup accepts only the current
transaction-image schema; it never falls back to an earlier scheduler directory
or reconstructs execution state from presentation. An explicit offline upgrade
is described below.

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

Same-Session continuation is deliberately two steps. Brain first submits one
scoped follow-up with one random Turn identity and the exact Review capability.
Provider acceptance leaves that admission non-owning. Brain then resolves the
same Review with typed `continue`; that transaction closes the Review and
activates exactly the accepted Turn as the next Attempt.

The exact provider mutation is first recorded as an `AdmissionState`. A
review-bound follow-up is tagged with the exact handling identity. It remains
non-owning until provider acceptance and the typed `continue` disposition
atomically promote it to the active Attempt.

Admissions are indexed by their exact proposed Turn token. Host review delivery
cannot replace a Worker's accepted signal contract. A Work still has at most one
unresolved transport transaction and one current Attempt. Retaining an accepted
receipt does not authorize an old token to become current, waive a handling
identity check, close a Review, or replay an uncertain submission.

After a lost CLI response, inspect known input without sending it again:

```sh
zen worker receipt --work-id <work-id> --id <session-id> --turn-id <turn-token>
```

The receipt reports canonical acceptance and `owns_attempt` separately. It works
without a live tmux target and does not mutate lifecycle state. A follow-up
accepted under an ended handling remains evidence, not a capability to bypass
the currently required review disposition.

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

An open Review is the durable delivery obligation. A handler atomically claims
it by `Review.EventID`. If it disappears before confirmed delivery,
`claim_expires_at` releases the claim and the same Review becomes claimable.
Repeated delivery and repeated resolution are idempotent. The timeline
projection keys the card by `Review.EventID`, so repeated handling produces
exactly one card.

The first unresolved actionable Event remains the review/card identity when
evidence strengthens. In particular, a provisional lease-expired/lost fact may
later be upgraded by exact terminal evidence for the same latest Attempt. The
existing `event_id`, claim, and notification stay in place while its current
reason and Attempt result reflect the stronger evidence. Brain dispositions
therefore target the same Event; no second card or automatic Attempt is made.

Provider uncertainty exists only at the exact input-admission boundary. A
definite pre-mutation failure may retry the identical payload with the same
Turn identity. An ambiguous or unknown outcome is durable no-replay evidence;
it creates no scheduler state, Session, Attempt, Wake, or fallback action.

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
- A definite no-submit may retry only the exact same admission identity.
- Ambiguous or unknown admission is never replayed automatically.
- Heartbeats affect Attempt liveness only.
- Execution evidence cannot imply Work completion.
- Lifecycle timers never create Sessions or infer delegated prompts.
- `until_done` gates completion only.
- Current rows are usable directly; no second event-log replay is required to
  repair a separate authority.
