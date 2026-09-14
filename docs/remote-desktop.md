# Remote Desktop

Zen Remote Desktop connects the paired Android or iOS app to the desktop of
the current Zen server. The Linux product route is the logged-in KDE Wayland
owner desktop through the session's xdg-desktop-portal ScreenCast/
RemoteDesktop interfaces, PipeWire, and Zen's existing GStreamer H.264 path.
Android and iOS use the same
pairing, transport, and session contract. Web, audio, clipboard, file
transfer, and multi-monitor composition are outside this feature. On Wayland
the compositor keeps its own screen-sharing consent; Zen never injects input
through a compositor test protocol.

An Xvnc desktop may be used only as an owned, isolated test fixture for the
native frame and input components. It is a separate virtual session and is not
a product backend or a way to control the user's existing desktop.

The KDE Wayland path requires the KDE xdg-desktop-portal backend to be selected
in the logged-in user session. If the capability check reports an unavailable
current session, inspect the user bus before changing Zen state:

```sh
busctl --user introspect org.freedesktop.portal.Desktop /org/freedesktop/portal/desktop | rg 'RemoteDesktop|ScreenCast'
```

Both interfaces must be present. A GTK-only portal selection leaves the host
gate closed even when `loginctl` shows an active KDE Wayland seat; install the
distribution's `xdg-desktop-portal-kde` package, select the KDE backend in the
user portal configuration, and reconnect after the session portal is healthy.

## Quick Start

For an already logged-in Linux X11 desktop, build a stable local ELF once
(native build prerequisites below), then run it from that desktop session.
This does not replace a developer's existing `zen` symlink to the DEV child:

```sh
./scripts/build-zen-local.sh "$HOME/.local/bin/zen-release"
"$HOME/.local/bin/zen-release" --lan
```

Then run the pairing command printed by Zen, scan the resulting link once,
and open **Remote Desktop**. Do not start this alongside a daemon that already
owns the same state/port. `zen-dev` uses the same runtime and native roles while
automatically rebuilding its child when source files change:

```sh
cd daemon
go run ./cmd/zen-dev -lan
```

The normal Connect action needs no second Zen-side Enable or Share step after
pairing. On X11 the daemon starts the selected desktop source itself. On
Wayland the compositor shows its own system permission dialog unless the user
previously granted persistent access there; Zen stores only the portal's
opaque single-use restore token in the owner's state directory and still
requires the paired device scope and a supported deployment boundary on every
connection (built-in TLS/Zen Link, an operator-trusted private network such as
`zen --lan` or a tailnet-bound origin, or a local proxy connector such as
`cloudflared`).
The app shows contextual failure states and keeps the secure OS-password
editor, disconnect, pan, pointer, keyboard, and scroll controls behind the
connection's reported capabilities.

Current-session access ends with that desktop session. Lock-screen, greeter,
logout, and boot-before-login access require the optional Linux host install
below. A user daemon cannot grant itself pre-login access.

## SSH-Started KDE Host And Unattended Authorization

A KDE Wayland owner desktop can also be prepared and supervised from an SSH
login while the owner is already logged into the desktop. The daemon resolves
the active owner session from logind (seat0, owner UID, unlocked) and the
owner's protected runtime directory; it never guesses a DISPLAY, never reuses
an unrelated login environment, and never creates a replacement X11 or xrdp
session. Reboot, SDDM pre-login handoff and remote login-screen unlock are out
of scope for this path; it covers the same logged-in session that is already
running. Run the daemon itself from the owner's systemd user manager, not as a
child of the SSH session; the sleep-inhibitor policy requirement is explained
below.

The supervised host engine is the explicitly configured, Zen-owned host
binary. Zen never guesses a binary or touches a personal Sunshine install. The
manual candidate package includes `setup-manual.sh`; it installs the reviewed
Sunshine ELF, creates `$HOME/.zen/desktop/sunshine.json` with fresh local Web
UI credentials and a stable per-install host key, and sets the required 0700 /
0600 permissions. It never prints the generated password or overwrites an
existing Zen Sunshine config. The generated config uses HTTP port 47989,
application id 1, and a private state directory containing only Zen-owned
certificates, keys, credentials, apps and pairing state.

The daemon starts the host on the first authenticated capability/connect request
and passes the discovered owner session environment (`XDG_RUNTIME_DIR`,
`WAYLAND_DISPLAY`, `DBUS_SESSION_BUS_ADDRESS`) to it, so the host attaches to
that same KDE session. The exact package and setup commands are recorded in the
candidate README; no placeholder values are needed for the trial.

