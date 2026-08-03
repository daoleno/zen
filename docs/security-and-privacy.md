# Security and privacy

## Trust model

Zen's normal self-managed path separates daemon identity and device
authorization from reachability. Optional Zen Link adds a distinct transport
identity. Across all configured paths, the boundaries are:

1. **Reachability** — Zen Link, LAN, Tailnet, tunnel, or reverse proxy.
2. **Daemon identity** — persistent Ed25519 key under the daemon state directory.
3. **Link transport identity** — separate Ed25519 TLS key and X.509 certificate,
   whose SPKI pin is signed by the daemon identity in Pairing V2.
4. **Device authorization** — enrolled phone Ed25519 keys and purpose-bound
   `ZenDevice` request signatures.

The relay is not an application trust anchor. In hosted mode the phone opens
TLS 1.3 through the relay and verifies the Pairing V2 SPKI pin; only the daemon
possesses that inner TLS key. The relay terminates a separate outer connector
TLS session but sees only encrypted inner records.

An enrolled device that can control a live Terminal is trusted with the host
authority already exposed by that Session. Session File Preview is read-only
and bounded, but it is not a workspace sandbox: absolute paths and symlinks
resolve on the daemon host. Pair only devices you trust with Terminal access.

## Pairing and replay boundaries

- V1 manual pairing and V2 Link pairing both use the existing one-time daemon
  enrollment token (15-minute default).
- V2 adds a random, short-lived relay admission alias. Relay routing reserves
  it for one stream, but only the Connector's daemon-signed confirmation of an
  actual `POST /pair` atomically consumes it. Empty TLS preflights and wrong
  paths release the reservation; expiry, replay, route, daemon, and stream
  binding remain enforced.
- V2 signs daemon ID/key, route, transport pin, all relay candidates,
  admission, enrollment token, and expiry with the daemon identity.
- Connector control requests use short timestamps, random nonces, an
  operator-provisioned service-admission token, and a daemon signature.
- Relay stream attachment tickets are random, short-lived, and single-use.
- Normal requests bind device, daemon, purpose, timestamp, and nonce.

The connector token is not a user password and is not sufficient to decrypt,
pair with, or impersonate a daemon. Keep it secret because it controls who may
consume relay route capacity.

## Relay-visible metadata

The MVP relay necessarily observes:

- connection source/destination IP and port
- connection timing, duration, direction, and byte counts
- aggregate active route/stream counts
- TLS ClientHello routing SNI (an unguessable route/admission label)
- daemon identity and route during authenticated connector registration
- selected protocol version and error class

It must not log or expose as metric labels:

- Pairing/enrollment tokens, connector tokens, stream tickets, or private keys
- Terminal, Chat, Brain, Calendar, Work, or agent messages
- HTTP methods, paths, query strings, headers, device signatures, or cookies
- filenames, host paths, MIME types, file bodies, upload names, or Range values
- daemon IDs, route IDs, admission aliases, SNI values, or device IDs

The shipped operator endpoints expose aggregate metadata only:
`/healthz`, `/readyz`, and `/metrics`.

## Bounds and abuse resistance

The relay enforces:

- bounded client and connector handshake concurrency
- bounded total streams and streams per route
- 64 KiB maximum routing ClientHello and 64 KiB maximum control frame
- TLS/auth, connector attach, and bidirectional idle deadlines
- fixed 32 KiB copy buffers and TCP backpressure
- one-time admissions/nonces/tickets and explicit route conflict rejection
- graceful listener shutdown and connector re-registration after restart

The daemon independently enforces device authorization and route-specific size
limits. A public Link operator still needs network DDoS controls, egress alerts,
per-admission rate policy, token rotation, and regional capacity alarms before
claiming production readiness. The MVP intentionally does not implement user
accounts, billing, or a global abuse database.

## What to expose

- Default daemon bind remains `127.0.0.1:9876`.
- Zen Link opens only outbound connector connections and requires no new daemon
  public port.
- `zen --lan` plus `http://` does not encrypt the LAN hop. Use it only on a
  trusted private network and restrict port 9876 with the host firewall.
- Tailscale IP HTTP remains inside the encrypted Tailnet and its ACL boundary.
- A Cloudflare/reverse-proxy HTTPS origin remains public according to that
  operator's policy and must forward the full origin.
