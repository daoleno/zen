# Connect and pair

Zen runs agents on your computer; pairing connects the phone app to that computer. Choose one route below and make the daemon origin reachable from the phone before pairing.

`zen pair <origin>` records that phone-reachable daemon origin in the pairing payload and issues short-lived pairing credentials. It does not create or verify a LAN, tailnet, tunnel, DNS record, or domain. Test reachability through the chosen route separately.

## Model

1. **Reachability** — your tunnel, Tailnet, or reverse proxy reaches the daemon.
2. **Daemon identity** — persistent Ed25519 keypair under the state directory (`~/.zen` by default).
3. **Device enrollment** — the phone presents its device key once with a short-lived pairing token; later traffic is signed.

There is no long-lived shared secret for normal traffic.

## 1. Same trusted Wi-Fi

This is the shortest route when the phone and computer are on the same trusted Wi-Fi.

1. Start Zen in private-network mode:

   ```bash
   zen --lan
   ```

2. Zen prints complete commands for the addresses it detects. In another terminal, run the exact **Same Wi-Fi/LAN** `zen pair` command it printed. Never substitute `0.0.0.0`.

3. Scan the QR code or import the printed link in the app.

The phone must be able to reach the host address and port `9876`; host firewalls and Wi-Fi client/AP isolation can block it. Plain LAN HTTP is not encrypted, so use this route only on a private network you trust. `zen --lan` listens on every IPv4 interface, not only Wi-Fi.

## 2. Tailscale across networks

Use Tailscale when the phone should connect privately from cellular, another Wi-Fi network, or another location.

1. [Install Tailscale](https://tailscale.com/docs/install) on the daemon host and phone, sign both into the same tailnet, and make sure your tailnet grants allow the phone to reach the host.

2. Bind Zen only to the host's Tailscale IPv4 address:

   ```bash
   zen -addr "$(tailscale ip -4):9876"
   ```

   The [`tailscale ip -4`](https://tailscale.com/docs/reference/tailscale-cli#ip) command returns the host's address. Before pairing, confirm the phone can load `/health` at the origin Zen printed. Then run the corresponding `zen pair` command in another terminal and scan or import the result on the phone.

Direct Tailscale HTTP stays inside Tailscale's encrypted network and is not public internet traffic. Tailnet membership and grants remain the access boundary. Binding the Tailscale address specifically avoids also exposing port `9876` on the local LAN.

## 3. Stable HTTPS with Cloudflare Tunnel

Use this route when the phone needs a stable HTTPS domain and installing Tailscale on the phone is not desired. The domain must be active on Cloudflare.

1. Keep Zen on its default loopback-only origin:

   ```bash
   zen
   ```

2. Follow Cloudflare's current [Create a tunnel (dashboard)](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/create-remote-tunnel/) steps: create a named tunnel, run the displayed `cloudflared` connector command on the Zen host, and add a **Published application** route.

3. Set the route's public hostname to the stable name you chose and its Service URL to:

   ```text
   http://127.0.0.1:9876
   ```

   Do not add a path restriction: Zen needs the full origin, including `/ws`, `/health`, `/auth-check`, `/pair`, `/upload`, and `/devices`.

4. The phone-reachable Zen origin is the exact published hostname with `https`. If the published hostname is `zen.example.com`, the final origin Zen needs is `https://zen.example.com`. Confirm the phone can load `https://zen.example.com/health`, substituting your real hostname, then run:

   ```bash
   zen pair https://zen.example.com
   ```

Cloudflare Tunnel uses outbound connector traffic, so the host does not need a public inbound port. The published hostname is nevertheless internet-reachable unless you add a compatible access layer. Zen exposes unauthenticated `/health` metadata; pairing and normal control routes require short-lived pairing credentials or enrolled-device signatures. Keep pairing links private. A Cloudflare login page is not supported by the Zen mobile client.

## Generate a pairing link

With the daemon already running, pass the origin the phone can actually reach:

```bash
zen pair https://zen.example.com
# Or run the exact private-network command printed by Zen.
```

Use the reachable origin (scheme + host). `zen pair` normalizes HTTP(S) to `ws`/`wss` and uses `/ws` for the compact `zen://` payload. The app derives the HTTP routes at the origin root, so path-prefixed proxy origins are not supported; forward every listed route on the same host.

If you use a custom state directory:

```bash
zen pair -state-dir /path/to/state https://zen.example.com
```

Pairing tokens expire (default TTL is 15 minutes). Generate a fresh link for each new phone.

## Import on the phone

In the Android or iOS app Settings:

- paste the printed `zen://...` link
- scan the QR
- import a screenshot/photo of the QR
- or use clipboard import

The native app can paste links, scan a QR code, import a QR image, and open `zen://` links. Expo Go is a development-only path and cannot exercise the custom Ghostty native module; use an APK or iOS development build for complete validation.

## Reconnect

After pairing, reconnect uses the stored server URL and device identity. You do not need a new pairing link unless you enroll a new device or clear app/daemon state.

## Revoking a device

List and revoke paired devices from the daemon host without editing state files:

```bash
zen devices list
zen devices revoke -id <device-id>
```

Removing a server in Settings only removes the local app record. Revoking a device removes its trusted key, immediately closes its authenticated live connections, and rejects subsequent signed requests. Every currently trusted device may revoke itself or another trusted device; this capability does not define separate administrator roles.

Optional before pairing: `zen doctor` to confirm tmux/state/port/executors on the host.