### The One Explicit Authorization

The unattended authorization remains the existing per-device
`desktop_scope_version: 1` grant: the phone opens **Remote Desktop** and chooses
**Enable remote desktop**, confirms once, and the daemon commits the scope to
the same canonical device record. No QR, re-pair, host install or root step is
involved, and cancelling changes nothing. While at least one trusted device
holds that grant and the supervised host is configured, the daemon holds a
scoped authorization/idle/suspend inhibitor tied to the authorization
lifecycle:

| Leg | Supported API | Effect while authorized |
| --- | --- | --- |
| idle | systemd-logind `Inhibit("idle", ..., "block")` | the session never reaches the idle action |
| sleep | systemd-logind `Inhibit("sleep", ..., "block")` | the machine is not suspended |
| lock | KDE `org.freedesktop.ScreenSaver.Inhibit` on the owner session bus | the compositor's automatic lock does not lock the authorized session |

This does not fake input, does not disable the lock or suspend policy, does not
weaken PAM, does not store an OS password, and does not create a second session.
A manual lock (the lock key or `loginctl lock-session`) still works and still
ends remote access, exactly as before. The inhibitor exists so an overnight idle
or a temporary phone disconnect does not lock or suspend the desktop that the
authorized phone will reconnect to.

The sleep leg has a distro policy requirement that decides where the daemon may
run. The stock Ubuntu 24.04 systemd policy ships
`org.freedesktop.login1.inhibit-block-sleep` as `allow_any=no`,
`allow_inactive=yes`, `allow_active=yes`, so polkit allows it only for a
subject it can resolve to a local session. A session without a seat (a bare
SSH/`login` TTY session) and a process in the root slice resolve to
`allow_any` and are rejected. A process with no session of its own may be
resolved by polkit to the owner's active graphical session when it runs in the owner's systemd user manager
(`user-<uid>.slice`), because polkit can fall back to
`sd_pid_get_owner_uid()` and `sd_uid_get_display()`. This is the supported
manual hypothesis for the trial, not a guarantee: inspect `logind_sleep` in the
read-only status output before treating unattended authorization as ready.

Start the long-running daemon from the owner’s user manager, not as a child of
the SSH login session. If a canonical Zen daemon is already running for
`$HOME/.zen`, authorization reuses that daemon and does not start a competing
`zen.service`. If no daemon is running, install the user unit once, then daily
remote desktop use is one command:

```sh
zen boot install --binary "$HOME/.local/bin/zen-release" --state-dir "$HOME/.zen"
zen-remote-desktop desktop-host authorize
```

`zen-remote-desktop desktop-host authorize` verifies the canonical `$HOME/.zen`
identity and proves the live canonical control socket when Zen is already
running. The host source is the current KDE Wayland portal session; Sunshine is
not required for this route. The command reports host preparation only; it does
not claim phone or compositor consent. Finish the
exact action printed by the command in the paired app (**Remote Desktop →
Enable remote desktop → confirm**). Repeating the command is safe.
Disable/revoke all desktop grants with `zen-remote-desktop desktop-host revoke`.

The command does not change the stock polkit rule and does not guarantee that
the sleep leg will be accepted; no root, sudo, PAM or logind policy edit is
performed by this procedure.
The idle and KDE screen-saver legs do not depend on this policy path.
`zen-remote-desktop desktop-host --status` reports each leg separately, and a rejected sleep
leg is shown as `logind_sleep: unavailable` with the raw systemd/polkit reason,
never as active. While the sleep leg is unavailable the authorization stays
inactive (`active: false`), so a phone reconnect is told the desktop is not
ready instead of being promised an unattended session the machine cannot keep;
suspend then remains possible, and the smallest supported setup step is to
restart the daemon from the owner's user manager as shown above.

If the owner session was already locked when authorization was prepared, the
inhibitors cannot unlock it retroactively. Recover the existing seat session
over SSH with the normal logind request, then read back the result:

```sh
session="$(loginctl show-seat seat0 -p ActiveSession --value)"
sudo loginctl unlock-session "$session"
loginctl show-session "$session" -p LockedHint -p State -p Active
zen-remote-desktop desktop-host --status --json
```

