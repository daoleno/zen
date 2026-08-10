# Brain Orchestration

Zen Brain has three orchestration objects: Work, Event, and the existing
Session.

- **Work** is a durable user commitment. It exists only when an outcome must
  survive the current turn. Ordinary questions and discussion do not create
  Work.
- **Event** is an append-only result fact about one Work. A fact becoming
  actionable queues Attention; it is not itself admission, handling, or a
  disposition.
- **Session** is the existing executor resource and remains authoritative for
  process state, delegation, progress, and leases. Brain manages only Sessions
  marked `delegated=true`.

The complete automatic scheduling rule is:

```text
if the exact foreground Host turn is still running:
    preserve queued Attention without interrupting or replaying that turn
else if a unique ready Work key can be atomically claimed:
    alternate the oldest and newest ready Work keys
    admit one exact Brain handling turn for that Work
else:
    wait
```

The scheduler keeps at most one in-flight handling per Work key and one
delivered handling globally. Repeated facts for a ready Work coalesce behind
its scheduler head. A claim carries a sequence fence and presents the latest
authoritative fact through that fence, so `session.stale` followed by
`session.done` asks Brain to review `done` without rewriting either audit row.
The oldest/newest alternation is persisted only on successful claims, excludes
the Work that just yielded when peers are ready, and is bounded in both
directions: a fresh completion gets an admission opportunity within two
claims, while every fixed backlog key advances on every other claim.

Open, running, or waiting Work does not poll Brain. The `until_done` completion
policy requires an explicit done-criteria reference and changes only when Work
may be marked done; it does not manufacture continuation turns. Provider Goal
features may operate inside an individual Session, but they are not Zen
scheduler state and their internal context is removed from user transcript
projections.

## Persistence and ownership

Brain stores a versioned atomic JSON database at
`~/.zen/brain/state/orchestration.json`. Work, append-only Events, canonical
Turns, input admissions, and pending Turn submissions are replaced atomically.
Work carries only title, objective,
status, optional owner Session, completion policy, next action, wait condition,
and a context reference. Long plans and evidence stay in `workspace/worklog/`.

Event dedupe keys are unique within one Work. An actionable Event is durably
claimed for one Brain host Session before input is sent. Zen deterministically
renders one compact direct input containing only the Event ID, Work ID and
title, kind, source, summary, next action, and context/payload references. It
never includes the full objective, worklog, history, a delivery token, an
encoded envelope, or a fetch instruction. The stable Event ID is the optional
receipt in a bounded receiver-side tmux ledger. Each retained receipt carries
the payload hash, accepted or ambiguous outcome, and the delegated candidate
Turn ID. An accepted steering receipt is rebound to the exact existing Turn ID
that received it, so restart dedupe never consults whichever Turn later became
current. Zen writes and confirms ambiguity before provider mutation, then
promotes that same entry to accepted only after the submit queue succeeds.
Codex, Claude Code, Cursor, Grok, Pi, OpenCode, and
future interactive providers share one per-Session input serializer and one
target-bound tmux command queue: replace the current unsent draft, paste the
exact original UTF-8 payload, send the provider adapter's submit key once, and
delete the buffer.

If target identity or pane generation changes before that queue starts,
Session Input reports a definite pre-mutation failure and atomically releases
that exact Event claim. If the queue starts, an ambiguous result retains the
claim and is never automatically replayed. Exact input acceptance atomically
marks that exact host-bound Event delivered; delivery does not handle its
Attention and does not complete its Work. If persistence of delivery fails
after acceptance, a later dispatch reads that exact receipt from the same
Session Input ledger and finalizes delivery
without reconstructing or resubmitting the payload. An ambiguous or unavailable
receipt remains claimed and undelivered.
Pane history, rendered Composer/Footer/ANSI state, pending PTY byte counts, and
elapsed time are never send authorization. There is no transaction journal,
spool, background resume loop, second scheduler, replacement Event, ordinary
Event pull, or provider Goal delivery path.

Schema 2 adds host-bound delivery evidence to the existing Event record. Its
one-way schema-1 migration binds an unresolved claim to the persisted host when
available. A migrated unresolved claim remains closed; the migration does not
create a table, copy an Event, or add another delivery path.

New visible delegated Sessions create Work before launch and attach through an
empty-owner compare-and-set after Session creation. A concurrent losing spawn
terminates only its newly created Session and preserves the incumbent. Every
successful fresh Session inventory is an authority boundary: a nonterminal
Work owner absent from that inventory is atomically detached, moves to
`needs_input`, and retains or receives one deduplicated actionable review
obligation. A nonterminal canonical Turn first records `session.uncertain`;
absence never fabricates done/failed. Markerless or immutable Turns and pending
input submissions keep their append-only audit rows, but none can leave the
Work operationally owned by an absent Session or cause input replay. Repeating
the same inventory or reopening the store is a no-op. Present
`delegated=false` Sessions are not managed, and no runtime fallback adopts
later unowned Sessions. This convergence uses the one current state model; it
does not add a migration, compatibility channel, or second scheduler.

