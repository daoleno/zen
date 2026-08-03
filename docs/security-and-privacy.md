# Security and privacy

## Trust model

Zen separates:

1. **Network reachability** (your tunnel / Tailnet / reverse proxy)
2. **Daemon identity** (Ed25519 keypair in the state directory)
3. **Device authorization** (enrolled phone keys; signed HTTP/WebSocket requests)

Pairing uses a short-lived enrollment token. Normal API traffic uses device signatures bound to the daemon identity you paired with—not a shared password.

An enrolled client that can control a live Session Terminal is trusted with that Session's host-file authority. Session File Preview is read-only and size-bounded, but it is not a workspace sandbox: absolute references resolve to their canonical host files, relative references resolve from the exact live Session CWD, and filesystem aliases or symlinks resolve to the same canonical file. Pair only devices that you trust with the host access already exposed by live Terminal control.

Details: [architecture.md](architecture.md).

## What to expose

- Default bind is `127.0.0.1:9876`. Prefer exposing via Tailscale/private mesh when possible.
- Direct private-network mode (`zen --lan` with an `http://` pairing origin) does not encrypt plain LAN traffic. Use it only on a trusted private network and restrict port `9876` with the host firewall. Direct Tailscale IP traffic remains inside the encrypted Tailnet and is subject to its membership and ACLs.
- If you publish a public HTTPS origin (Funnel, Cloudflare Tunnel, reverse proxy), anyone who can reach it can hit unauthenticated `/health` (daemon id / public key material used for pairing UX). Pairing and authenticated routes still require a valid token or device signature.
- Always forward the **full origin**, not only `/ws`.

## Data locations (daemon host)

| Path                                             | Contents                                                                                                                                                                   |
| ------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `~/.zen/identity.json`                           | Daemon private key material                                                                                                                                                |
| `~/.zen/trusted-devices.json`                    | Enrolled phones                                                                                                                                                            |
| `~/.zen/pairing-tokens.json`                     | Short-lived pairing tokens                                                                                                                                                 |
| `~/.zen/work/`                                   | File-first work log markdown                                                                                                                                               |
| `~/.zen/brain/`                                  | Brain workspace state                                                                                                                                                      |
| `~/.zen/executors.toml`                          | Optional executor overrides                                                                                                                                                |
| `~/.zen/worktrees/`                              | Optional durable Git worktrees when concurrent writers genuinely require isolation                                                                                         |
| `~/.zen/run/agent-resources/`                    | Delegated-session resource leases                                                                                                                                          |
| `~/.zen/t/`                                      | Exact-owned delegated-session private temporary directories, removed with the owned session; legacy `~/.zen/tmp/agent-resources/` is cleanup-compatible for older sessions |
| `<state directory>/uploads/`                     | Authenticated app uploads (32 MiB/file, 512 MiB aggregate, seven-day retention)                                                                                            |
| Agent homes (e.g. Codex/Claude/Cursor/Grok dirs) | Transcripts zen may read for Chat                                                                                                                                          |

Treat `~/.zen` like SSH keys: mode `0700` directory, do not commit it, do not share pairing links publicly.

## Mobile app data

The app stores server endpoints and device keys in platform secure storage / app storage. Removing a server in Settings is local; it does not revoke the device on the daemon (see [connect-and-pair.md](connect-and-pair.md)).

## Device revocation

On the daemon host:

```bash
zen devices list
zen devices revoke -id <device-id>
```

The authenticated protocol owner is `GET /devices` with the operation-specific `zen-device-admin:list:GET:/devices` purpose and `DELETE /devices` with `zen-device-admin:revoke:DELETE:/devices:sha256=<digest>`, where the digest binds the trimmed target device ID. Every trusted device may revoke itself or another trusted device; there is no separate administrator role. A successful revocation persists the removed trusted key, immediately closes that device's authenticated WebSockets, and rejects its subsequent signed requests. The CLI asks the running daemon to perform listing and revocation through its private local control socket; it constructs a direct state owner only after an exclusive lifecycle lock proves the daemon is offline.

## Executor risk

Default and Brain-delegated launch flags can disable sandbox / approval prompts. See [executors.md](executors.md) and [executors.example.toml](../executors.example.toml). This is a user-host trust decision, not a remote Zen cloud.

## Reporting issues

See [SECURITY.md](../SECURITY.md).