The installed policy requires administrator authentication (`auth_admin_keep`)
for `org.freedesktop.login1.lock-sessions`; `sudo` may therefore prompt the
operator, but Zen never asks for or handles an OS password. `unlock-session` is
only a request. `LockedHint=no`, `State=active`, and `Active=yes` in the
read-back, followed by a fresh status record with `active=true`, are the proof
that the gate changed. After that first unlocked authorization, the existing
idle, sleep, and KDE screen-saver legs protect ordinary reconnects; they do not
override a manual lock or PAM policy.

The daemon writes the status record under its desktop state directory. When
`ZEN_STATE_DIR=$HOME/.zen`, Zen resolves it at
`$HOME/.zen/desktop/authorization-status.json`, matching the daemon; the
daemon's `--state-dir` still selects canonical identity/device state. An
explicit `ZEN_DESKTOP_AUTHORIZATION_STATUS` remains available for isolated
diagnostics.

### Status, Stop And Revoke

Check the exact authorization and inhibitor state over SSH at any time; the
command is read-only and acquires nothing:

```sh
zen-remote-desktop desktop-host --status
zen-remote-desktop desktop-host --status --json
```

The daemon publishes an owner-only status record and the command cross-checks
the live kernel-level logind inhibitor. `active` is true only when every leg is
held; each leg is reported separately, and a missing KDE screen-saver interface
or a logind rejection is reported truthfully instead of being hidden.

Revoking the device in the app, running `zen-remote-desktop desktop-host revoke`, or stopping
the supervised host releases every leg and restores the previous system policy;
the CLI revoke clears only desktop scope while preserving pairing. If the
session is locked, a greeter, or belongs to another account, the daemon releases
every leg and re-acquires only when the owner desktop is active and unlocked
again.

### Cloudflare HTTP/2 connector update

The current root unit is `/etc/systemd/system/cloudflared.service`. When its
`ExecStart` still has the expected `cloudflared --no-autoupdate tunnel run`
shape, an administrator may apply this reversible update (Zen does not run it):

```sh
sudo cp -a /etc/systemd/system/cloudflared.service \
  /etc/systemd/system/cloudflared.service.bak-20260914-zen-brain
sudo sed -i \
  's#cloudflared --no-autoupdate tunnel run#cloudflared --no-autoupdate --protocol http2 tunnel run#' \
  /etc/systemd/system/cloudflared.service
sudo systemctl daemon-reload
sudo systemctl restart cloudflared.service
```

Before editing, inspect the unit and stop if the `ExecStart` shape differs;
never copy `/etc/cloudflared/token` into reports. A redacted verification is:

```sh
unit=$(sudo systemctl cat cloudflared.service)
if printf '%s\n' "$unit" | rg -q -- '--protocol[= ]http2'; then
  echo 'cloudflared.service already uses HTTP/2' >&2
  exit 0
fi
if ! printf '%s\n' "$unit" | rg -q '/usr/bin/cloudflared --no-autoupdate tunnel run --token-file /etc/cloudflared/token'; then
  echo 'unexpected cloudflared ExecStart shape; refusing update' >&2
  exit 1
fi
sudo journalctl -u cloudflared.service -n 80 --no-pager | rg 'protocol=http2'
```

## What Is Distributed

There is one distribution executable: a desktop-capable Linux amd64 `zen`
ELF. The ELF contains the ordinary daemon and the `desktop-helper`,
`desktop-host`, and `desktop-agent` roles. It is dynamically linked; it is
not a static standalone file and it does not extract `libzen-desktop.so` or
another helper at startup.

The runtime processes are:

| Process | Purpose | Privilege |
| --- | --- | --- |
| `zen` daemon | HTTP/WebSocket, pairing, Brain, Terminal, and current-session desktop | Normal owner UID |
| `zen-dev` watcher | Development-only rebuild and supervision of the same daemon ELF | Normal owner UID |
| `zen desktop-helper` | Current-session capture, encode, and input | Normal owner UID |
| `zen desktop-host` | Optional broker, installation, and SDDM registration | Root only for the installed broker role |
| `zen desktop-agent` | Per-session capture after broker admission | Session UID; never root |

The optional services are independent. `zen boot install` creates a user
systemd unit for the canonical daemon if boot persistence is wanted.
`zen-desktop-host.service` is the optional root broker for SDDM greeter and
locked/login surfaces. It must not launch a second daemon. A tmux development
watcher is not a boot service and must not be installed as the root broker.

