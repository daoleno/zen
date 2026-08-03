# Zen Link Relay operations

This runbook describes an optional single-region MVP. All Link source and
deployment examples are inert until an operator explicitly supplies
configuration, credentials, certificates, DNS, and starts them. This
repository does not deploy or start a relay and does not claim that a
production endpoint, account system, global SLO, or backbone exists.

## Ports and prerequisites

The relay has three listeners:

| Listener                   |          Default | Exposure                                    |
| -------------------------- | ---------------: | ------------------------------------------- |
| opaque mobile L4           |           `:443` | public TCP; wildcard client DNS points here |
| connector control/data TLS |          `:8443` | reachable from daemon hosts                 |
| operator health/metrics    | `127.0.0.1:8080` | private monitoring only                     |

The control listener needs a normal CA-valid TLS certificate/key. Each client
candidate needs a wildcard DNS record such as `*.us1.link.example` reaching the
raw L4 listener. The wildcard name is routing only: the certificate inside that
connection is daemon-generated and Pairing V2 pinned, not a relay wildcard
certificate.

Generate a high-entropy operator connector token using your normal secret
manager. Do not put it in source control, shell history, images, logs, pairing
links, or mobile config.

## Build and run

From the repository root:

```bash
docker build -f daemon/Dockerfile.relay -t zen-relay:local .
```

Direct binary:

```bash
cd daemon
go build -o ../bin/zen-relay ./cmd/zen-relay

ZEN_LINK_CONNECTOR_TOKEN='from-your-secret-manager' \
../bin/zen-relay \
  -client-addr=:443 \
  -control-addr=:8443 \
  -operator-addr=127.0.0.1:8080 \
  -tls-cert=/secure/path/control.crt \
  -tls-key=/secure/path/control.key
```

Compose:

```bash
cd deploy/zen-link
ZEN_LINK_CONNECTOR_TOKEN='from-your-secret-manager' docker compose up --build
```

The Compose file maps public TCP `443` to unprivileged container port `9443`;
the distroless process remains non-root. The direct binary defaults to `:443`,
so a bare-host operator should either set `-client-addr` behind a load balancer
or grant only the platform's narrow low-port binding capability.

Place `tls.crt` and `tls.key` under `deploy/zen-link/secrets/` only for local
operator testing; that directory must remain untracked. In production mount
from a secret manager instead.

## Daemon configuration

Create `<state-dir>/link.json` (default `~/.zen/link.json`) with mode `0600`:

```json
{
  "version": 1,
  "connector_token_env": "ZEN_LINK_CONNECTOR_TOKEN",
  "max_streams": 32,
  "relays": [
    {
      "name": "us-east",
      "control_address": "control.us-east.link.example:8443",
      "control_server_name": "control.us-east.link.example",
      "client_domain": "us-east.link.example",
      "client_port": 443
    },
    {
      "name": "eu-west",
      "control_address": "control.eu-west.link.example:8443",
      "control_server_name": "control.eu-west.link.example",
      "client_domain": "eu-west.link.example",
      "client_port": 443
    }
  ]
}
```

For a private/local CA, set `control_ca_file`; relative paths resolve against
the config directory. `connector_token` is supported for constrained local
testing, but `connector_token_env` is preferred. The two are mutually
exclusive.

Start the daemon with that environment:

```bash
ZEN_LINK_CONNECTOR_TOKEN='from-your-secret-manager' zen
```

An explicit config elsewhere uses `zen -link-config /path/link.json`. The
daemon continues listening on loopback and opens only outbound Link sockets.
After it connects:

```bash
ZEN_LINK_CONNECTOR_TOKEN='from-your-secret-manager' zen pair
```

If Link is not configured, use `zen pair <origin>`; it remains Pairing V1.

## Health, readiness, and metrics

```bash
zen-relay -check=http://127.0.0.1:8080/healthz
curl -fsS http://127.0.0.1:8080/readyz
curl -fsS http://127.0.0.1:8080/metrics
```

Only these aggregate series are exported:

- active routes
- active streams
- accepted streams total
- rejected connections total
- forwarded bytes total

