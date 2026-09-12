# Remote Desktop

Zen Remote Desktop connects the paired Android or iOS app to the desktop of
the current Zen server. Supported Linux amd64 targets are an X11 desktop and
an unlocked KDE Wayland owner desktop through the session's
xdg-desktop-portal RemoteDesktop interface. Android and iOS use the same
pairing, transport, and session contract. Web, audio, clipboard, file
transfer, and multi-monitor composition are outside this feature. On Wayland
the compositor keeps its own screen-sharing consent; Zen never injects input
through a compositor test protocol.

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
requires the paired device scope and encrypted transport on every connection.
The app shows contextual failure states and keeps the secure OS-password
editor, disconnect, pan, pointer, keyboard, and scroll controls behind the
connection's reported capabilities.

Current-session access ends with that desktop session. Lock-screen, greeter,
logout, and boot-before-login access require the optional Linux host install
below. A user daemon cannot grant itself pre-login access.

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

Unattended desktop requires actual encrypted transport: identity-bound TLS,
the existing pinned Zen Link, or another verified `wss` origin. The daemon's
existing identity creates the certificate and SPKI pin; no certificate file,
domain, helper path, or `ZEN_DESKTOP_HELPER` setting is needed. Numeric private
addresses do not prove encryption. The app rejects redirects, forged proxy
headers, identity mismatches, and secure-to-plain downgrades.

The legacy plaintext private-LAN path remains attended-only in the transport
policy and cannot carry OS passwords or open lock/login surfaces. It is an
advanced compatibility capability, not part of the normal Remote Desktop
Connect flow. Its explicit network acknowledgement, if used by an older or
specialized client, remains bound to the exact daemon identity and origin.

## Capability States

These facts are independent and must not be collapsed into a generic
authorization error:

| State | Meaning | Action |
| --- | --- | --- |
| `desktop_scope_required` | Device is legacy terminal-only | Choose **Enable remote desktop** in the app and confirm once; no re-pair or host change |
| `desktop_tls_required` | The attempted path is not encrypted to this daemon | Use identity TLS or pinned Link; do not enable plaintext for passwords |
| `host_setup_required` | Neither this process's current display nor the broker is available | Start Zen inside the logged-in desktop session, or complete host install |
| `connected` | Native decoder has presented the current generation | Use desktop controls; a received sample alone is not connected proof |
| `connected` + `inputError` | The computer rejected one character or key in the current layout | Keep using the stream and adjust the layout, or use the OS password action; only a revoked or closed portal ends control |
| `locked` / `greeter` (X11) | The OS surface changed | Use the normal OS password through the secure native editor |
| Wayland locked / greeter | This host contract does not inject into a Wayland lock or login surface | Unlock the computer, then reconnect; the phone reports the explicit reason |
| `disconnected` | Session, transport, revocation, or runtime ended | Reconnect only after the reported recovery condition is fixed |

The app clears frames, pending input, and sensitive editor contents on
disconnect, backgrounding, server change, lock/greeter transitions, and
revocation. Control is never silently resumed across a generation change.

## Optional Linux Host Setup

### Prepare And Review

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
