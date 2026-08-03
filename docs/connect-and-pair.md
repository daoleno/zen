# Connect and pair

Zen runs agents on your computer; pairing connects the phone app to that
computer. LAN, Tailscale, Cloudflare Tunnel, and reverse proxies are the normal
self-managed paths. **Zen Link** is optional and exists only when an operator
has explicitly configured relay and daemon infrastructure.

- `zen pair` uses configured Zen Link and emits Pairing V2.
- `zen pair <origin>` always keeps the existing Pairing V1 contract for a
  phone-reachable self-managed full origin.

Zen never invents a production Link endpoint. If `<state>/link.json` is absent,
the no-argument command reports how to configure Link or use an explicit
endpoint.

## Model

1. **Reachability** — Link relay or your self-managed network reaches the daemon.
2. **Daemon identity** — persistent Ed25519 keypair under the state directory (`~/.zen` by default).
3. **Device enrollment** — the phone presents its device key once with a short-lived pairing token; later traffic is signed.

There is no long-lived shared secret for normal traffic.

## Optional: Zen Link

Zen Link keeps the daemon loopback listener private. A daemon Connector opens
outbound TLS to one selected regional relay. The relay forwards opaque inner
TLS streams and cannot read Zen HTTP, WSS, Terminal, Chat, or file content.

An operator first supplies `~/.zen/link.json`; this repository does not
hard-code or claim a production service. With that config:

```bash
# Terminal 1
zen

# Terminal 2
zen pair
```

Scan the QR in the Android or iOS app. Pairing V2 dynamically pins the daemon's
inner TLS transport identity and stores all configured relay candidates on the
same daemon/server record. Relay, SNI, and certificate details are not user UX.
Opening the native loopback bridge does not probe the one-time admission. The
first remote stream is the real `POST /pair`; an empty preflight or wrong path
cannot consume the admission, while a completed pairing makes replay fail.

If Link says offline, keep `zen` running and inspect the connector/relay health.
Generating a Link admission requires the daemon Connector to be registered;
`zen pair` fails clearly instead of printing a link that cannot work.

Operator configuration, Docker invocation, health/metrics, rollback, and local
E2E are in [Zen Link Relay operations](zen-link-relay.md).

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

   Do not add a path restriction: Zen needs the full origin, including `/ws`,
   `/health`, `/auth-check`, `/pair`, `/upload`,
   `/session-file-capability`, `/session-file`, and `/devices`.

4. The phone-reachable Zen origin is the exact published hostname with `https`. If the published hostname is `zen.example.com`, the final origin Zen needs is `https://zen.example.com`. Confirm the phone can load `https://zen.example.com/health`, substituting your real hostname, then run:

   ```bash
   zen pair https://zen.example.com
   ```

Cloudflare Tunnel uses outbound connector traffic, so the host does not need a public inbound port. The published hostname is nevertheless internet-reachable unless you add a compatible access layer. Zen exposes unauthenticated `/health` metadata; pairing and normal control routes require short-lived pairing credentials or enrolled-device signatures. Keep pairing links private. A Cloudflare login page is not supported by the Zen mobile client.

## Generate a pairing link

For configured Zen Link:

```bash
zen pair
```

For an explicit LAN/Tailscale/Cloudflare/reverse-proxy origin:

With the daemon already running, pass the origin the phone can actually reach:

```bash
zen pair https://zen.example.com
# Or run the exact private-network command printed by Zen.
```

Use the reachable origin (scheme + host). Explicit endpoint pairing normalizes
HTTP(S) to `ws`/`wss` and uses `/ws` for the unchanged compact V1 `zen://`
payload. The app derives HTTP routes at the origin root, so path-prefixed proxy
origins are not supported.

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

After pairing, reconnect uses the stored server and device identity. Link
remeasures candidates and uses the daemon-signed transport pin. Self-managed
records continue using their explicit URL. You do not need a new pairing link
unless you enroll a new device, rotate the Link transport identity, or clear
state.

## Revoking a device

List and revoke paired devices from the daemon host without editing state files:

```bash
zen devices list
zen devices revoke -id <device-id>
```

Removing a server in Settings only removes the local app record. Revoking a device removes its trusted key, immediately closes its authenticated live connections, and rejects subsequent signed requests. Every currently trusted device may revoke itself or another trusted device; this capability does not define separate administrator roles.

Optional before pairing: `zen doctor` to confirm tmux/state/port/executors on the host.
