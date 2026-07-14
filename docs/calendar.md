# Calendar

Calendar is Zen's time view. Brain owns intent and orchestration, Calendar owns when a commitment happens, Work owns execution, and Stats owns history.

## Model

The daemon is the source of truth. It stores a versioned, private document at `~/.zen/calendar/calendar.json`; mobile clients sync snapshots over the authenticated WebSocket. Each item has one explicit kind:

- `event`: `start_at` and `end_at`
- `reminder`: `notify_at`
- `deadline`: `due_at`
- `scheduled_action`: `due_at` and `action_instruction`

The user-facing app and CLI accept a local date, local wall time, and IANA timezone; RFC 3339 instants are only the internal wire/storage representation. DST gaps are rejected explicitly. When a wall time occurs twice during fall-back, the user must choose the first or second occurrence. Recurrence (`none`, `daily`, `weekly`, or `weekdays`) advances in that timezone using calendar dates, preserving the intended local wall clock through daylight-saving changes.

States are `scheduled`, `waiting`, `running`, `completed`, `failed`, and `cancelled`. Reminder and deadline items become waiting when due. Events run during their interval and complete afterward. A scheduled action is atomically claimed before any agent is launched.

## Scheduled actions

A scheduled action creates a visible `calendar_action` Work item and launches it through the configured delegated executor. Launching Work only makes the Calendar item `running`; it is not completion. The scheduler reconciles the linked Work frontmatter and agent lifecycle and records completion or failure only after a terminal signal.

The durable claim is the idempotency boundary. After restart, Zen reconciles a running claim and never launches the same occurrence again. If its linked Work and agent are no longer observable, the item fails with an uncertain-outcome explanation so the user can inspect and explicitly choose Run now.

At startup, a scheduled action missed by no more than 15 minutes runs as catch-up. Older scheduled actions fail visibly instead of unexpectedly starting stale work. Overdue reminders and deadlines remain waiting and visible.

## Brain and API control

The mobile API supports list, get, create, update with revision checking, cancel, and Run now. The same operations are deterministic local control-plane commands:

```text
zen calendar list --json
zen calendar get -id <item-id> --json
zen calendar create -title "Review plan" -kind reminder \
  -date 2026-07-15 -time 09:30 -timezone Asia/Shanghai --json
zen calendar update -item-json '<complete item JSON>' -revision <revision> --json
zen calendar cancel -id <item-id> -revision <revision> --json
zen calendar run -id <item-id> --json
```

Create, update, cancel, get, and run responses include a plain confirmation with the resolved local date, time, timezone, and effect. Brain is instructed to use these tools only for explicit calendar intent, not to extract commitments from every message.

## Device reminders

After a sync, the app schedules the next future reminder as a local device notification. The OS keeps that notification useful if the phone temporarily loses its daemon connection. Cancelling or editing an item resynchronizes the local schedule. Permission denial is non-fatal: Calendar remains usable and explains that notifications are disabled.

V1 does not sync Apple or Google calendars and does not include attendees, shared calendars, meeting rooms, complex colors, or arbitrary automatic scheduling.
