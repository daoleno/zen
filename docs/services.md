# Services

The mobile Services sheet lists TCP services the daemon can attribute to
Agent work: listening ports owned by live Zen tmux Sessions, plus explicitly
registered persistent services that outlive their creating Session (for
example a user systemd unit kept running after its Worker was reclaimed).

## What the sheet shows

Every row comes from one authenticated `list_session_services` snapshot, so
the panel, `zen service list --json`, and the daemon agree. Rows carry a
source:

- **Session** — a listening port inside a live Zen-owned tmux pane process
  tree. The row offers a terminal action into that Session.
- **Persistent · `<unit>`** — a port owned by a registered persistent unit,
  verified live on every snapshot against structured user-systemd properties
  (`ActiveState`, `MainPID`) plus cgroup membership and the listening socket
  PID. These rows never carry a Worker id and never offer a terminal action:
  the creating Session is gone, so there is no live target.

Loopback-only services appear with a bind chip (for example
`127.0.0.1:3080`) and `local_only`; the sheet never invents a LAN URL for
them. A registered unit that is stopped appears as `Inactive`; a unit whose
live state cannot be determined appears as `Error` with the reason — never
as a silently healthy row.

## Retained-service handoff (Workers)

A service started outside tmux must be registered at handoff or it stays
invisible. Registration is explicit, read-only toward the unit (it never
restarts, stops, or reconfigures anything), and goes through the daemon:

```sh
zen service register -unit dsh-web.service -name "DeepSeek Harness" \
  -project dsh-smoke -port 3080 -cwd ~/workspace/dsh-smoke
```

Only plain user unit names are accepted — no paths, no name/port guessing,
no machine-wide scan. The daemon persists the descriptor (unit, display
name, project, expected port, owning Worker/Work provenance) in daemon
state, so completed Worker cleanup or a daemon restart cannot lose it.
Liveness is always re-derived: a restarted unit with a new PID is picked up
automatically, and a stopped, replaced, or foreign unit can never produce a
stale positive row.

```sh
zen service list --json        # same authoritative snapshot as the sheet
zen service unregister -unit dsh-web.service   # removes registration only
```

On platforms without Linux user systemd, registration reports an honest
unsupported error; tmux session discovery keeps working.
