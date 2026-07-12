# Connect and pair

Pairing connects one Android installation to one daemon. You need a reachable daemon origin first; the pairing QR does not create a network tunnel.

## Model

1. **Reachability** — your tunnel, Tailnet, or reverse proxy reaches the daemon.
2. **Daemon identity** — persistent Ed25519 keypair under the state directory (`~/.zen` by default).
3. **Device enrollment** — the phone presents its device key once with a short-lived pairing token; later traffic is signed.

There is no long-lived shared secret for normal traffic.

## Expose the full origin

Forward the whole daemon origin to `http://127.0.0.1:9876` (or your chosen `-addr`), including:

- `/ws`
- `/health`
- `/auth-check`
- `/pair`
- `/upload`

Do not publish only `/ws`.

Examples:

```bash
tailscale funnel --https=443 http://127.0.0.1:9876
cloudflared tunnel --url http://127.0.0.1:9876
```

## Generate a pairing link

With the daemon already running:

```bash
zen pair https://zen.example.com
```

Use the publicly reachable origin (scheme + host, optional path). `zen pair` normalizes HTTP(S) to `ws`/`wss` and ensures a `/ws` WebSocket path for the compact `zen://` payload. You still must expose the non-WebSocket HTTP routes on that same origin.

If you use a custom state directory:

```bash
zen pair -state-dir /path/to/state https://zen.example.com
```

Pairing tokens expire (default TTL is 15 minutes). Generate a fresh link for each new phone.

## Import on the phone

In the Android app Settings:

- paste the printed `zen://...` link
- scan the QR
- import a screenshot/photo of the QR
- or use clipboard import

The release APK can paste links, scan a QR code, import a QR image, and open `zen://` links. Expo Go is a development-only path and is not required for normal installation.

## Reconnect

After pairing, reconnect uses the stored server URL and device identity. You do not need a new pairing link unless you enroll a new device or clear app/daemon state.

## Revoking a device

There is currently no first-class “forget device” UI/CLI. Trusted devices live in `~/.zen/trusted-devices.json`. Treat a lost phone as: stop exposing the origin, remove or edit that file carefully, and re-pair remaining devices with a fresh `zen pair` link. A proper revoke command is planned but not shipped.

## App onboarding vs README

The in-app first-run steps match this flow: start `zen`, expose the origin, run `zen pair <origin>`, then import the link.

Optional before pairing: `zen doctor` to confirm tmux/state/port/executors on the host.
