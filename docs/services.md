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

## Temporary public Quick Tunnels

A service row can start, inspect, open, copy or stop one temporary Cloudflare Quick
Tunnel. Starting is explicit and exposes only that selected service. Zen confirms
HTTP with a bounded local request; a TCP listener alone is not HTTP evidence.
Authentication challenges and known control services are not publishable.

Each tunnel binds to the current daemon, service ID, listener PID and process birth
identity. Duplicate starts return the existing operation. A private loopback proxy
rechecks the origin's process and listener before and after connecting, so a reused
port cannot silently become the tunnel's new application. Origin loss stops the
tunnel and clears its URL. Stopping a tunnel never stops the origin. Linux binds
cloudflared to daemon death; restart begins with no tunnel and no stale URL. Other
daemon operating systems report that this lifecycle mode is unsupported.

Zen passes an isolated empty config and filters Cloudflare-specific inherited
configuration variables. It does not modify `~/.cloudflared/config.yaml`, named
tunnel credentials, system units or unrelated cloudflared processes. Temporary
URLs are kept in memory, not the repository or persistent service registry. The
installed binary must support `--output json`; readiness requires its native
connection confirmation and DNS publication as well as the generated URL.
While publication is pending, Open/Copy remain unavailable. A bounded publication
timeout stops the tunnel and offers an actionable retry instead of a stale URL.

Quick Tunnel URLs are random and temporary. Anyone with the URL can access the
selected service. Cloudflare limits Quick Tunnels to 200 in-flight requests and
does not support Server-Sent Events (SSE). HTTP and WebSocket behavior must be
verified on the deployment's network; receiving a URL is not public reachability
proof. Apps requiring SSE should use their private connection or an appropriate
managed hosting/tunnel configuration.

The existing CLI exposes the same owner:

```sh
zen service list --json
zen service tunnel start -id SERVICE_ID -generation PROCESS_GENERATION
zen service tunnel status -id SERVICE_ID -generation PROCESS_GENERATION
zen service tunnel stop -id SERVICE_ID -generation PROCESS_GENERATION
```

Use the exact ID and generation from the current Services snapshot; stale rows
cannot start a tunnel on a replacement process.