- All transports preserve `/ws`, `/health`, `/auth-check`, `/pair`, `/upload`,
  `/session-file-capability`, `/session-file`, and (for device administration)
  `/devices`.

## Data locations on the daemon host

| Path                                                            | Contents                                                                    |
| --------------------------------------------------------------- | --------------------------------------------------------------------------- |
| `~/.zen/identity.json`                                          | Daemon Ed25519 private key                                                  |
| `~/.zen/link-identity.json`                                     | Link route and TLS Ed25519 private key                                      |
| `~/.zen/link.json`                                              | Optional relay candidates and connector token source                        |
| `~/.zen/trusted-devices.json`                                   | Enrolled device public keys                                                 |
| `~/.zen/pairing-tokens.json`                                    | Short-lived outstanding enrollment tokens                                   |
| `<state>/uploads/`                                              | Authenticated uploads: **2 GiB/file, 8 GiB aggregate, seven-day retention** |
| `~/.zen/work/`, `~/.zen/brain/`                                 | User-owned Work and Brain data                                              |
| Agent homes                                                     | Provider transcripts Zen may read for Chat                                  |
| `~/.zen/worktrees/`, `~/.zen/run/agent-resources/`, `~/.zen/t/` | Explicit agent worktree, lease, and owned temporary state                   |

The previous documentation values of 32 MiB/file and 512 MiB aggregate were
stale. The authoritative daemon constants and app preflight are 2 GiB/file and
8 GiB aggregate. Tests exercise boundaries with limit readers/small fixtures,
not multi-GiB repository artifacts.

Treat the state directory like SSH keys: directory mode `0700`, private files
`0600`, never commit it, and never publish a pairing link.

## Mobile storage and pinned loopback bridge

The device Ed25519 seed is held in platform secure storage. Server identity,
transport candidates, route, and public SPKI pin are app metadata.

For Link, the shared TypeScript owner asks the Android/iOS native module for a
loopback-only local L4 bridge. That bridge opens TLS 1.3 to the selected relay
hostname, sets SNI, verifies SHA-256 of the daemon certificate SPKI in constant
time, and streams bytes with bounded buffers/backpressure. The same bridge is
used by WSS, Pair/Probe HTTP, native streaming upload, and Session File/Range.
It is not a WebSocket-only pin. Pairing starts the bridge in on-demand mode so
creating the local listener cannot spend a one-time admission. Every
post-pairing HTTP/WSS owner accepts the canonical `StoredServer`, not a bare
Link URL; missing route, pin, or candidate availability fails closed with a
Zen Link setup/offline error. Manual V1/self-managed servers continue to return
their original endpoint.

Native image/PDF loaders may issue HEAD, Range, and automatic retries. Zen does
not place a replay-protected ordinary `ZenDevice` nonce in that reusable
resource URL. A fresh signed POST to `/session-file-capability` instead returns
separate two-minute GET and HEAD daemon signatures. Each signature is bound to
daemon, enrolled device, live Session/process/start, exact path, inspected file
generation, method, and expiry. Range may repeat within that exact file and
method scope; changed fields, expiry, or device revocation fail with 401.

Expo Go cannot provide this custom native module. Use a current Android/iOS
development or release build for Zen Link.

## Device revocation

Removing a server in mobile Settings is local and does not revoke its key.
On the daemon host:

```bash
zen devices list
zen devices revoke -id <device-id>
```

The authenticated protocol owner is `GET /devices` with the operation-specific `zen-device-admin:list:GET:/devices` purpose and `DELETE /devices` with `zen-device-admin:revoke:DELETE:/devices:sha256=<digest>`, where the digest binds the trimmed target device ID. Every trusted device may revoke itself or another trusted device; there is no separate administrator role. A successful revocation persists the removed trusted key, immediately closes that device's authenticated WebSockets, and rejects its subsequent signed requests. The CLI asks the running daemon to perform listing and revocation through its private local control socket; it constructs a direct state owner only after an exclusive lifecycle lock proves the daemon is offline.

## Executor risk

Some host executor configurations can disable sandbox or approval prompts.
That is a daemon-host trust choice, not authority granted to the relay. See
[executors.md](executors.md).

Report vulnerabilities through [SECURITY.md](../SECURITY.md).
