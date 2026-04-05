# Architecture

This document defines the current OSS-core architecture for `zen`.

It focuses on the transport, pairing, trust, and runtime boundaries between:

- the mobile app
- the externally reachable network endpoint
- `zen-daemon`
- the local CLI agent processes observed by the daemon

## System Shape

```
[Phone: Expo App]
    ↕ signed HTTP / WebSocket
[Your Tunnel / Tailnet / Reverse Proxy]
    ↕
[zen-daemon]
    ↕ local tmux / watcher integration
[Claude Code] [Codex] [Other CLI Agents]
```

`zen` does not provide a hosted relay in OSS core.
Reachability is delegated to infrastructure the user already trusts, such as:

- Tailscale
- Tailscale Funnel
- Cloudflare Tunnel
- a reverse proxy
- a private mesh or VPN

That keeps the core product focused on identity, pairing, and agent control, not operating a network.

## Design Goals

The current design optimizes for:

1. No hosted control plane requirement.
2. Strong daemon identity, even when the network path is user-provided.
3. One-time pairing, not long-lived shared secrets.
4. A simple mobile UX: print a link, paste it, or scan a QR.
5. Compatibility with whatever network setup the user already has.

## Non-Goals

The OSS core intentionally does not try to solve:

- NAT traversal
- relay routing
- peer-to-peer hole punching
- global service discovery
- centralized device management

Those can exist later, but they are not required for a secure and useful first-principles architecture.

## Trust Boundary

There are three distinct trust layers:

### 1. Reachability layer

This is the external network path that gets the phone to the daemon.
Examples: Cloudflare Tunnel, Tailscale, a self-managed reverse proxy.

This layer answers:

- how does the phone reach the daemon?

This layer does **not** establish application trust by itself.

### 2. Daemon identity layer

Each `zen-daemon` instance has a persistent Ed25519 keypair stored in its state directory.

From that keypair the daemon derives:

- a stable daemon public key
- a stable daemon ID, currently the SHA-256 fingerprint of the daemon public key

This layer answers:

- which daemon is this?

### 3. Device authorization layer

Each mobile app installation creates and persists its own local device keypair.
The daemon only accepts signed requests from devices that were previously enrolled.

This layer answers:

- which phone is allowed to talk to this daemon?

## Network Model

The daemon listens locally by default:

- `127.0.0.1:9876`

The user is expected to expose that origin through their preferred network path.
The app needs more than just the WebSocket endpoint. The externally reachable origin must forward:

- `/ws`
- `/health`
- `/auth-check`
- `/pair`
- `/upload`

This is why `zen` treats the external URL as a full daemon origin, not just a single WebSocket path.

## Startup Modes

`zen-daemon` has two user-facing startup modes.

### `LOCAL-ONLY`

The daemon is running and has a stable identity, but no externally advertised URL was provided.

In this mode:

- agent watching works locally
- no phone can pair yet
- the daemon prints the next step and the `pair` command to run later

### `PAIRABLE`

The daemon was started with an advertised public URL.

In this mode:

- the daemon issues a fresh one-time pairing token
- the daemon prints a pairing link
- the daemon prints a QR code for that link

The `pair` subcommand exists so a fresh link can be generated later without restarting the daemon.

## Pairing Model

Pairing is import-only.

There is no manual shared-secret entry.
The phone imports a daemon-generated `zen://` link by:

- pasting it
- scanning a live QR code
- or scanning a QR from a local image

That link is intentionally self-contained. It contains everything the app needs to:

- know where to connect
- know which daemon public key to trust
- present a one-time enrollment token

## Pairing Link Format

The current pairing link format is:

`zen://settings?p=<payload>`

The payload is a compact, versioned binary blob encoded with URL-safe base64.
It currently contains:

- payload version
- externally reachable daemon URL
- daemon public key
- one-time enrollment token

Important:

- The payload does **not** carry a display name.
- The payload does **not** carry a shared secret.
- The payload does **not** redundantly carry `daemon_id`, because that value is derivable from the daemon public key.

This is smaller and cleaner than a large query string with multiple long field names.

## Pairing Flow

The pairing flow is:

1. `zen-daemon` issues a one-time enrollment token with a short TTL.
2. The daemon prints a compact `zen://settings?p=...` link and QR code.
3. The app imports the link and decodes the payload.
4. The app creates or loads its own persistent local device identity.
5. The app calls `POST /pair` with:
   - the one-time enrollment token
   - the expected daemon public key
   - the local device ID
   - the local device name
   - the local device public key
6. The daemon validates:
   - the token exists
   - the token is not expired
   - the token matches the requested daemon
7. The daemon stores the device as trusted and invalidates the token.
8. The daemon returns its identity and a daemon-signed assertion.
9. The app verifies the daemon assertion against the scanned daemon public key.
10. The app persists the paired server record and starts connecting.

After enrollment, the token is gone. Future requests use device-signed authentication, not the pairing token.

## Request Authentication

After pairing, requests are authenticated by device identity.

The app signs requests using its local device keypair.
The daemon verifies those signatures against the enrolled public key for that device.

The request auth model is bound to:

- a specific daemon ID
- a specific request purpose
- a timestamp
- a nonce

That gives the daemon:

- request authenticity
- replay protection
- daemon binding
- purpose scoping

Current purpose values include:

- `zen-connect`
- `zen-probe`
- `zen-upload`

## Daemon Assertions

Some daemon responses also include daemon-signed assertions.

This matters because the network path is not trusted by default.
Even if the user points the app at the wrong proxy target, the app can still verify:

- whether the daemon identity matches the daemon public key it paired with

This is used on pairing and health/auth-check style probes.

## Storage Model

### Daemon side

The daemon persists security state under its configured state directory:

- daemon private key
- trusted devices
- outstanding pairing tokens

This makes daemon identity stable across restarts and allows the `pair` command to work against an already-running daemon identity.

### App side

The app persists two different classes of state:

- local device identity in `SecureStore`
- paired server metadata in `AsyncStorage`

The local device identity is private, device-local secret material.
The paired server metadata includes:

- server record ID
- display name
- externally reachable URL
- daemon ID
- daemon public key

Current server storage key:

- `zen:v3:servers`

Current local device identity keys intentionally use SecureStore-safe names, for example:

- `zen.device.v3.id`
- `zen.device.v3.seed`

## UX Surface

The current mobile pairing UX is deliberately narrow:

- `Pair Server` accepts a full `zen://...` link
- the scanner can read a QR from the camera
- the scanner can also import a QR from a local image

That keeps the product simple:

- the daemon is the source of truth for trust bootstrap
- the app never asks the user to manually retype key material

## Why No Shared Secret

A shared secret looked simpler, but it had the wrong properties:

- users have to move or type secret material manually
- secrets tend to get reused
- secrets do not identify a daemon instance
- secrets do not identify the device that is making requests

The current design is better because:

- daemon identity is explicit
- device identity is explicit
- pairing is one-time
- normal traffic uses signatures instead of a bearer secret

## Why External Networking Is the Right Default

This architecture is the right default for OSS core because it composes with existing infrastructure instead of competing with it.

Users already have preferences and constraints:

- some use Tailscale
- some use Cloudflare Tunnel
- some already have reverse proxies
- some cannot accept a VPN requirement on mobile

By keeping `zen` transport-agnostic:

- adoption friction stays low
- the product does not need to own relay complexity
- future relay support can still be added without rewriting the trust model

## Operational Rules

To use the system correctly:

1. Expose the full daemon origin, not only `/ws`.
2. Treat the printed pairing link as short-lived enrollment material.
3. Re-import a fresh pairing link if you are pairing a new phone.
4. Keep the daemon state directory stable if you want the same daemon identity after restart.

## Known Tradeoffs

This design is opinionated.
It gives up a few things on purpose:

- no zero-config global connectivity
- no built-in NAT traversal
- no relay fallback
- no centralized revocation UX yet

Those are acceptable tradeoffs for the current stage because the core experience stays understandable and secure.

## Future Work

The next reasonable expansions, if needed later, are:

1. Optional hosted relay, without changing the pairing and device-auth trust model.
2. Better device management UI, such as listing and revoking enrolled devices.
3. Link size improvements beyond the current compact payload, if we ever need a more specialized encoding.
4. Multi-device push registration and richer operational tooling.

The important constraint is this:

Network transport can evolve.
Daemon identity and device-signed authorization should stay the foundation.