Required runtime packages on the supported Linux host are `tmux`, GTK 3,
GStreamer plus app/video support, GIO Unix, X11, and XTest. The desktop-capable
ELF has `DT_NEEDED` entries for these libraries. GStreamer runtime plugins
also need to be present for the selected pipeline, including the X11 or
PipeWire source and H.264 parser/encoder plugins used by the host. SDDM X11,
systemd logind, and an active `seat0` are required for the optional
boot/greeter path. Zen does not install packages or run `sudo`.

Build and inspect a local ELF:

```sh
"$HOME/.local/bin/zen-release" desktop-identity
"$HOME/.local/bin/zen-release" doctor
gst-inspect-1.0 --exists ximagesrc openh264enc h264parse videoconvert appsink
```

`desktop-identity` prints the resolved executable, SHA-256, native-role flag,
and native build input. `doctor` lists the ELF's dynamic dependencies and
reports the current display, broker, TLS identity, and desktop readiness. If a
desktop-capable ELF is missing a dynamic library, the loader may reject it
before `zen doctor` can start. The installer validates the staged executable
before replacing the old one and reports package prerequisites on failure.
Installing runtime libraries does not require a rebuild of a native-linked
ELF. A CGO-disabled archive remains daemon-only.

Arch/Manjaro build and runtime dependencies:

```sh
sudo pacman -S --needed base-devel go pkgconf tmux gtk3 gstreamer \
  gst-plugins-base gst-plugins-good gst-plugins-bad libx11 libxtst
```

Debian/Ubuntu build and runtime dependencies:

```sh
sudo apt install build-essential golang pkg-config tmux libgtk-3-dev \
  libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev libxtst-dev \
  gstreamer1.0-tools gstreamer1.0-plugins-good gstreamer1.0-plugins-bad
```

SDDM is an additional requirement only for optional greeter setup. Installing
a display manager can change system defaults; do not replace an existing one
just to enable current-session access.

## Pairing And Trust

The first new pairing acknowledges terminal access plus unattended desktop
view/control for the current logged-in session and supported lock/login
surfaces. This is the one meaningful device-scope acknowledgement. Later
Connect actions do not repeat it.

The existing canonical trusted-device record remains the only device trust
store. Revocation removes terminal and desktop access and retires an active
desktop connection. A legacy paired record without `desktop_scope_version: 1`
has terminal and explicitly attended access only. It does not gain unattended
access from a LAN acknowledgement, a reconnect, or host setup.

Instead, the phone grants that existing device in place. In the app, open
**Remote Desktop** and choose **Enable remote desktop**: after one explicit
confirmation naming the server and the view/control permission, the phone signs
one versioned consent request over the identity-bound encrypted transport, the
daemon commits `desktop_scope_version: 1` to the same canonical device record,
and the app re-fetches the capability and connects. No new QR, `zen pair`
token, device record, host install, or re-pair is involved; cancelling changes
nothing. The grant endpoint authenticates the device's own signed purpose over
real TLS and never accepts a target device from the request body, so a device
cannot grant another device and terminal-only records stay terminal-only until
their owner consents.

Unattended desktop requires a supported deployment boundary: identity-bound
TLS, the existing pinned Zen Link, another verified `wss` origin, or an
operator-trusted private network that Zen itself verified. The daemon reports
that decision truthfully per request (`trusted_ingress`) from its configured
`--lan`/tailnet bind and the actual peer address; the app never trusts proxy or
forwarded headers, hostnames, or client JSON as proof. Built-in TLS remains an
option, not a compulsory second tunnel or certificate.

- Same trusted Wi-Fi (`zen --lan`): deliberately unencrypted application HTTP/WS
  on the private network. Device pairing, scope, enrollment proof and
  revocation still apply. Use only on a network you trust.
- Tailscale (`zen -addr "$(tailscale ip -4):9876"`): plain application HTTP
  carried inside Tailscale's WireGuard encryption. Tailnet membership and
  grants are the access boundary.
- Cloudflare Tunnel (public HTTPS -> `http://127.0.0.1:9876`): Cloudflare
  protects the public hop; the local connector hop is HTTP. Zen verifies the
  request as arriving over the trusted loopback/local connector path.
  Trusted HTTP admission on the WS/broker path requires the matching updated
  daemon **and** broker: an older installed broker that predates the trusted
  ingress field rejects it (it still accepts TLS/Zen Link). Updating the root
  broker is a separate approved step; Zen never reports TLS for a plain route. A working
  control/WS route does not by itself prove that Sunshine's native UDP media
  ports are reachable through the tunnel; use LAN/tailnet for the native engine
  or expect the reported native-media limitation.

