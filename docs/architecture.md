# Architecture

Zen is a mobile-native control plane for coding agents that continue to run on
the user's computer. The daemon remains the business-logic and data owner.
Self-managed Pair/direct connectivity is the normal path. Explicitly configured
Zen Link adds an optional reachability path; it does not move Sessions,
Terminal, Brain, Chat, Calendar, Work, files, or credentials into the relay.

## System shape

```text
                          operator-only HTTPS
                          /healthz /readyz /metrics
                                      │
[Android / iOS app]                   │
  shared StoredServer owner           │
  native SPKI-pinned TLS              │
          │                           │
          │ inner TLS 1.3 carrying HTTP/1.1 + WSS
          ▼                           ▼
┌──────────────── Regional Zen Link Relay ────────────────┐
│ bounded TLS ClientHello SNI routing only                │
│ opaque bidirectional L4 streams; no inner TLS keys      │
│ in-memory route presence and one-time admissions        │
└───────────────┬─────────────────────────────────────────┘
                │ outer TLS 1.3, daemon-initiated only
                │ one control connection + one outbound data
                │ connection per mobile stream
                ▼
        [daemon Link Connector]
                │ terminates the pinned inner TLS
                ▼
        [the existing daemon HTTP handler]
 /ws /pair /auth-check /upload /session-file-capability /session-file /health /devices
                │
                ▼
 [tmux, agent CLIs, repositories, daemon-local state]
```

LAN, Tailscale, Cloudflare Tunnel, and reverse-proxy origins continue to call
the exact same daemon HTTP handler without Zen Link. The daemon opens no new
public listener for Link.

## Durable invariants

1. Exactly one `StoredServer` is current. Link regions and future direct paths
   are transport candidates of that record, never extra servers and never a
   feature-local server picker.
2. The daemon is the sole application endpoint. Relay loss cannot make a
   self-managed/LAN transport depend on the relay.
3. The relay has no inner TLS private key and cannot read HTTP paths, headers,
   device signatures, WebSocket messages, Terminal bytes, prompts, or files.
4. Existing Ed25519 daemon identity and enrolled device keys remain the
   application trust anchors. A separate Link TLS key is explicitly signed by
   the daemon identity in Pairing V2.
5. Pairing admissions, enrollment tokens, stream tickets, auth nonces, and
   device signatures are purpose-bound and replay resistant.
6. Android and iOS consume the same TypeScript transport contract. Only the
   TLS/SPKI socket bridge is platform native.

## Zen Link relay protocol

The MVP deliberately uses standard, widely implemented primitives:

| Boundary                       | Protocol                                                       | Reason                                                                                        |
| ------------------------------ | -------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| Phone to daemon, through relay | TLS 1.3 with X.509 Ed25519 certificate and Pairing V2 SPKI pin | True end-to-end confidentiality/authentication; relay has no inner key                        |
| Inner application stream       | HTTP/1.1, WebSocket Upgrade, Range and streaming bodies        | Preserves every current full-origin route without an application rewrite                      |
| Connector control/data         | TLS 1.3 TCP, ALPN/frame protocol version 2                     | Length-bounded control, atomic pairing-admission commit, and explicit mixed-version failure   |
| Relay routing                  | bounded TLS ClientHello SNI inspection                         | Routes before forwarding while preserving the complete inner ClientHello                      |
| Each client stream             | a fresh daemon-outbound connector data connection              | Natural stream isolation and backpressure; large upload cannot block Terminal in a shared mux |

The relay does not terminate the inner TLS. Its stable SNI label is a random
128-bit route ID. Pairing uses a separate random 128-bit admission alias that
expires. A client TLS connection first reserves the alias; an empty TLS
preflight or a non-`POST /pair` request releases that reservation. The
Connector commits consumption through a daemon-signed
route+alias+stream control message only after it receives the actual pairing
request. The relay performs that commit atomically, so concurrent use and
post-pair replay still fail without the relay reading inner HTTP. Once routed,
the relay sends the connector a random stream ID and 256-bit single-use
attachment ticket.