Never add route/SNI/daemon/device/path/token/filename labels. Alert on
readiness failure, restart loop, sustained rejection growth, connection
capacity, file-egress growth, and connector reconnect SLO.

## Limits and cost model

Important flags:

- `-max-clients`
- `-max-clients-per-route`
- `-max-client-handshakes`
- `-max-connector-handshakes`
- `-handshake-timeout`
- `-attach-timeout`
- `-idle-timeout` (fires only when neither stream direction makes progress;
  one-way transfers remain active and half-closed requests may drain responses)

Cost is principally public egress bytes, concurrent client + connector sockets,
kernel memory, TLS CPU, and regional load-balancer/DDoS service. Large file
traffic can dominate cost: the daemon permits 2 GiB per upload, 8 GiB aggregate
retained storage, and seven-day retention. Relay copies are streaming and do
not store the body.

## Local black-box E2E

The automated test starts real TCP listeners, relay, connector, daemon-side
inner TLS, and full-origin handler:

```bash
cd daemon
go test -run TestOpaqueRelayConnectorHealthAndAdmissionReplay -v ./link

# Also execute the real TS importConnection owner through an on-demand
# loopback pinned-transport adapter, the same Relay/Connector, and daemon /pair.
ZEN_LINK_MOBILE_RELAY_E2E=1 \
  go test -run TestOpaqueRelayConnectorHealthAndAdmissionReplay -v ./link
```

It proves:

- empty TLS preflight and a wrong HTTP path do not consume admission
- the actual `POST /pair` atomically consumes admission and replay fails
- wrong SPKI pin fails
- `/pair`, `/auth-check`, `/health`, `/upload`,
  `/session-file-capability`, `/session-file` Range, and `/ws` traverse opaque
  relay streams
- a blocked upload does not prevent a Terminal-like WebSocket round trip
- relay metrics contain aggregate metadata, not the payload sentinel

Security/unit coverage also includes expired/replayed connector auth, wrong
daemon binding, stream-ticket replay, unknown/expired route admissions,
slowloris deadline, and over-concurrency rejection:

```bash
cd daemon
go test ./relay ./link ./auth ./server
go test -race ./relay ./link
```

No multi-GiB fixtures are created.

## Single-region rollout and rollback

Rollout order:

1. Run one relay process in one region.
2. Validate health/readiness, metadata-only metrics, admission, pair, probe,
   WSS/Terminal, upload, and Range.
3. Distribute `link.json` only to opt-in daemon hosts.
4. Keep explicit V1/LAN/Tailscale/Cloudflare paths unchanged.
5. Add a second region as a Pairing V2 candidate only after connector/mobile
   failover testing.

Upgrade one active single-region process with drain time. SIGTERM stops new
listeners, cancels active forwarding, and causes connectors to re-register.
The reconnect target is p95 ≤10 seconds and p99 ≤35 seconds, but that is an
acceptance target, not a deployed SLO claim.

The atomic pairing-admission commit is control protocol/ALPN version 2. Relay
and Connector deliberately reject a mixed v1/v2 control connection instead of
silently losing replay protection. Upgrade the single-region Relay and opted-in
daemons in the same maintenance window; self-managed transports do not depend
on that window.

Rollback:

1. Remove/rename `link.json` and restart the daemon in the operator's normal
   maintenance window, or launch without `-link-config`.
2. Existing self-managed server records and Pairing V1 links continue working.
3. Stop the relay after connectors drain.
4. Do not delete daemon identity, trusted-device, or Link identity state merely
   to roll back transport. Preserving the Link identity avoids needless pin
   rotation if Link is re-enabled.

## Multi-region boundary

MVP route presence is process memory. One active process per region is the
honest deployment. Candidate failover works across separately named regions;
it does not make multiple replicas behind one uncoordinated load balancer safe.
A later route-owner directory or deterministic shard is required before
horizontal scale.

GeoDNS/Anycast may steer a region hostname to its regional process, with
`/healthz` and `/readyz` as probes. Keep each candidate hostname explicit in
Pairing V2. Do not build a custom backbone, SD-WAN, or P2P/NAT traversal layer
for this MVP.