Untrusted public HTTP without one of these deployment facts stays refused,
including requests that merely set proxy or forwarded headers.
specialized client, remains bound to the exact daemon identity and origin.

## Capability States

These facts are independent and must not be collapsed into a generic
authorization error:

| State | Meaning | Action |
| --- | --- | --- |
| `desktop_scope_required` | Device is legacy terminal-only | Choose **Enable remote desktop** in the app and confirm once; no re-pair or host change |
| `desktop_tls_required` | The request is neither encrypted nor on an operator-trusted deployment path | Use TLS/Zen Link, or start Zen with `--lan`/a tailnet bind trusted for this network |
| `host_setup_required` | Neither this process's current display nor the broker is available | Start Zen inside the logged-in desktop session, or complete host install |
| `connected` | Native decoder has presented the current generation | Use desktop controls; a received sample alone is not connected proof |
| `connected` + `inputError` | The computer rejected one character or key in the current layout | Keep using the stream and adjust the layout, or use the OS password action; only a revoked or closed portal ends control |
| `locked` / `greeter` (X11) | The OS surface changed | Use the normal OS password through the secure native editor |
| Wayland locked / greeter | This host contract does not inject into a Wayland lock or login surface | Unlock the computer, then reconnect; the phone reports the explicit reason |
| `disconnected` | Session, transport, revocation, or runtime ended | Reconnect only after the reported recovery condition is fixed |

The app clears frames, pending input, and sensitive editor contents on
disconnect, backgrounding, server change, lock/greeter transitions, and
revocation. Control is never silently resumed across a generation change.

The capability response also carries an additive, truthful `host.authorization`
block for the scoped inhibitor: `active`, per-leg `idle_inhibited` /
`suspend_inhibited` / `lock_inhibited`, `authorized_devices`, `reason` and
`updated_at`. It is not part of the signed v1/v2 payload; installed clients
ignore it, so no new app build is required for the host-side inhibitor.

## Optional Linux Host Setup

### Prepare And Review

The optional SDDM/boot host install below is only for lock-screen, greeter and
boot-before-login access. The SSH-started current-session flow above does not
need it and does not run root commands.

The preparation command detects the current Linux desktop-capable ELF, reads
the canonical daemon identity from the selected state directory, uses the
current account UID and supported `seat0`, and writes a user-owned config. It
does not run root commands, install packages, change SDDM, start a service, or
change the daemon:

```sh
"$HOME/.local/bin/zen-release" desktop-host --init-config
```

The default file is `$HOME/.zen/desktop-host.json`. Use `--state-dir DIR`
when the daemon uses another state directory, or `--config PATH` when a
reviewed location is required. The generated config has only `version`,
`hostId`, `ownerUid`, `seat`, and an optional explicitly supplied
`ownerUnit`. A second run with the same identity is a no-op. A changed
existing file is refused. Missing daemon identities, symlinks, unsafe config
ownership/modes, inactive/non-graphical seat0, locked or non-owner Wayland
sessions, a session without the portal RemoteDesktop/ScreenCast interfaces,
and unsafe or missing SDDM X11 hooks are refused without generating a config.
Wayland qualification discovers exactly one running compositor and reads the
portal interface versions without creating a portal session or showing a
dialog.

Review the exact transaction before applying it:

```sh
"$HOME/.local/bin/zen-release" desktop-host --plan --config "$HOME/.zen/desktop-host.json"
```

The plan is output only. It names the root-owned config, same-binary broker
and agent path, systemd unit, SDDM hook wrappers, modes, and requirements. It
does not write files or activate anything.

### Apply The One Admin Step

Use a reviewed, desktop-capable ELF. The preparation command prints the exact
absolute command for the current executable. The generic form is:

```sh
sudo "$HOME/.local/bin/zen-release" desktop-host \
  --install \
  --config "$HOME/.zen/desktop-host.json" \
  --binary-source "$HOME/.local/bin/zen-release" \
  --activate --register-current
```

This is the only root-required step in the host setup. It installs one
root-owned copy of that ELF at `/usr/libexec/zen/zen`, a root broker unit,
the validated config, and SDDM X11 start/stop wrappers that preserve the
existing hooks. `--activate` enables and starts only the broker. It does not
restart SDDM, reboot, log out, alter the canonical daemon, or create a second
device database. The broker admits only the configured owner account and the
existing daemon identity; an optional `ownerUnit` adds system-unit cgroup
binding.