Connector registration and admission requests carry:

- protocol version
- random route and nonce
- daemon ID and public key
- short timestamp
- operator-provisioned connector admission token
- an Ed25519 daemon signature over a domain-separated canonical frame

The operator token is an MVP service-admission control, not application trust.
It is never sent to the phone and cannot impersonate a daemon or decrypt a
stream. There is intentionally no user-account database or global control plane
in this slice.

### Backpressure and isolation

Relay copies use fixed 32 KiB buffers, one shared last-progress clock for both
directions, socket read/write deadlines, bounded client/route/handshake
concurrency, and no unbounded stream queue. Only a stream with no progress in
either direction reaches the idle timeout. A one-sided EOF propagates
`CloseWrite` and leaves the reverse direction open to drain a delayed response.
TCP backpressure reaches the sender. Each HTTP or WebSocket connection has its
own outer connector socket, so a large `/upload` body is independent of a live
Terminal WebSocket. Existing daemon uploads are HTTP, while Terminal remains on
`/ws`; Link preserves that separation.

The MVP does not add application-level resume to protocols that do not already
have it:

- WebSocket reconnect re-authenticates and the app refreshes canonical current
  server snapshots.
- HTTP Range lets Session File Preview resume/read bounded byte ranges. The app
  spends a fresh ordinary nonce once on `/session-file-capability`, then native
  GET/HEAD/Range and retry use a two-minute daemon-signed capability bound to
  device, live Session/process/start, path, generation, and HTTP method.
- Upload retry restarts the request; the server does not claim resumable upload.
- Terminal latency comes from an independent stream, not priority scheduling in
  a custom multiplexor.

## Pairing versions

### Pairing V1 — preserved

`zen pair <origin>` remains the existing compact binary V1 payload. It includes
the full phone-reachable `/ws` URL, daemon public key, and one-time enrollment
token. HTTP(S) is normalized to WS(S); the app derives the other root routes.
This is the contract for LAN, Tailscale, Cloudflare Tunnel, and reverse proxies.

### Pairing V2 — Zen Link

When `link.json` exists, `zen pair` with no endpoint requests a short-lived
relay admission and emits a JSON V2 payload inside URL-safe base64. It includes:

- daemon ID and public key
- one-time daemon enrollment token and expiration
- stable unenumerable route ID
- relay candidate names, one admission URL, and stable URLs
- Link transport SPKI SHA-256 pin
- daemon Ed25519 signature over every field above

The app verifies this binding before opening the pinned transport. For the
admission URL it starts only the loopback listener; it does not open a remote
TLS preflight. The actual `POST /pair` creates the first admission-bearing
stream. After enrollment it stores stable relay candidates on the same
`StoredServer`. Re-importing Link for the same daemon updates/reuses that server
record instead of creating a region-specific duplicate.

## Request authentication and revocation

After enrollment, HTTP and WebSocket operations keep the current `ZenDevice`
signature contract. Signatures bind daemon ID, device ID, purpose, timestamp,
and nonce. `/auth-check` and the following `/ws` probe are distinct requests
and therefore each build a fresh timestamp, nonce, and signature. Session File
capability issuance also spends one fresh ordinary nonce; the resulting
short-lived GET/HEAD signatures are deliberately repeatable for native Range
recovery and are rejected after expiry, device revocation, or any bound-field
change. Current purposes cover connection, probe, upload, Session File, and
device administration. Daemon response assertions still prove the paired
daemon on Pair, Health, and probes.

`zen devices list` and `zen devices revoke -id <device-id>` provide the minimum
host owner for revocation. When the daemon is running, revoke goes through its
mode-0600 local control socket so an in-flight authorization update cannot
resurrect a stale key; offline revoke updates the same state directly.
Authenticated `GET /devices` and `DELETE /devices` provide the network protocol
owner. Revocation removes the key from `trusted-devices.json`; subsequent signed
requests fail immediately.

## Relay selection and failure