## Event sources

Watcher transitions are projected only when the Session already owns Work.
Ordinary progress is passive. Done, failed, blocked, and needs-input facts append
deduplicated actionable Events. Lease expiry requests authoritative
reconciliation; it is not itself a stale fact. A present live delegated Session
remains non-actionable even when progress is overdue. A missing, dead, or
irreconcilable delegated Session produces one deduplicated actionable stale
Event, while present `delegated=false` Sessions remain unmanaged. Within one
running phase, routine progress cannot move an active lease deadline backward;
a phase transition may establish a shorter lease. User input to the Brain host
has foreground priority, so an internal Event remains queued until that exact
user Turn reaches a terminal transition. The turn-end observer clears the
foreground gate and reserves the next Attention claim while holding the same
admission mutex. A following user send cannot overtake that checkpoint, but
the current response is never preempted and its input is never replayed.

Delegated input carries one durable turn marker on the existing Session input
receipt boundary. A successful tmux submit queue records only dispatched input;
it does not prove that a provider started. The existing provider-native
`ProviderActivity` is the preferred lifecycle fact. When no correlated native
Activity is usable, the same provider-neutral reducer may use post-dispatch pane
activity, stable process identity/liveness, and a persisted wall-clock quiet
boundary. A live dispatch with no observed start
by the existing startup timeout fails once and is never replayed. The reducer
settles the one Session turn marker; it does not create another Work object,
Event source, scheduler, transcript parser, or workflow state machine.

After Brain receives a direct Event input, it re-anchors to foreground Work,
checks the current status and next action, and takes the next useful
orchestration step. This is prompt policy, not automatic daemon workflow. Raw
direct Event input is removed at the user-facing conversation projection only
when it exactly matches Zen's canonical typed input. Malformed, partial,
reordered, padded, or otherwise noncanonical user text remains visible. A typed
disposition is the only operation that marks every applicable fact through the
delivery fence handled and changes Work. Terminal Work can additionally create
an idempotent Session-finalization obligation. Result fact, queued Attention,
admitted handling, handled disposition, and Session finalization are therefore
five observable and non-interchangeable states.

Durable Work result cards remain chronological facts. Their wire projection
adds `work_review_state` (`queued`, `reviewing`, or `resolved`),
`work_session_state` (`open`, `closing`, `finalized`, `close_failed`, or
`not_required`), and `work_result_current`. A historical stale card can remain
visible while a later done card is current; neither card is relabeled as the
other.

Calendar remains authoritative for scheduled-action occurrence claims,
execution, canonical results, source-thread delivery, and recurrence. Brain
only projects a scheduled-action run into deterministic Work and deduplicated
Calendar Events. Due and launch facts are passive; terminal result or failure
facts are actionable. A raw terminal Session fact for Calendar-owned Work is
recorded passively so it cannot race the canonical Calendar result into a
second Brain turn. This projection does not retarget or redeliver Calendar
results.

## Operator surface

Use the existing CLI for inspection and narrow changes:

```text
zen brain work list --json
zen brain work get -id <work_id> --json
zen brain work create -title "<title>" -objective "<outcome>"
zen brain work update -id <work_id> -status <status>
zen brain work event -id <work_id> -kind <kind> -dedupe <key> -actionable

zen agent spawn -name "<name>" -cwd <workspace> -prompt "<task>"
zen agent spawn -work <work_id> -name "<name>" -cwd <workspace> -prompt "<task>"
```

A visible delegated spawn creates bounded Work unless `-work` attaches the
Session to an existing nonterminal Work. Use `-completion until_done` together
with `-done-criteria` only for an explicit verified-completion commitment.
Hidden host Sessions do not create Work.

The Brain snapshot exposes two different read models for the current server:

- `current_work` is a bounded operational relationship projection: present
  delegated owners, present typed Session waits, user/Calendar waits, the live
  handling, the fair next queued Attention window, and present terminal
  finalization obligations.
- `work_backlog` summarizes durable rows excluded from current relationships,
  including queued Attention, historical results, and relationship repair.

Unread terminal history alone is never current execution. Stored owner or wait
strings cannot manufacture an unavailable Session endpoint. The mobile graph
renders `current_work`, labels queued and reviewing truth separately, and shows
the backlog as a compact summary instead of paging through historical
`Blocked -> Unavailable` relationships. Marking a card read changes only its
presentation; it does not handle Attention, change Work completion, or finalize
a Session.

For diagnosis, inspect `orchestration.json`, `zen brain work list --json`, and
`zen agent list --json`. There is no diagnostic command that handles a claimed
Event. A waiting item without queued actionable Attention is intentionally
idle. Do not repair that state by injecting a continuation; record the actual
external fact as a deduplicated Event.