`--register-current` completes current-display registration in the same admin
execution. It uses logind's active seat0 X11 display, identifies the root Xorg
process through that display's Unix socket credentials, verifies membership in
the active SDDM system unit, and pins the process lifetime. Only that verified
process's unique `-auth` argument is used. Its bounded, protected authority file
must resolve beneath `/run/sddm`; the open descriptor goes through the existing
root-only registration API. No guessed `:0`, first-Xorg search, user DISPLAY,
or Xauthority cookie in config or logs. This requires Linux pidfd support
(Linux 5.3+) and rootful SDDM Xorg; other display managers, rootless Xorg,
missing seat metadata, and ambiguous arguments are diagnosed before install.
On an unlocked owner Wayland session, the same flag validates the active
desktop, discovers the single running compositor, and records no display
registration: the broker re-discovers the compositor on every admission. It
does not create a portal session, so the compositor's consent dialog appears
when the paired phone connects. An existing X11 SDDM hook install is left in
place so the X11 greeter path keeps working.

The command then launches the installed ELF's existing UID-dropped agent in
view-only mode. Success requires valid stream metadata and an actual H.264
access unit, which is discarded without storing or displaying pixels. The
agent also checks XTest availability; no keyboard or pointer input is sent.
The active session is rechecked before success. A broker socket alone is not
sufficient. The normal SDDM hooks retain registration on future display starts.

On success the default output stays brief and always English: it states that
remote desktop is set up, points at Remote Desktop in the phone app, and
confirms the daemon and login screen were not restarted. Add `--verbose` to
print the verified surface/display/resolution and frame evidence, the
device-scope note, and the rollback command. Failures still print the failing
check and exit non-zero; `--verbose` changes presentation only. The message is
independent of `LANG`/`LC_ALL`; there is no locale detection and no localized
CLI copy.

An unchanged installation can run the same command again without restarting
the broker or replacing its files. A different config/binary or administrator
file drift is refused, as is a local/transient unit override or mask requiring
manual review. An already connected phone is left connected and the
verification reports it busy. A failed new activation/registration/probe
attempts journaled rollback; failures against an existing installation preserve
it. If rollback itself is blocked, the command reports the failure and retains
the journal rather than claiming completion. Neither path restarts SDDM or Zen.

After activation, keep the canonical daemon running with its original state
and identity. If it must start after reboot, use the optional user-owned boot
unit for that same daemon:

```sh
zen boot install --binary "$HOME/.local/bin/zen-release" \
  --state-dir "$HOME/.zen" --lan
zen boot status
```

Enable user lingering only when boot-before-login persistence is explicitly
wanted and permitted by the OS policy:

```sh
loginctl enable-linger "$USER"
```

This does not replace a user's tmux workflow. Do not run the whole daemon as
root and do not install `zen-dev` as the root broker.

### Verify And Roll Back

Check the installed unit and live identity without printing credentials:

```sh
zen boot status
systemctl is-active zen-desktop-host.service
zen doctor
```

Rollback is explicit, root-only, and conservative:

```sh
sudo systemctl disable --now zen-desktop-host.service
sudo "$HOME/.local/bin/zen-release" desktop-host --rollback
```

Rollback refuses to overwrite an administrator edit made after installation.
It removes only files recorded in Zen's root-owned installation journal and
restores the previous SDDM configuration when unchanged. Pairing state and the
canonical daemon state are not removed. If the broker is active, stop it first;
the rollback path will not kill an unrelated process.

## Platform Limits

| Host | Current support |
| --- | --- |
| Linux amd64, X11, logged-in session | Supported with a desktop-capable ELF and native dependencies |
| Linux amd64, SDDM X11 greeter/lock/login | Supported only after the reviewed broker/SDDM host install and owned-stack qualification |
| Linux Wayland desktop (KDE, unlocked owner session) | Supported after the reviewed host install; the compositor's portal owns screen-sharing consent and may require interactive approval unless persistence was granted there |
| Wayland greeter/login or no monitor | Not supported by this host contract |
| macOS | Host capture/boot-login adapter is not implemented; ScreenCaptureKit/TCC cannot be assumed |
| Windows | Host service, per-session agent, secure desktop, and WTS boundaries are not implemented |
| iOS native runtime | Shared source contract exists; local runtime qualification is still required |

