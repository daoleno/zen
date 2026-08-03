# Brain Orchestration

Zen Brain has three orchestration objects: Work, Event, and the existing
Session.

- **Work** is a durable user commitment. It exists only when an outcome must
  survive the current turn. Ordinary questions and discussion do not create
  Work.
- **Event** is an append-only fact about one Work. Only an unconsumed,
  actionable Event can make Brain run automatically.
- **Session** is the existing executor resource and remains authoritative for
  process state, delegation, progress, and leases. Brain manages only Sessions
  marked `delegated=true`.

The complete automatic scheduling rule is:

```text
if an actionable Work Event can be atomically claimed:
    start one Brain turn and consume that Event
else:
    wait
```

Open, running, or waiting Work does not poll Brain. The `until_done` completion
policy requires an explicit done-criteria reference and changes only when Work
may be marked done; it does not manufacture continuation turns. Provider Goal
features may operate inside an individual Session, but they are not Zen
scheduler state and their internal context is removed from user transcript
projections.

## Persistence and ownership

Brain stores a versioned atomic JSON database at
`~/.zen/brain/state/orchestration.json`. Its two logical tables are
`brain_work` and `brain_work_events`. Work carries only title, objective,
status, optional owner Session, completion policy, next action, wait condition,
and a context reference. Long plans and evidence stay in `workspace/worklog/`.

Event dedupe keys are unique within one Work. An actionable Event is durably
claimed for one Brain host Session before input is sent. The stable Event ID is
the optional receipt in a bounded receiver-side tmux ledger. Each retained
receipt carries the payload hash and an accepted or ambiguous outcome. Zen
writes and confirms ambiguity before provider mutation, then promotes that same
entry to accepted only after the submit queue succeeds. Codex, Claude Code,
Cursor, Grok, and future interactive providers share one per-Session input
serializer and one target-bound tmux command queue: replace the current unsent
draft, paste the exact original UTF-8 payload, send the provider adapter's
submit key once, and delete the buffer.

If target identity or pane generation changes before that queue starts,
Session Input reports a definite pre-mutation failure and atomically releases
that exact Event claim. If the queue starts, success or an ambiguous result
retains the claim and is never automatically replayed. The Host later reads and
consumes the identity-bound Event with `zen brain event`. Pane history, rendered
Composer/Footer/ANSI state, pending PTY byte counts, and elapsed time are never
send authorization. There is no transaction journal, spool, background resume
loop, second scheduler, replacement Event, or provider Goal delivery path.

Schema 2 adds host-bound delivery evidence to the existing Event record. Its
one-way schema-1 migration binds an unresolved claim to the persisted host when
available. A migrated unresolved claim remains closed; the migration does not
create a table, copy an Event, or add another delivery path.

On the first authoritative Watcher inventory after an upgrade, schema migration
`delegated_sessions_v1` adopts visible `delegated=true` Sessions that predate
Work. The migration marker is durable and the adoption is one-way. New visible
delegated Sessions create Work before launch and attach through an empty-owner
compare-and-set after Session creation. A concurrent losing spawn terminates
only its newly created Session and preserves the incumbent. A persisted owner
missing from the first authoritative post-restart inventory is atomically
detached into waiting Work plus one deduplicated actionable stale Event.
Present `delegated=false` Sessions are not managed, and no runtime fallback
adopts later unowned Sessions.

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
has foreground priority, so an internal Event remains unclaimed until that user
turn ends.

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
zen brain event --json

zen agent spawn -name "<name>" -cwd <workspace> -prompt "<task>"
zen agent spawn -work <work_id> -name "<name>" -cwd <workspace> -prompt "<task>"
```

A visible delegated spawn creates bounded Work unless `-work` attaches the
Session to an existing nonterminal Work. Use `-completion until_done` together
with `-done-criteria` only for an explicit verified-completion commitment.
Hidden host Sessions do not create Work.

The mobile Brain screen shows the current server's minimal Active work
projection: title, status, owner or wait condition, and unread-result state.
Marking an item read changes only the projection; it does not alter Work
completion or Session lifecycle.

For diagnosis, inspect `orchestration.json`, `zen brain work list --json`, and
`zen agent list --json`. A waiting item without an actionable unconsumed Event
is intentionally idle. Do not repair that state by injecting a continuation;
record the actual external fact as a deduplicated Event.
