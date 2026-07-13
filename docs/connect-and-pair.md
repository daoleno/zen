# Connect and pair

Zen runs agents on your computer; pairing connects the phone app to that computer. The phone needs a reachable daemon origin first—the pairing QR does not create a network path.

## Model

1. **Reachability** — your tunnel, Tailnet, or reverse proxy reaches the daemon.
2. **Daemon identity** — persistent Ed25519 keypair under the state directory (`~/.zen` by default).
3. **Device enrollment** — the phone presents its device key once with a short-lived pairing token; later traffic is signed.

There is no long-lived shared secret for normal traffic.

## Route A: direct private network

Use this route when the phone and computer are on the same trusted Wi-Fi or connected directly through the same Tailscale Tailnet.

1. Start Zen in private-network mode:

   ```bash
   zen --lan
   ```

2. Zen prints complete pair commands for detected LAN and Tailscale addresses. In another terminal, run the one for the network your phone uses. Example LAN output:

   ```bash
   zen pair http://192.168.1.42:9876
   ```

   A direct Tailnet address looks like:

   ```bash
   zen pair http://100.101.102.103:9876
   ```

3. Scan the QR code or import the printed link in the app.

The pair command must use the computer's actual LAN or Tailscale IP, never `0.0.0.0`. If Zen cannot detect an address, use `hostname -I` on Linux or `ipconfig getifaddr en0` on macOS to find the LAN IP. The phone must be able to reach that IP and port; host firewalls and Wi-Fi client/AP isolation can block it.

Plain LAN HTTP is not encrypted, so use it only on a trusted private network. A direct Tailscale IP is carried inside the encrypted Tailnet, but access still follows your Tailnet membership and ACLs.

## Route B: HTTPS endpoint

Keep Zen in its default local-only mode:

```bash
zen
```

Then forward the whole `http://127.0.0.1:9876` origin through Cloudflare Tunnel, Tailscale Serve/Funnel, or another HTTPS reverse proxy, including:

- `/ws`
- `/health`
- `/auth-check`
- `/pair`
- `/upload`

Do not publish only `/ws`.

Examples using current [Tailscale Serve](https://tailscale.com/docs/reference/tailscale-cli/serve), [Tailscale Funnel](https://tailscale.com/docs/reference/tailscale-cli/funnel), and [Cloudflare Tunnel](https://developers.cloudflare.com/tunnel/setup/) CLIs:

```bash
tailscale serve 9876       # private HTTPS URL inside your Tailnet
tailscale funnel 9876      # public HTTPS URL
cloudflared tunnel --url http://127.0.0.1:9876  # temporary public HTTPS URL
```

Use the HTTPS URL printed by the chosen tool:

```bash
zen pair https://your-zen-host.example
```

Tailscale Serve is limited to your Tailnet; Funnel and Cloudflare public hostnames are internet-reachable. For a stable Cloudflare hostname, configure a named tunnel rather than relying on a temporary Quick Tunnel.

## Generate a pairing link

With the daemon already running, pass the origin the phone can actually reach:

```bash
zen pair https://zen.example.com
# or, on the same trusted LAN:
zen pair http://192.168.1.42:9876
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

There is currently no first-class “forget device” UI/CLI. Trusted devices live in `~/.zen/trusted-devices.json`. Treat a lost phone as: stop exposing the origin, remove or edit that file carefully, and re-pair remaining devices with a fresh `zen pair` link. A proper revoke command is planned but not shipped.

Optional before pairing: `zen doctor` to confirm tmux/state/port/executors on the host.