The first reproducing platform determines test priority, not the product
boundary. A platform-specific adapter must have an explicit counterpart or
remain reported as unsupported. Zen does not bypass FileVault/LUKS preboot,
UAC, TCC, PAM, SDDM authentication, screen locking, or OS security policy.

## Security And Input Limits

The broker is the only privileged process. It uses a fixed root-owned same-ELF
path, kernel peer credentials, bounded IPC, session/display generations, fresh
device signatures, scope checks, actual TLS, and synchronous revocation. The
capture agent drops to the session UID before initializing display or codec
libraries. Same-UID processes remain inside the configured account trust
domain unless an explicit root-enrolled `ownerUnit` is configured.

On Wayland the compositor's RemoteDesktop portal is the consent authority. Zen
requests persistent consent only when the interface advertises restore-token
support, stores the returned opaque single-use token with owner-only file
permissions, rotates it on every successful Start, and drops it once when the
computer rejects it. The token never bypasses the paired-device scope,
encrypted transport, or revocation. A Wayland connection needs the owner
desktop to be unlocked; the broker refuses a Wayland lock or greeter surface
instead of reporting a misleading ready state.

The secure OS-password editor sends at most 64 printable ASCII characters and
Enter through the existing encrypted native channel. Zen never stores, logs,
validates, or copies the password and never reads physical keyboard input.
Committed phone text travels as Unicode scalars in protocol batches of at most
64 events; the compositor maps each scalar to its own keymap and may reject a
character it cannot inject. A rejected character or key is reported to the
phone as an `inputError` without ending the video stream, while a revoked or
closed portal ends control and retirement. The phone keyboard is a native
commit-aware field: soft-IME composing text (candidates, pinyin, autocorrect)
stays on the phone and only committed text plus named Backspace, Enter, and
Delete keys cross the wire; hardware and injected key events and pasted text
follow the same bounded batches, and one physical backspace arriving through
two input paths is deduplicated. Remote Desktop is the only route that unlocks
rotation; leaving the route restores the product portrait lock. Unicode
injection quality still depends on the compositor keymap; arbitrary lockers
and physical-device native qualification are not claimed.

## Historical Acceptance Notes

The disposable Ubuntu/SDDM/X11 fixture has proven the production path for
pairing, identity TLS, greeter capture, normal OS login, owner handoff,
lock/unlock, revocation, input retirement, daemon restart, and both `zen` and
`zen-dev` runtime parity. Those observations are acceptance evidence, not a
promise that every Linux display manager, locker, GPU, or mobile runtime is
equivalent.

The accepted candidate used the same ELF role architecture and retained the
known emulator video-overlay screenshot limitation. Android native behavior
was exercised on an owned AVD; physical Android, native iOS runtime, and
personal-host boot installation remain separate qualification items. Full
evidence, hashes, and resource-control notes belong in the Brain worklog, not
in this operator document.

## Lightweight Wayland backend decision (2026-09-14)

The MVP backend is the existing Zen-owned path:

```
KDE ScreenCast/RemoteDesktop portal -> PipeWire -> GStreamer H.264
  -> Zen desktop-helper length-prefixed packets -> authenticated Zen WebSocket
  -> native Android/iOS decoder and view
```

The source already contains the important bounded pieces: typed portal session
and consent handling, one selected monitor stream (or portal virtual source
when no monitor is advertised), a portal-issued PipeWire file descriptor and
node binding, software-first H.264 probing with optional VA
hardware selection, bounded queueing, metadata plus binary frame packets, and
portal RemoteDesktop pointer/keyboard events. The broker/client owns the local
capability transfer and the authenticated Zen connection. This is enough for
the one-screen, no-audio MVP when the portal grants either a monitor or its
advertised virtual source. It is not
a claim that the C path is a general streaming server: the remaining product
work is real-session validation with a listener and decoder, lifecycle and
reconnect observation, and platform UI evidence.

| Choice | Result for the bounded MVP |
| --- | --- |
| Existing portal/PipeWire/GStreamer chain | Chosen. Reuses Zen auth, scope, revoke, WSS, framing, and native decoder boundaries; no game-streaming control plane. |
| Sunshine/Moonlight host | Rejected for this MVP. It adds the admin/pairing/runtime and codec/network surface that Zen already owns, while the current host still fails before capture when KWin has zero outputs. Existing Sunshine artifacts remain untouched. |
| Headless source | The KDE portal reports `AvailableSourceTypes=7`, including Virtual. Zen requests that source when no monitor is advertised and keeps creation/cleanup inside the portal session; no compositor restart or replacement is performed. |