The daemon tries configured candidates, measures completed control TLS plus
registration RTT, deterministically selects lowest RTT (name/address breaks a
tie), closes unused registrations, and keeps one primary control connection.
On loss it performs deterministic bounded exponential reconnect and remeasures.
A route conflict is explicit and never silently replaces an existing daemon.

The app probes all stable candidates through the same native pin owner, chooses
the lowest successful RTT deterministically, and caches that selection briefly.
If the connector has failed over to another configured relay, the next mobile
selection reaches the new route. Manual candidates are not silently mixed into
Link failover; the user selects a self-managed record/path explicitly.

Route presence is in memory. A relay restart deliberately disconnects routes;
connectors re-register. A single-region MVP therefore runs one relay process
(or one active replica). Horizontal scale requires a later minimal route-owner
directory or deterministic connection sharding so a client reaches the same
process as its connector. This repository does not pretend that a load
balancer alone solves that state placement.

## Region model

The shipped unit is region-neutral. Each candidate declares:

- connector control address and certificate name
- client wildcard domain and port
- stable operator name

A production operator can place one active relay in each selected region and
use GeoDNS/Anycast only to steer a region-specific hostname to that region.
The candidate hostname remains explicit in Pairing V2, so failover does not
depend on hidden DNS substitution. Health checks use `/healthz`; traffic
readiness uses `/readyz`. Connector registrations are not a readiness
requirement because an empty relay must still accept new routes.

Initial service objectives to validate before any production claim:

- relay process availability: 99.9% monthly per region
- successful registered-route connection setup: p95 ≤ 1 s in-region, p99 ≤ 3 s
- relay-added steady-state Terminal RTT: p95 ≤ 25 ms within the selected region
  (measured against a direct baseline)
- reconnect after process restart: p95 ≤ 10 s, p99 ≤ 35 s
- wrong pin, expired/replayed admission, and unknown route acceptance: zero
- metadata/content leakage in relay logs and metrics: zero

These are acceptance targets, not claims that a production network exists.

## Protocol choices intentionally deferred

| Candidate             | MVP decision                                                                          |
| --------------------- | ------------------------------------------------------------------------------------- |
| WebSocket over TLS    | Keep as the inner real-time application protocol                                      |
| HTTP/2 CONNECT        | Not needed; mobile and daemon already share a full HTTP/WSS origin                    |
| HTTP/3 / WebTransport | Prototype later only if cellular loss/hand-off data proves benefit                    |
| custom QUIC mux       | Not in MVP; would duplicate congestion, resume, and mobile work                       |
| WireGuard / DERP      | Not selected; would turn Link into a device VPN/overlay and expand platform/ACL scope |
| ICE / STUN / TURN     | Future direct-path optimization only; NAT traversal must not block relay MVP          |
| P2P hole punching     | Explicitly out of scope for this slice                                                |

## Storage

Daemon state adds:

- `<state>/link.json` — optional operator connector configuration; may name an
  environment variable instead of embedding its token
- `<state>/link-identity.json` — stable random route and Link Ed25519 transport
  private key, mode `0600`

Existing identity, pairing token, trusted-device, upload, Work, Brain, Calendar,
and agent state remain owned by the daemon. The relay has no database and
persists none of them.

The app keeps its Ed25519 device seed in secure storage. AsyncStorage keeps one
server record with daemon trust plus optional Link route, SPKI pin, and
transport candidates.

## Capacity and cost drivers

Relay capacity is dominated by concurrent TCP/TLS sockets, file-transfer
egress, kernel buffers, and encryption for the outer connector TLS. Inner
encryption happens on phone and daemon. Metadata metrics expose only aggregate
active routes/streams, accepted/rejected connection counts, and total forwarded
bytes. Route IDs, SNI, daemon IDs, filenames, paths, request headers, tokens,
and content must never be metric labels or logs.

The current daemon upload policy is **2 GiB per file**, **8 GiB aggregate
stored uploads**, and seven-day retention. Tests use bounded readers and small
fixtures; no multi-GiB artifact belongs in the repository.
