# Remote Desktop

Zen Remote Desktop connects the paired Android or iOS app to the desktop of
the current Zen server. The supported delivery target is Linux amd64 with an
X11 desktop. Android and iOS use the same unattended pairing, transport, and
session contract. Web, audio, clipboard, file transfer, and multi-monitor
composition are outside this feature.

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

The normal Connect action is unattended after pairing. It does not require a
second Enable, Allow, or Share action. The daemon starts the selected desktop
source itself. The app still shows contextual failure states and keeps the
secure OS-password editor, disconnect, pan, pointer, keyboard, and scroll
controls behind the connection's reported capabilities.

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
access from a LAN acknowledgement, a reconnect, or host setup. Re-pair once
with a fresh owner-issued `zen pair` link to upgrade that existing device.

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
| `desktop_scope_required` | Device is legacy terminal-only | Re-pair once and acknowledge the desktop scope |
| `desktop_tls_required` | The attempted path is not encrypted to this daemon | Use identity TLS or pinned Link; do not enable plaintext for passwords |
| `host_setup_required` | Neither this process's current display nor the broker is available | Start Zen inside the logged-in desktop session, or complete host install |
| `connected` | Native decoder has presented the current generation | Use desktop controls; a received sample alone is not connected proof |
| `locked` / `greeter` | The OS surface changed | Use the normal OS password through the secure native editor |
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
ownership/modes, inactive/non-graphical seat0, unsupported Wayland, and unsafe
or missing SDDM hooks are refused without generating a config.

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
  --activate
```

This is the only root-required step in the host setup. It installs one
root-owned copy of that ELF at `/usr/libexec/zen/zen`, a root broker unit,
the validated config, and SDDM X11 start/stop wrappers that preserve the
existing hooks. `--activate` enables and starts only the broker. It does not
restart SDDM, reboot, log out, alter the canonical daemon, or create a second
device database. The broker admits only the configured owner account and the
existing daemon identity; an optional `ownerUnit` adds system-unit cgroup
binding.

Installing hooks does not retroactively register the running display. A
subsequent normal SDDM display start registers it automatically. For an already
running greeter, registration requires administrator-verified display and
Xauthority metadata through the existing `desktop-host --register start`
API; a reachable broker socket alone is not capture readiness. Never guess
`:0`, select the first Xorg process, restart SDDM to make a probe pass, or copy
Xauthority cookies into config. Zen currently does not automate this live
registration step.

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
| Linux Wayland desktop | Not a generic one-command target; portal permission and compositor input must be qualified per environment |
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

The secure OS-password editor sends at most 64 printable ASCII characters and
Enter through the existing encrypted native channel. Zen never stores, logs,
validates, or copies the password and never reads physical keyboard input.
Unicode, IME composition, non-US layouts, arbitrary lockers, and
physical-device native qualification are not claimed.

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