The current host is genuinely headless. Read-only evidence on 2026-09-14:

* `kwin_wayland --version` → `kwin 6.6.4`; the running command has no `--virtual` and uses the DRM backend.
* `loginctl show-session 30` reports an active, unlocked `Type=wayland` owner session, while `Display=` is empty.
* `/sys/class/drm/card0-DP-1`, `DP-2`, `DP-3`, and `HDMI-A-1` all report `disconnected`; the only writeback connector is not a display output.
* The user portal exposes ScreenCast v5 and RemoteDesktop v2. `AvailableSourceTypes` returns `7` (monitor, window, and virtual bits); Zen now prefers a monitor and requests the portal's virtual source when no monitor is advertised, keeping source creation and cleanup inside the same portal session.
* The Wayland runtime has exactly one held compositor socket (`/run/user/1000/wayland-0`) and the live KWin object tree exposes EIS and screenshot interfaces, with no output-creation control.

The portal advertises a virtual source, and Zen requests it in the same
portal session when no monitor is advertised. That source is created by KWin
only in the post-consent continuation. On the installed KDE 6.6.4 stack,
`xdg-desktop-portal-kde`'s `RemoteDesktop.Start` first requires a Qt screen,
then either restores a valid prior grant or creates a `RemoteDesktopDialog`;
the virtual output is started only after that dialog is accepted. A session
with no physical or logical output therefore cannot render the first consent
dialog or create the virtual output that would make it renderable. The
portal's advertised bit is capability metadata, not an unattended first-grant
path.

If the KDE portal declines the source or returns no stream, the capability
reports the portal failure; Zen does not enable VKMS, grant broad `/dev/dri`
access, add `CAP_SYS_ADMIN`, create a nested compositor, use X11/xrdp as a
replacement, or report a black/fake frame as success. A saved restore token
can bypass the chooser only after an operator has previously accepted a grant
for this portal identity; Zen never fabricates or edits that token.

#### Zero-output KDE session: operator prerequisite

For KDE/Plasma 6.6.4 with KWin's DRM backend, the smallest first-grant
prerequisite is one renderable Qt/KWin output in the existing owner session.
The operator must complete the normal KDE Remote Desktop consent once while
that output exists. Zen then stores the portal-issued single-use restore token
at `$HOME/.zen/desktop-portal-restore-token`; later reconnects can use the
supported restore path while the same KDE session remains valid. Removing the
temporary output after the first grant is a valid rollback only if the saved
grant still restores successfully; otherwise the consent prerequisite returns.

On this host the current session reports `QGuiApplication::screens() = 1` but
its only screen is `0x0`, `/sys/class/drm` reports every display connector
disconnected, and there is no `kde-authorized/remote-desktop` permission or
Zen restore-token file. KDE's only virtual-output call is the internal
`zkde_screencast_unstable_v1` stream operation invoked after consent. There is
no supported same-session command that can create an output before that
dialog, so unattended first consent is not executable under the current
zero-output constraint. This is a host prerequisite, not a phone action; the
existing KDE session, daemon, APK, VNC service, and portal lifecycle remain
unchanged.

The daily UX after that first grant remains **Zen start → one phone
authorization → Connect**. Wayland portal consent and a renderable output for
the first grant are unavoidable host requirements; Sunshine's game streaming,
UDP traversal, hardware codec negotiation, audio, gamepad, and separate admin
plane are optional complexity outside this MVP.

## Scope correction (2026-09-15)

Remote Desktop has one product source on Linux: the current logged-in KDE
Wayland session. Zen selects the portal/PipeWire source in that session,
requests the OS consent dialog when needed, binds one stream, and forwards the
existing length-prefixed metadata/H.264 and pointer/scroll/keyboard/text
packets over the authenticated Zen WebSocket. Device scope, revoke, lifecycle,
and the native Android/iOS decoder remain the control boundary.

The `scripts/zen-virtual-x11.sh` launcher is an optional owned fixture for
verifying native X11 capture and input without a physical monitor. It does not
add a Zen backend, route, selector, or persistent desktop lifecycle, and its
frames cannot prove that the logged-in KDE session is capturable.
