# Security and privacy

## Trust model

Zen separates:

1. **Network reachability** (your tunnel / Tailnet / reverse proxy)
2. **Daemon identity** (Ed25519 keypair in the state directory)
3. **Device authorization** (enrolled phone keys; signed HTTP/WebSocket requests)

Pairing uses a short-lived enrollment token. Normal API traffic uses device signatures bound to the daemon identity you paired with—not a shared password.

Details: [architecture.md](architecture.md).

## What to expose

- Default bind is `127.0.0.1:9876`. Prefer exposing via Tailscale/private mesh when possible.
- If you publish a public HTTPS origin (Funnel, Cloudflare Tunnel, reverse proxy), anyone who can reach it can hit unauthenticated `/health` (daemon id / public key material used for pairing UX). Pairing and authenticated routes still require a valid token or device signature.
- Always forward the **full origin**, not only `/ws`.

## Data locations (daemon host)

| Path | Contents |
| --- | --- |
| `~/.zen/identity.json` | Daemon private key material |
| `~/.zen/trusted-devices.json` | Enrolled phones |
| `~/.zen/pairing-tokens.json` | Short-lived pairing tokens |
| `~/.zen/work/` | File-first work log markdown |
| `~/.zen/brain/` | Brain workspace state |
| `~/.zen/executors.toml` | Optional executor overrides |
| Agent homes (e.g. Codex/Claude/Cursor/Grok dirs) | Transcripts zen may read for Chat |
| `/tmp/zen-uploads` (typical) | Uploaded files from the app |

Treat `~/.zen` like SSH keys: mode `0700` directory, do not commit it, do not share pairing links publicly.

Legacy `~/.config/zen` may still contain copies after migration.

## Mobile app data

The app stores server endpoints and device keys in platform secure storage / app storage. Removing a server in Settings is local; it does not revoke the device on the daemon (see [connect-and-pair.md](connect-and-pair.md)).

## Executor risk

Default and Brain-delegated launch flags can disable sandbox / approval prompts. See [executors.md](executors.md) and [executors.example.toml](../executors.example.toml). This is a user-host trust decision, not a remote Zen cloud.

## Reporting issues

See [SECURITY.md](../SECURITY.md).

## Planned

Device revoke CLI/UI is not shipped yet. `zen doctor` can diagnose readiness (including executor presence); it is not a full security audit.
