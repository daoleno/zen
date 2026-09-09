# Remote Desktop

## Outcome And Boundaries

Operate an explicitly selected desktop on the current Zen server from a phone.
Host priority is Linux, macOS, then Windows. Android and iOS share the same
product contract; web is outside scope. The first scope includes viewing,
mouse, keyboard, scrolling, mobile zoom and pan. Audio, clipboard, file
transfer, gamepads and multi-monitor composition are outside the current
delivery. **Unattended own-computer access, an existing user's lock screen,
and the running OS login screen after reboot are required**, not optional
future scope. Normal password entry must work without a local person approving
each connection. Attended assistance remains an explicit mode. Full-desktop
access is not application or window isolation. Elevation bypass and encrypted
disk preboot unlock are excluded.

The previously distributed bfad184 artifact remains attended-only. Current source adds a
Linux broker and UID-dropped agent as **roles of the same `zen` executable**, default
scoped/TLS `/desktop` admission, SDDM registration, journaled installation and native
sensitive input. This does not update an installed APK or qualify every OS/display stack.
The attended GTK helper is a separate process of that same ELF; it has no
permission-bypass flag. A current standalone phone APK still needs a separately approved
system-host installation; installing the phone client alone does not enable boot or
greeter access.

This plan distinguishes intended behavior from implementation and acceptance.
A platform is not accepted without a native build and actual capture,
presentation and input evidence on that platform. Software encoding,
emulators, Expo exports and static contracts do not replace hardware
acceptance. Private execution reports, screenshots and raw measurements
belong in the Brain worklog, not this repository. Existing releases remain
immutable; implementation does not authorize publishing or deployment.

## Pairing And OS Setup

New pairing has one confirmation granting terminal plus unattended desktop
view/control, including supported lock/login screens. Subsequent connections
need neither another enable switch nor host approval. The existing pairing
token, daemon identity, device key and canonical trusted-device record remain
the only trust system. `desktop_scope_version: 1` records this acknowledgment;
it is authority, not evidence of installed OS permissions or capture support.

Old records without that version retain terminal and explicitly requested
attended access (`X-Zen-Desktop-Mode: attended`). The default endpoint refuses
legacy scope and plaintext transport; old clients do not silently gain access.
They never gain unattended privileges through upgrade, loading state, ordinary
authentication, LAN consent or discovering a display. Migration is one explicit
re-pair using a fresh owner-issued `zen pair` token and the same device key.
The phone shows the expanded scope once. Its signed acknowledgment binds the
scope domain, daemon public key, one-time token, device ID and device key.
Missing/unknown scope versions or altered proofs cannot grant new authority.
An old client's token enrollment remains legacy scope, not an automatic upgrade.
Revoking the device in the existing owner removes terminal and desktop access
and synchronously retires desktop sessions. No second device list is created.

Consolidate OS installation/permission requests into initial host onboarding:
explain privileged broker installation and boot service ownership on Linux,
Screen Recording/Accessibility on macOS, and service installation on Windows.
Pairing does not grant those OS rights. Show separate runtime states such as
`Host setup required`, `OS permission required`, `Login screen unsupported`,
`Locked`, `Switching session` and `Connected`. Do not advertise `Ready` until
the matching agent has actually obtained capture and input capabilities.

## Linux Unattended Architecture

Use a small privileged broker plus UID-dropped per-session agents, retaining
Zen's authenticated connection, paired identity, bounded input/frame protocol
and native phone decoders. Do not run the full Zen daemon or GStreamer/GTK/GPU
code as root. The canonical unprivileged daemon must become available after
OS boot with its original identity/state and no duplicate owner. A separately
approved system service can launch it as the existing owner UID; a desktop
autostart or terminal-only development watcher cannot provide this guarantee.
If the owner's home/state is unavailable before login, explicitly plan a
single-owner state relocation during enrollment rather than copying identities
or silently promising access. LUKS/FileVault preboot is not handled by Zen.

The broker has local Unix IPC only, fixed root-owned executables/configuration,
peer-credential checks, bounded messages and an allowlisted command set. It
never accepts a client executable, environment, DISPLAY, bus address, UID,
Xauthority path or raw device path. Device proof and scope are rechecked against
the canonical owner; a peer UID or claimed device name is not sufficient.
Agents receive only the exact display capability/FD for their generation.
Per-session codecs, X11/Wayland libraries and UI run without elevated privileges.

## Single-Binary Roles And Native Integration

One distributed `zen` ELF handles the ordinary daemon and internal desktop roles.
Users run `zen` and `zen setup` / `zen doctor`; they do not download, build, or
point at a second helper executable. Internal subcommands are dispatched **before**
ordinary server startup:

| Role | Command | Process | Privileges |
| --- | --- | --- | --- |
| Daemon | `zen` / `zen serve` | Network, pairing, Brain, Terminal | Unprivileged owner UID |
| Attended helper | `zen desktop-helper …` | GTK consent, capture, encode, input | Same owner UID; no daemon |
| Unattended broker | `zen desktop-host …` | Local Unix IPC, SDDM registration, install | Root only when installed as the reviewed system copy |
| Session agent | `zen desktop-agent …` | X11 capture/XTest after UID drop | Dropped session UID; never root |
| Identity | `zen desktop-identity` | Provenance JSON only | No capture, no daemon |

The daemon and broker launch children with `os.Executable()` (symlink-resolved),
never `PATH`. Remote clients cannot supply an executable, `DISPLAY`, helper path or
library search path. `ZEN_DESKTOP_HELPER` is a test-only absolute override; default
startup must work without it. Relative values are ignored.

Native Linux capture is **cgo-linked** into desktop-capable builds (`-tags zen_desktop`,
`CGO_ENABLED=1`) from `daemon/desktop/native/` (`linux.c`, portal/encoder sources, and
`host-agent.c`). This is not a second ELF extracted at runtime, and end-user startup
does not compile C. Makefile targets remain for standalone encoder/portal checks.

**Dynamic library tradeoff (explicit):** desktop-capable Linux `zen` has `DT_NEEDED`
entries for system GTK 3, GStreamer app/video, X11 and XTest. GStreamer plugins
(ximagesrc, openh264, h264parse, pipewiresrc) stay runtime plugin dependencies.
This is **not** a static single-file artifact. `zen doctor` reports whether this
binary was compiled with native roles, hashes the executable, and lists missing
`DT_NEEDED` libraries. On a Linux amd64 host, `scripts/build-daemon-linux.sh`
produces a desktop-capable `zen-linux-amd64`. linux/arm64, Darwin, and
linux/amd64 built from another host stay daemon-only; doctor must say so rather
than pretending capture is present. Local `bun run daemon:build` and `zen-dev`
enable the desktop tag when `pkg-config` finds the development files.
macOS/Windows host adapters remain unimplemented. If GTK/GStreamer are not
installed, `ld.so` will not start a desktop-capable ELF, so doctor cannot run
until those OS packages exist.

Privilege separation is **process isolation of the same ELF**, not a shared
crash/security domain: the root broker never initializes GTK/GStreamer; the agent
drops UID/capabilities before capture; the user daemon does not run as root.
GTK/GStreamer constructors may still map in a desktop-capable broker process
because they are `DT_NEEDED`; the broker does not call capture APIs. A sibling
`.so` was rejected because it would split the distributed artifact.

DEV: `zen-dev` rebuilds this complete binary when Go **or** native C/headers/Makefiles
change, then restarts its child daemon. External `#include` of `desktop/native/*.c`
from cgo is not a Go package file, so the watcher passes a content-hash
`-DZEN_NATIVE_BUILD_INPUT=…` compiler define (preserving caller `CGO_CFLAGS`)
instead of deleting GOCACHE or touching `.go` files. A comment-only C edit can
still compile to an identical ELF; linked rebuild proof is a used fixture marker
plus `desktop-identity` / role `/proc/pid/exe`, not output size/mtime. Builds
write `tmp/zen-dev.building` and rename onto the last-good ELF so a compile
failure keeps the previous child binary. Existing daemon shutdown closes the desktop
manager and retires helper children. The user-writable DEV binary (`daemon/tmp/zen-dev`)
must never be `ExecStart` for the root broker; the installed copy is
`/usr/libexec/zen/zen` only after reviewed installation. Root agent launch opens that
root-owned ELF and refuses when `/proc/self/exe` is not the same inode, or the path
is under a user-writable prefix (`/home`, `/tmp`, `/var/tmp`, `/dev/shm`, `/run/user`,
`.zen`, `zen-dev`).

Updater/setup treat one binary identity: `zen update` replaces `zen`; helper, broker
and agent roles cannot drift because they are the same file. Atomic replace/rollback
cannot strand mismatched role versions.

The owner socket
checks both kernel UID and the configured systemd unit's current MainPID. A
one-use broker challenge binds boot/session/display generation and the request;
the canonical daemon signs it only after fresh device/scope and TLS admission.
The broker never loads a second device database. Its bounded byte relay closes
before agent termination, preventing cleanup latency from forwarding a retired
generation. Relevant seat/current-session D-Bus changes retire it; unrelated SSH
session changes do not. The existing desktop owner also closes it on daemon
shutdown and device revocation.

Only the broker retains `SETUID`, `SETGID`, `CHOWN`, `DAC_READ_SEARCH` and `KILL`
capabilities. `CAP_SETUID` is explicitly ambient for systemd sandbox startup;
agents change all UIDs/GIDs, clear supplementary groups and capability sets,
disable dumps and retain no-new-privileges before capture/codec initialization.
`CAP_KILL` is used only for the tracked child during bounded retirement. No
`CAP_SYS_ADMIN`, DRM master, input-device enrollment or whole-daemon root mode
is introduced.

| Host Surface | Capture And Input Plan | Admission And Evidence Gate |
| --- | --- | --- |
| SDDM X11 greeter | Approved SDDM DisplayCommand/DisplayStopCommand registration of display and Xauthority FD; UID-dropped greeter agent uses X11 capture/XTest. Preserve existing hooks. | Independently verify active seat0, greeter class, service/process ownership and display generation. Never probe guessed displays or use `xhost +`. Owned SDDM VM capture/input remains required. |
| Owner X11 desktop and lock | Owner-session agent uses existing frame/XTest pipeline; capture the standard locker, not underlying unlocked windows. | Observe lock lifecycle and active seat; prove locker rendering/input on the actual desktop stack. Retire old media/input before lock handoff. X11 clients are not mutually isolated. |
| Owner Wayland desktop | Portal/PipeWire plus compositor-supported input; persistent restore grants may avoid repeated permission prompts after OS enrollment. | Restore can fail or prompt again. Report missing permission, never switch silently to XTest/Xwayland. |
| Wayland lock or greeter | Separate compositor-supported system remote-login backend, or audited DRM/KMS capture broker and seat-bound uinput adapter. | A user portal/restore token/EIS FD does not establish greeter rights. DRM framebuffer handles may require DRM master or CAP_SYS_ADMIN; do not steal compositor master. Driver/GPU/plane/DPMS and input-seat behavior need owned tests. No generic ready claim. |
| No monitor | Explicitly enrolled virtual output on the intended compositor/X server, or a separately labeled isolated virtual desktop. | No active CRTC means no KMS image. Xvfb is not the personal console or a boot-greeter substitute. Do not alter personal monitor/EDID/boot settings implicitly. |

Use logind's system D-Bus `Seat.ActiveSession`, `SessionNew/Removed` and
`PropertiesChanged` with `Class`, `Type`, `User`, `Seat`, `Active`, `State` and
`LockedHint`. `LockedHint` is advisory, not a security barrier or capture grant;
combine it with verified locker/compositor lifecycle. `TakeControl/TakeDevice`
are session-controller APIs with pause/resume rules, not universal screen-copy
authorization. Never invoke `UnlockSession`, patch PAM, disable locking, set
autologin, or send credentials to SDDM's authentication socket.

On lock, logout, active VT/UID change, display resize/replacement, agent failure,
revocation or reboot: close admission, release held input, stop old capture,
clear phone frames/keyboard state, then retire that generation. Reacquire the
active display and create fresh SPS/PPS/IDR plus source dimensions before new
input. Greeter-to-owner handoff binds to the enrolled owner UID; a different
logged-in UID ends access. No retained key event may become a password character
in the next session. An approved device reconnects using fresh existing signed
authentication, without a local approval or replayed input. Network reconnection
after reboot is continuity of authority, not survival of the old process/socket.

Local Stop/temporary Deny and device revocation remain available at the owner
boundary. A greeter stop surface may only deny, never grant scope or unlock.
Display a remote-control indicator without depending on someone clicking it.
Bound authenticated connection attempts (10/minute host-wide),
one active desktop, input/frame sizes and queue deadlines. Add per-device and
unauthenticated gateway limits before boot-host exposure; keep OS login attempt
limits intact. Neither Zen nor diagnostics should interpret login success by
watching typed passwords; observe OS session transitions instead.

## Password Transport

Require real authenticated end-to-end TLS for all unattended sessions, including
lock/login viewing. Prefer the existing SPKI-pinned Zen Link transport or a
properly verified direct TLS origin. Do not deploy a new relay or trust a public
TLS terminator as end-to-end confidentiality. No `X-Forwarded-Proto`, numeric
LAN address, loopback address, device signature or pairing grant proves encryption.
Zen Link's close-observing listener now preserves actual inner TLS state on
HTTP requests; actual TLS and forged-header plaintext regressions cover this.

The accepted opt-in plaintext LAN feature remains for ordinary attended desktop
use. It is unsuitable for OS password entry. The new broker must retire it on
lock/session transitions; the current attended artifact has not proved that
barrier and must not be presented as safe for entering any sensitive data.
Zen cannot detect every password field in an arbitrary application.

Credentials go only as ephemeral input through the existing encrypted auth/media
channel to the standard OS login UI. Never store OS passwords, capture physical
keyboard input, log key codes/payloads, copy credentials to clipboard, call PAM
on behalf of the user, or add a password-validation RPC. Android and iOS source
now provides native secure editors that send bounded ephemeral character events
directly on the native socket. JS can request the editor but never receives its
text. Editors recheck generation and live encrypted transport before submission,
clear on submission/cancellation/background/server change, and do not persist
drafts or enable prediction. A live native Link-listener registry validates the
loopback/pin association; a JS string or an arbitrary localhost port is not
sufficient. Direct TLS requires a completed TLS connection.

Android configures single-line behavior before the password input type and
explicitly applies `PasswordTransformationMethod`; otherwise Android can replace
the masking transformation. The native field disables saved state, autofill,
suggestions and personalized IME learning. Owned Android emulator verification
has exercised masked entry, background draft clearing, real TLS SDDM login and
lock/unlock, legacy scope migration, new-pair scope and active revocation.
This is not physical-device, native iOS, or every Linux locker acceptance.

The current sensitive-character contract is at most 64 printable ASCII
characters, sent to the visible OS field followed by Enter. Unicode, non-US
keyboard layouts and IME composition are not qualified. The JS history keyboard
is disabled on broker-reported greeter/lock surfaces; their printable input must
use the native sensitive path. It is still the OS, not Zen, that authenticates
the password. Native-device UI evidence remains a separate acceptance gate.

## Linux Installation

Installation requires an existing, reviewed, boot-owned **unprivileged** canonical
daemon unit. The installer references its exact unit and identity; it does not
create another daemon or relocate/copy state. A development watcher alone is
not that boot unit. Review the existing unit's executable, state directory and
network flags before any personal-host handover.

`zen desktop-host --install --config <reviewed-config> --binary-source <ELF>`
installs one reviewed desktop-capable `zen` ELF as the root-owned broker/agent
identity. `--broker-source` and `--agent-source` remain accepted only when they
name the same bytes as each other (and as `--binary-source` when that flag is
also set). `--activate` additionally enables/starts only the broker after checking
that the canonical unit is active; it does not restart SDDM or the owner. A normal
subsequent display start uses the registered hooks. `--rollback` requires the
broker to be stopped, removes its enablement, restores unchanged installed files
and reloads systemd. It refuses to overwrite administrator edits made since
installation. `zen setup` does not silently convert a user DEV watcher into this
boot service; OS consent remains a separate, reviewed installation.

The transaction covers `/usr/libexec/zen/zen` (the same ELF for broker and agent
roles), `sddm-start` and `sddm-stop` in that directory, `/etc/zen/desktop-host.json`,
`/usr/lib/systemd/system/zen-desktop-host.service` (`ExecStart` runs
`/usr/libexec/zen/zen desktop-host --config …`) and the two hook keys in
`/etc/sddm.conf`. Existing effective Xsetup/Xstop commands are read with an INI
parser and preserved. `/etc/zen/desktop-install.json` is the root-only rollback
journal; `desktop-install.lock` serializes installation and rollback. No password
or independent device-grant file is created. Descriptor walks reject symlinks and
writable/unowned parent directories. Personal-host root installation remains gated.

The broker uses godbus/dbus v5 (BSD-2-Clause); the installer uses ini.v1
(Apache-2.0). Their notices are in the repository `NOTICE`. The agent links system
X11/XTest and GStreamer/OpenH264 libraries; test-VM QEMU packages are not product
dependencies or shipped assets.

## Reuse And Delivery

Primary sources inspected 2026-09-08:

- [SDDM 0.21 Xorg lifecycle](https://github.com/sddm/sddm/blob/v0.21.0/src/daemon/XorgDisplayServer.cpp) supplies DISPLAY/XAUTHORITY to display hooks; greeter and user sessions can use different display servers. [Configuration](https://github.com/sddm/sddm/blob/develop/data/man/sddm.conf.rst.in) defines the execution UID of those hooks.
- [systemd login1](https://www.freedesktop.org/software/systemd/man/latest/org.freedesktop.login1.html) defines session/seat and controller boundaries. [Portal RemoteDesktop](https://github.com/flatpak/xdg-desktop-portal/blob/main/data/org.freedesktop.portal.RemoteDesktop.xml) documents restore tokens, failure/prompt behavior and EIS, not SDDM login support.
- [RustDesk Linux service](https://github.com/rustdesk/rustdesk/blob/master/src/platform/linux.rs) demonstrates system/per-session supervision, uinput and optional DRM greeter handling. Its AGPL-3.0 code is research evidence, not code to embed or translate into Apache-2.0 Zen.
- [Sunshine KMS](https://github.com/LizardByte/Sunshine/blob/master/src/platform/linux/kmsgrab.cpp) shows privileged DRM framebuffer acquisition. Sunshine is GPL-3.0; no silent linking, copied implementation or assumed subprocess license exemption.
- [GNOME Remote Desktop](https://github.com/GNOME/gnome-remote-desktop/blob/master/README.md) has GDM-specific system remote login and separate headless modes, GPL-2.0+. [KRDP](https://github.com/KDE/krdp/blob/master/README.md) documents lack of SDDM RDP support; its autologin workaround is explicitly rejected here.
- [xrdp](https://github.com/neutrinolabs/xrdp) and [FreeRDP](https://github.com/FreeRDP/FreeRDP) are Apache-2.0 core-reuse candidates. xrdp normally creates/reconnects a separate Xorg session via sesman, not the current SDDM console; adopting it would change session and credential boundaries. Keep as an explicit alternative, not a hidden backend substitution.

Retain permissive X11/XTest and dynamically linked system multimedia libraries
for the first SDDM-X11 increment. Review each future adapter's license and driver
requirements; commercial UX references are not source or capability evidence.

1. Implement signed pairing scope and explicit legacy migration in existing auth;
   prepare session admission, canonical revocation and TLS provenance tests.
2. The Linux executable broker, canonical process/proof admission, bounded
   media/input relay, SDDM registration, UID-dropped X11 agent and journaled
   installer are implemented in source. Qualify them against the intended OS
   stack before personal-host installation; source completion is not deployment.
3. In an owned disposable SDDM VM, prove cold OS greeter capture, normal synthetic
   fixture-account password entry, owner handoff, lock/unlock, logout, reboot,
   wrong-device/key/scope denial, revocation during input and no credential logs.
   Run both phone clients; evidence from policy callbacks is not OS proof.
4. Native sensitive input and the unattended default are implemented in shared
   app/native source. Complete Android/iOS native-device UI verification, layout
   qualification, local-host indicator implementation and encrypted-path evidence.
   Attended assistance remains explicitly selected, not a silent fallback.
5. Qualify Wayland system capture/input and no-monitor output independently.
   macOS needs a signed launch daemon/agent design and user-granted TCC; ordinary
   ScreenCaptureKit cannot be advertised as prelogin/FileVault support. Windows
   needs a native service and per-session agent using WTS notifications and
   permitted desktop handles; Session 0, secure-desktop ACLs and SendInput/UIPI
   prevent assuming a user agent can operate Winlogon/UAC. No UAC/TCC bypass.

Actual personal-host root service installation, SDDM changes, boot ownership
migration, device-node enrollment, or personal login/unlock require separately
explained approval. Installer/source preparation does not authorize applying it.

### Owned-VM Evidence

An owned Ubuntu 24.04 / SDDM 0.20 / X11 / Openbox / xsecurelock fixture has
demonstrated authenticated H.264 greeter capture, normal OS password login,
fresh owner-desktop capture/input, normal-password unlock, normal Openbox logout
to a fresh greeter, and device/daemon-identity persistence through guest reboot.
Fresh capture was denied while a different UID owned seat0. Active device
revocation closed the stream, rejected stale input and rejected reauthentication;
a same-UID process outside the canonical unit MainPID was also denied on IPC.
The installed unit used the source-defined capability profile, without a
temporary override. Synthetic passwords were absent from the guest journal.

These are real guest OS and native-host-agent observations with a scripted TLS
consumer, not Android/iOS native-client login evidence. A forced logind session
termination crashed the fixture's SDDM helper and is not counted as normal logout
support; the accepted normal path uses the desktop session's ordinary exit.
Other SDDM versions, lockers, physical GPUs and mobile runtime behavior require
their own qualification. No new APK, personal-host installation or release is
implied by this source milestone.

## Verified Transport Boundaries

| Path | Source And Contract | Media Implication |
| --- | --- | --- |
| LAN or self-managed origin | `daemon/server/server.go`: `Handler`, `handleWS`; `app/services/connection.ts` | HTTP/WebSocket reachability does not establish UDP reachability. A plaintext LAN origin is not an encrypted video channel. |
| Zen Link | TCP dial in `daemon/link/connector.go`; inner TLS in `daemon/link/`; `docs/zen-link-relay.md` | The relay forwards opaque TCP streams. Pairing pins the daemon SPKI. There is no routed UDP or TURN contract. |
| Mobile Link | `app/services/pinnedTransport.ts`; `app/modules/zen-link-transport/` | The existing native pinned-TLS proxy exposes a loopback WebSocket origin. Reuse it without another server selector or trust root. |
| Cloudflare or other HTTP tunnels | Existing origin and HTTP/WebSocket integration | Do not assume arbitrary UDP forwarding or attempt to put WebRTC UDP inside WebSocket. |

Use a separate authenticated binary WebSocket session through the existing
origin or Link. This is continuous H.264 access-unit streaming, not screenshot
polling. Native code decodes and presents video; JS carries configuration,
state and input commands only. TCP and TLS remain system implementations. Do
not introduce custom cryptography, congestion control, NAT services or relays.
Media has its own socket, write ownership, queue and helper process rather
than sharing chat scheduling. Physical bandwidth is still shared. TCP
head-of-line blocking remains a limitation: terminate a stalled session
instead of accumulating unbounded video.

WebRTC/Pion remains a candidate when an existing path actually provides
direct UDP reachability; deploying its network prerequisites is not this
feature's scope. Successful signaling must never be reported as successful
video. Native desktop clients accept trusted `wss` or the existing pinned
Link loopback origin, without weakening certificate validation. Manual numeric
private-network `ws` also works after explicit unencrypted-desktop consent.
Private addressing is not encryption: screen content and input may be read or
altered by another party on that network. Public/plain hostname endpoints,
link-local addresses, unbound loopback and secure-to-plain downgrades remain
rejected. IPv4 private ranges, RFC6598 overlay addresses and IPv6 ULA are
parsed, not inferred from DNS or string prefixes.

LAN approval is persisted only for the exact paired daemon identity, public
key and origin (including port). Pairing imports cannot grant it. Changing
identity, origin or transport invalidates approval; switching the current
server clears the native connection. The desktop screen offers cancellation,
an approval-revocation switch and a connected unencrypted-LAN indicator.
This network acknowledgement is separate from pairing scope and OS permissions.
For the current attended artifact it also does not replace local session consent.

Before native connection, bounded signed health and authenticated device
checks validate the paired daemon. Each device probe and desktop upgrade uses
a fresh purpose-specific signed nonce. HTTP checks and native WebSocket
clients reject redirects. Android and iOS independently enforce the prepared
transport and source-origin binding; neither installs a universal trust rule.

## Modules And Data Flow

1. `app/store/currentServer.tsx` owns the only current server. Desktop state is
   bound to that ID and a connection generation. Switching servers, leaving
   the route or entering the background closes the old native connection,
   clears retained frames and releases input. Control does not auto-resume.
2. `app/services/remoteDesktop.ts` reuses stored transport resolution and
   device signatures with the dedicated `zen-desktop` purpose. Credentials
   never enter URLs. There is no second pairing system or persistent media
   token.
3. `daemon/server/remote_desktop.go` exposes `/desktop`. Device trust,
   timestamp and nonce checks occur before upgrade. Desktop connections have
   separate ownership, connected to existing device revocation and runtime
   shutdown. They do not subscribe to Brain, Session or chat broadcasts.
4. `daemon/desktop/` owns a bounded session and local helper. Clients cannot
   supply executables, DISPLAY addresses, pipelines or shell commands. Default
   startup launches `desktop-helper` from the running `zen` executable.
   `ZEN_DESKTOP_HELPER` is test-only. Default startup never captures a screen.
5. `daemon/desktop/native/` contains host capture, encoding and permission
   adapters. Helper stdout carries length-delimited packets; stdin accepts
   validated input records. EOF must release held input and terminate the
   helper. No detached descendants are permitted.
6. `app/modules/zen-remote-desktop/` owns native views, networking and decoder
   lifecycle. Android uses MediaCodec and Surface. iOS uses CoreMedia and
   AVSampleBufferDisplayLayer. Neither pixels nor encoded video cross JS.

## Session And Permission Contract

The following wire/helper contract describes the current attended implementation.
The unattended transition and pairing rules above supersede its per-connection
consent requirement for the new default host; they are not yet runtime accepted.

The state progression is `disconnected -> connecting -> sources -> requesting
-> streaming -> connected -> disconnected`. Unconfigured, missing dependency,
unsupported, declined and connection-error states must be truthful. Receiving
a sample is not proof of presentation: only native display readiness or a
render callback can advance the visible state to connected.

Each connection receives a versioned source inventory. Sources are assigned
by the host. The Linux helper offers only the explicitly configured X11 or
Wayland backend and never accepts a client-supplied display or bus address. `start` selects
the source and view/control mode. Codec and resolution negotiation must be
explicit before adding modes beyond the initial fixed H.264 profile. A local
visible prompt identifies the requesting device, source and access scope.
Cancellation produces no video. A local stop control remains visible while
sharing. Consent belongs only to this authenticated connection, source and
mode; it is not reusable by another device or reconnect. Reject repeated
start, stale sources and unknown input types.

The initial wire format uses JSON for status, start, stop and input, and one
complete H.264 Annex-B access unit per binary message, capped at 4 MiB.
SPS/PPS accompany IDR frames. Helper packets contain a big-endian 32-bit
length followed by a type byte: 1 for JSON, 2 for video. GStreamer appsink
sample boundaries establish access units; arbitrary pipe reads do not.
Disable B frames. Start conservatively with an aspect-preserving maximum of
1280x720, 30 fps and 4 Mbps. 1080p60 is an acceptance target, not a default
performance promise.

Input consists of normalized absolute pointer positions, explicit button
and key down/up events, and bounded discrete scroll steps. Validate finite
coordinates, ranges, enums and record sizes on the server. View-only sessions
reject input. The initial shared key subset uses X11 keysym values; other
hosts must map those values explicitly rather than treating them as scan
codes. Backgrounding, focus loss, revocation, stop, timeout and process exit
release only the keys/buttons held by this session. Reconnect requires fresh
selection and consent.

### Ordered Input Delivery

The app uses Expo native-view imperative `sendCommand(generation, sequence,
payload)` and `disconnect(generation)` methods, not a replaceable React
command prop. A shared JS queue snapshots every payload, including down/up
batches, before React can batch renders. It admits at most 32 pending commands
and 32 KiB including the in-flight command, with an 8 KiB per-message bound and
one native call awaiting acknowledgement. The next sequence is dispatched
only after native enqueue succeeds. A two-second acknowledgement timeout,
full queue, missing view or native rejection closes ownership; it never
silently drops a key-up and continues controlling the host.

Both native implementations run these methods on Expo's main queue, require
the connection's generation and next sequence, and clear them on stop.
Background, focus loss, revocation/disconnect status and current-server
switch discard pending JS work. Old acknowledgements do not advance a new
queue, delayed native calls cannot enter a different generation, and reconnect
starts an empty queue with new consent. Native acknowledgement establishes
enqueue ordering, not network delivery, host execution or video presentation.

### Revocation Ordering

The daemon serializes helper startup and each input record through an input
admission gate. Revocation/shutdown publishes retirement before waiting for
the current admitted record, closes helper stdin, then completes media-socket
closure. Each record rechecks trust; queued records from an already validated
batch cannot overtake retirement. Once the manager revocation call returns,
no helper write is active and no further record can be admitted. The write
deadline bounds an already admitted pipe write to one second.

This is an admission barrier, not retroactive cancellation: records written
before retirement may already be in the pipe or OS input queue and can still
take effect. EOF requests release of held input; the helper has a bounded
two-second kill fallback after cleanup begins. Host-side effect timing and
actual key release still require owned desktop/device evidence. Authorization
revocation returns only after its synchronous runtime listeners cross this
barrier; no claim is made that prior physical input effects can be undone.

## Bounded Resources And Feedback

Admit at most one desktop helper per daemon. Limit input messages to 8 KiB
and access units to 4 MiB. Keep native pending decode work bounded to two
samples, with one-at-a-time receive processing on iOS. Apply media write
deadlines and terminate clients that stop receiving. Never discard an H.264
reference frame and continue displaying dependent P frames: wait for a fresh
SPS/PPS/IDR sequence or end the session. Limit the host raw-frame queue to two
frames and discard stale frames only before encoding. Use the encoder's
standard rate-control and low-latency modes, not a custom adaptive algorithm.

Diagnostics should distinguish received, submitted and presented frames,
drops, decoder failures and last-frame age. Host diagnostics should include
access units, bytes, write timeouts and helper exits. Do not claim adaptive
bitrate until its feedback loop is implemented and measured. Resolution
changes require rebuilding decoder format and invalidating the old coordinate
generation; never project input onto a stale source.

## Platforms And Dependencies

This inventory describes the current attended adapters and their dependencies,
not accepted unattended or prelogin capabilities. The Linux host plan above
defines the required extensions and their independent OS evidence gates.

| Platform | Capture, Input And Limits | Build And Acceptance Requirements |
| --- | --- | --- |
| Linux X11 | GStreamer ximagesrc, visible GTK consent and XTest input. Other X11 applications can observe the desktop. | GStreamer 1.22+ core/app/video/ximagesrc/openh264/h264parse, GTK3, X11 and XTest, dynamically linked to system libraries. |
| GNOME/KDE Wayland | Opt-in portal ScreenCast viewing or RemoteDesktop control, granted PipeWire FD/node and typed portal input. ScreenCast alone does not grant control. | Integrated source and private-bus checks, but actual compositor selection/cancellation/revocation/video/input remain unvalidated. Missing grants or backend fail closed. |
| wlroots Wayland | Do not assume its portal supports RemoteDesktop. | Report verified capabilities only; screenshot support does not establish remote control. |
| Linux headless | Only an explicitly created isolated virtual desktop is usable. | Session-owned X server and test application. Daemon startup does not implicitly create a desktop. |
| macOS | ScreenCaptureKit picker, realtime VideoToolbox H.264, Accessibility and CGEvent. Recording and input permissions are separate. | macOS SDK, Xcode and an authorized test host. Linux compilation cannot prove TCC behavior. |
| Windows | WGC/DXGI, Media Foundation H.264 and SendInput, respecting UIPI, interactive sessions and secure desktop boundaries. | A native Zen daemon delivery path is also needed. Existing Unix PTY/tmux dependencies and WSL do not establish Windows desktop hosting. |
| Android | System MediaCodec, Surface and OkHttp WebSocket. | Java 17, Android SDK/NDK and Expo 57/RN 0.86 native build; hardware decoding, rotation and background tests on a device. |
| iOS | URLSessionWebSocketTask, CoreMedia and AVSampleBufferDisplayLayer. | Xcode/iOS SDK, native Pod build and actual device presentation. Expo export is not native proof. |

License inventory: Zen is Apache-2.0; GStreamer and GTK are dynamically linked
LGPL-2.1+ system libraries; OpenH264 is BSD, with source licensing and H.264
patent/binary distribution obligations reviewed separately; X11/XTest are
MIT; OkHttp is Apache-2.0; Expo is MIT. The numeric private-address parser is
ipaddr.js 2.3.0 (MIT), pinned in the app dependency lock. Its complete copyright
and license text is packaged as `IPADDR-MIT.txt` in Android assets and iOS
resources. Mobile codecs come from platform SDKs.
The inspected Linux environment has GStreamer 1.28.2, GTK 3.24.52, X11 1.8.13
and Xtst 1.2.5; this does not validate all minimum versions. Do not silently
select GPL x264, GPL/AGPL remote-control stacks or restricted virtual-input
drivers. Before distribution, generate an artifact-specific SBOM and retain
actual plugin paths, versions, licenses and SDK versions. Record the selected
hardware encoder, not merely installed VAAPI/NVENC/VideoToolbox/MF capability.
The initial OpenH264 path is software encoding, not a hardware claim.

## Current Implementation Boundary

### Manual LAN Client Verification

The explicit manual-LAN contract has been exercised by a standalone x86 Android
APK against an owned server bound to a numeric private LAN interface, without
ADB port reversal or a pinned loopback proxy. After local X11 consent, the
native client presented 1280x720 software-encoded video at approximately30fps
over110seconds. Two screenshots contained14,950 changed pixels inside the
video region; host counters confirmed a click, six exact keyboard events and
three scroll events. These are emulator functional results, not physical
ARM64, hardware encoding or input-to-photon measurements.

The same run checked network-consent cancellation, persisted approval after
app relaunch, approval revocation and cleared video, separate-server approval,
and revoked-device rejection during preflight. Host input was released after
revocation. Native JVM policy regressions and shared policy/storage tests
cover origin, paired identity, transport downgrade and stale-owner writes.
An isolated cold deep-link launch produced a React Native Fabric native crash;
normal relaunch and a serialized cold-route repeat succeeded. The crash remains
a recorded startup stability risk, not a proven transport-policy failure.
Native iOS compilation/runtime still require an assigned macOS/Xcode host.

The retained implementation currently provides the X11 helper and shared
Android/iOS native client source. The Wayland portal protocol is now linked
into the Linux helper with a granted-FD/node `pipewiresrc` binding and typed
portal input. This is an integrated, compiled adapter increment, not an
accepted compositor runtime. Private-bus tests inspect source bindings only
in GStreamer NULL state; no fake video is generated. macOS hosting, Windows
hosting and hardware encoder selection above are planned adapters, not
implemented support. Missing host adapters must remain unavailable. The
initial helper uses system OpenH264 software encoding at up to 720p30; it
does not meet the 1080p60 acceptance target by construction.

### Verification Status

| Platform | Implemented | Built | Runtime Verified | Performance Measured | Released |
| --- | --- | --- | --- | --- | --- |
| Linux X11 / explicit virtual X11 | Capture, local consent and input helper; authenticated daemon endpoint | Strict-warning helper compilation and isolated real-auth fixture | Owned Xvfb/GTK capture, consent/cancel, view-only and actual mouse/key/scroll effects verified through Android | 1280x720 H.264/OpenH264 software; 5,015 access units over 167.124 seconds, approximately 30 fps | No |
| GNOME/KDE/wlroots Wayland | Explicit portal/FD/node capture and portal input integrated; unsupported portal capabilities fail closed | Linux helper compiled with strict warnings; private-bus consent/cancel/revoke/source-binding tests pass | No compositor, real PipeWire stream, phone presentation or input acceptance; no universal wlroots claim | No | No |
| macOS host | No ScreenCaptureKit/VideoToolbox/input adapter | No; requires macOS SDK and host | No; recording and Accessibility grants untested | No | No |
| Windows host | No native daemon delivery or desktop adapter | No; requires Windows toolchain and host | No | No | No |
| Android client | Native MediaCodec view, ordered imperative input and configured X11/Wayland source interface | Corrected standalone x86_64 debug APK verified; 13 JVM tests, 33 shared tests, typecheck and Android export pass. Original ARM64 APK is not corrected | Owned API35 emulator: sustained rendered frames and changing pixels; exact rapid keyboard input; cancel/view-only/stop; held-button release on background, server switch and revocation; revoked reconnect rejected | 5,010 reported rendered callbacks; stable 30.002 fps over 165.991 seconds. 200 click-to-screenshot samples: p50 353 ms, p95 401 ms, p99 439 ms; not physical latency | No |
| iOS client | Native client and shared keyboard corrections present | JavaScript export passes; no native build without Xcode/iOS SDK | No; requires an owned simulator/device | No | No |

Android's module minimum follows the app's configured minimum (currently API
24); it does not independently raise supported-device requirements. Native
build and runtime checks must be resource-bounded and serial. A generated APK,
an export, a local server-ready message or a before-image does not establish
native video presentation or input effects. Missing OS adapters remain
unsupported rather than falling back to another desktop or permission model.

### Linux Host Configuration

Build a desktop-capable `zen` with the repository local recipe (`bun run daemon:build`
or `cd daemon && go run ./cmd/zen-dev`). That enables CGO and `-tags zen_desktop`
when `pkg-config` finds GTK3, GStreamer app/video, GIO Unix, X11 and XTest.
Linux amd64 production builds (`scripts/build-daemon-linux.sh` on a Linux amd64
host) use the same linked ELF. `make -C daemon/desktop/native` remains a
standalone encoder/portal compile check; it is not a user-facing helper to
download or configure. Runtime requires those shared libraries plus GStreamer
capture, conversion, H.264 encoder/parser and app plugins. Keep the verified
`zen` outside temporary Worker directories before configuring a persistent
daemon. Do not point a user daemon at an instrumented test wrapper or an owned
test display. linux/arm64 and Darwin `CGO_ENABLED=0` archives do not include
native roles; `zen doctor` reports that.

`ZEN_DESKTOP_HELPER` is not required. `ZEN_DESKTOP_BACKEND` defaults to `x11`;
`ZEN_DESKTOP_DISPLAY` explicitly selects the X display. For Wayland, set `ZEN_DESKTOP_BACKEND=wayland`, select the owned
Wayland display/socket in `ZEN_DESKTOP_DISPLAY`, and set the matching session's
explicit D-Bus address in `ZEN_DESKTOP_BUS_ADDRESS`. The helper sets GTK to
Wayland only and uses that bus for its portal and GTK session environment.
No compositor, session bus or virtual/headless desktop is discovered or
created automatically. These settings do not authorize operating a personal
desktop; host consent and an appropriate session are still required.

A terminal-only login or a display-manager greeter cannot be served by this
attended helper. This is an implementation gap, not a requirement that someone
log in locally. Use the approved broker/greeter implementation above once built
and verified; never point the attended helper at the greeter as a workaround.
A missing helper or display is shown as an unsupported host, not as an
encrypted-network error.
Helper environment changes require restarting the existing daemon launch
owner with its original state directory, pairing identity and network options;
do not start a second daemon or supervisor. Restart interrupts active client
connections but does not grant screen access.

The helper first identifies the requesting device in a local GTK view/control
prompt, then requests exactly one monitor through the portal. View-only uses
ScreenCast; control additionally requires both keyboard and pointer through
RemoteDesktop. No persistent restore token, clipboard, EIS connection or
XTest fallback is used for Wayland. A portal without the requested interface,
monitor identity/logical dimensions or full control grant is unavailable for
that request. The source inventory describes configured requests, not a
validated compositor capability guarantee.

`pipewiresrc` is bound to the granted FD and node using typed properties. The
installed PipeWire1.6.4 plugin duplicates the FD for its own connection.
The original grant stays owned by the portal session until teardown; local
stop/EOF/cancellation/revocation close the session and its virtual devices.
No default PipeWire remote, last-frame resend or keepalive frames are used.
The helper requires the plugin's FD/path, buffer and disconnect policy
properties, failing closed when unavailable. It uses the same software
OpenH264 access-unit format and media WebSocket as X11, capped at 720p30.
Logical/physical aspect mismatch or capture pixel-size change ends the
Wayland session to prevent stale coordinate mapping. Compositor-scale,
rotation, frame pacing and real permission behavior remain runtime gates.

Source resize ends the X11 session and requires a fresh connection and local
grant. Pending local consent expires after 60 seconds. Android bounds pending
encoded access units/status work to two and outbound input to 32 KiB including
the next message; iOS admits at most
four outstanding sends and reads one access unit at a time. Android waits up
to two seconds on the socket reader for a decode/status slot, including codec
startup, without enlarging the two-item work queue. Expired backpressure
terminates the session instead of discarding reference frames. These are
bounded fail-closed policies, not adaptive bitrate or seamless recovery.
Both clients reject host-supplied `connected` state and close on terminal
status without waiting for a second transport event. Android clears retained
connection credentials on stop. Android's current native source compiles,
including the imperative view methods and source-inventory forwarding; four
Annex-B parser unit tests pass. iOS wiring is still source-reviewed only.
The shared input queue has dynamic model tests. Model/parser tests do not
verify codec presentation or prove bridge/device lifecycle. An actual Android
emulator run initially disconnected during codec startup; later corrected
runs established sustained native rendered callbacks, changing screen pixels
and actual controlled-desktop input effects.

Android's pinned TLS proxy now treats socket-close, handshake and executor
shutdown failures as connection-local termination rather than allowing an
uncaught forwarding-thread exception to crash the application. A failing
pump closes both sockets to wake its peer; clean EOF retains half-close
semantics. TLS1.3/SPKI verification and loopback ownership remain unchanged.
iOS already stops its bridge on receive/send errors. JVM regressions cover
closed-socket handling, byte-preserving EOF and on-demand admission. Android
desktop logs expose queue/decoder failures and debug-build state/frame counts
without credentials or pixel payloads. The corrected-proxy APK reproduced a
three-frame termination: Android's immediate two-item admission rejection
preceded codec creation, while the real host helper exited successfully with
empty stderr. Bounded admission now propagates backpressure to the reader;
tests cover startup waiting, timeout and stale-generation slot release.
That fix reached a first native frame-render callback, then exposed a second
startup failure: a single 10 ms input-buffer wait was treated as fatal.
Android now drains output while retrying input-buffer admission for at most
two seconds, with generation cancellation. Focused tests cover transient
buffer unavailability, deadline expiry and stale-buffer rejection.
The corrected x86 client sustained more than 5,000 native frame callbacks
against the real GTK target. Screenshot comparison found 6,193 changed pixels
inside the video region; no placeholder or mocked image was used. Reported
native dropped counters remained zero, which is not complete pipeline drop
accounting. The approximately 141 kbit/s steady H.264 payload rate reflects
this low-complexity test scene and excludes WebSocket/TLS overhead.

Two hundred sequential native taps each advanced the host's binary click
marker and were observed in real client screenshots. The reported p50/p95/p99
values use one host monotonic clock from ADB input invocation through PNG
inspection. They include command, network, decode and screenshot overhead;
they are observation bounds, not input-to-photon or hardware performance.
This 720p30 software-codec run does not satisfy the 1080p60 acceptance target.

### Checkpoint Gaps

This is a local WIP checkpoint, not acceptance or release. Before acceptance:

- The JS queue now has dynamic tests for same-tick down/up, payload snapshots,
  count/byte saturation, timeout, rejection and lifecycle cancellation without
  reconnect replay. Android live bridge calls and held-pointer lifecycle were
  exercised in the owned guest; physical Android and native iOS verification
  remain open. Model/parser tests alone do not prove live bridge calls.
- Broaden the passing Android background/focus/current-server tests to surface
  recreation, rotation and delayed native callbacks. Held-button release was
  measured at the owned host, not inferred from a disconnected phone label.
- The daemon input admission barrier has dynamic in-flight writer tests and
  isolated helper lifecycle tests for revocation/shutdown. Measure the
  remaining pre-retirement pipe/OS input effects and actual held-key release
  on an owned desktop; do not infer physical-effect timing from socket close.
- Exercise malformed status, terminal status followed by video, queue
  saturation and no-first-frame timeout. Android counts presented frames;
  iOS readiness/submission and receive age are not presented-frame metrics.

Local standalone Android builds now pass using the existing generated project,
Java17, Gradle9.3.1, SDK36/NDK27.1, offline mode, one CPU/worker, parallel
execution disabled and Kotlin in-process. The proven full-build recipe uses
Gradle heap2048MiB/metaspace512MiB, Node1536MiB, explicit Ninja-j1 and
test heap128MiB/one fork. Both the actual package and native JVM tests are
verified; embedded JS is built from the real app entry without a Metro server.
Builds and emulator phases must remain serial and use their separately
authorized aggregate/headroom guards; host availability alone does not waive
the cache-inclusive scope ceiling.
The repository already configures `ci.yml:android-native` on ubuntu-latest
and `ci.yml:ios-native` on macos-26. The latter produces an unsigned arm64
simulator app; the former assembles the Android app. Neither job is a test
pass for the current local source, and no CI dispatch or push is implied.
The local SDK has the current RN-required API36/build-tools36/NDK27.1;
CI currently requests API35 explicitly and needs that mismatch reviewed
before a bounded build. Both jobs need explicit worker/native parallel caps;
the Android generated defaults enable parallel Gradle and multiple ABIs.
The standalone full-debug packaging graph uses a guarded offline
`:app:assembleDebug` invocation on this same generated project, with
an explicitly selected `reactNativeArchitectures` of `arm64-v8a` or `x86_64`,
matching verified cached Ghostty libraries and the intended device. This is a compile target;
choosing it does not require a device or imply an installation. Retain the
successful module JVM/worker settings and the original aggregate memory and
host-headroom guard. Explicitly constrain actual Ninja invocations to `-j1`;
`CMAKE_BUILD_PARALLEL_LEVEL=1` alone was not sufficient in the earlier build.
Use isolated build/temp output and verified existing matching-ABI native libraries,
not a new Ghostty build or a different resource pool hidden in the gate.

For a self-contained debug APK, a temporary packaging init script must clear
React's debuggable-variant exclusion and set the offline JS bundle worker
count to one. Pass `-PzenStandalone=true` as well: the generated
`BuildConfig.ZEN_STANDALONE` disables React host development support so an
installed immutable debug APK cannot silently load a developer bundle instead
of its embedded JS. Normal local development keeps this property false. Verify
the flag and host factory in DEX, not just the presence of bundled assets.
That is build-time JS bundling, not starting or reusing a Metro
server. The module-only task allowlist cannot simply be reused for packaging.
The first execution of this standalone graph failed during build-time JS
bundling at the configured 512 MiB Node heap limit, before APK generation or
native CMake/Ninja builds. The whole-scope guard did not trip. No APK signature,
embedded-bundle, DEX registration or packaged ABI check passed from that run.
The Ninja wrapper's option-clamping check passed, but actual native build
invocations were not reached. Repeating the same graph or increasing limits
is not an accepted recovery plan; first establish a bounded, demonstrable
bundler-memory reduction. A guard stop or offline cache miss likewise remains
a failure, not permission to download dependencies or change resource policy.
Any install candidate must pass integrity, matching-ABI library inventory, embedded JS
and `apksigner verify` checks before installation is considered.

Isolated builds set `EXPO_NO_DOTENV=1`; the app's custom config loader now
honors that flag rather than importing personal `.env.local` values into the
build environment. Normal local configuration remains unchanged when the
flag is absent. Debug packaging uses only the existing/local debug key, never
release signing credentials. The generated project's cached version may lag
tracked `app/app.base.json`; packaging must set and verify the current debug
identity without silently accepting a stale cached APK.
Runtime needs an explicitly owned Android emulator or device identified by
ADB serial. The installed API35 emulator enforces a2GiB guest minimum; a
two-vCPU owned guest has now completed real video/input verification. Earlier
8 GiB and 12 GiB guarded attempts stopped before acceptance. A subsequently
authorized runtime-only ceiling of 16 GiB total and 6 GiB anonymous memory,
with a 12 GiB host floor and 18 GiB host preflight, completed the final run
without guard, OOM or sustained-PSI stops. Builds remained serial under 8 GiB.
No guest reduction, swap, pool or live-service policy change was used.
Physical-device performance still requires an owned physical
device and is not established by this emulator. An iOS build needs an assigned
macOS/Xcode executor and an owned device/simulator identified by UDID; neither
is available in the current environment. Use a newly owned virtual X11 desktop
and fixture process for video/input verification, never the personal desktop.
Do not create resource pools, change live services or raise limits to bypass
these missing assignments. Wayland still needs an owned GNOME/KDE compositor
and portal backend for actual integration execution; macOS/Windows hosting and hardware encoding
remain separate implementation and OS-specific verification gates.

The current keyboard supports printable ASCII and explicit special-key
events, not Unicode composition or arbitrary keyboard layouts. Shared keyboard
input tracks cumulative native text changes instead of clearing and replaying
each value. Edits are split into bounded batches without splitting key pairs;
imperative key buttons do not rewrite native text history. The local input
history is capped at 1,024 characters and resets with keyboard/owner changes.
Keyboard avoidance measures the route's actual screen offset; Android
screenshots verify controls remain visible above the IME. A dedicated drag
mode holds the primary pointer button until gesture end; pinch and gesture
cancellation release it. Actual held-button state changed from 256 to zero
after background, current-server switch and revocation, and late pointer-up
events did not restore input. Model tests separately cover delayed bridge
acknowledgements; arbitrary delayed native bridge calls were not injected in
the emulator. Physical-device zoom/rotation and native iOS interaction remain
unverified.

## Mobile Interface

Enter Remote Desktop through existing navigation and show the current host.
Source and view/control selection must be visible. Never show a connected
placeholder. The primary surface is an unframed native view with essential
exit, stop, keyboard, pointer/pan and zoom-reset controls. Use the existing
icon library and accessibility labels. Single-finger pointer movement and
tap, long-press right-click, scrolling, pan and pinch must not accidentally
trigger each other. Verify keyboard, dragging, rotation and letterbox-aware
coordinates through actual interactions, not the presence of buttons.

## Attended Baseline Tests

These stages and cases describe the existing attended baseline. The paired-default
unattended rollout and acceptance requirements are specified above; fresh local
consent on reconnect below applies only to explicitly attended sessions.

1. Audit transport and establish this plan, permission boundaries, lifecycle
   and observable contracts before the Linux/native vertical implementation.
2. Implement X11 consent, stop, continuous encoding and input; shared native
   mobile views, current-server routing and disconnect cleanup. The owned
   test desktop displays readable text, motion and visible input counters.
3. Implement and exercise Wayland portals, macOS capture/permissions and the
   native Windows daemon/capture path. Missing hardware leaves acceptance
   open but does not stop independent work on other platforms.
4. Add and measure hardware encoding, native performance, existing LAN/tunnel
   behavior, impaired-network recovery and chat isolation. Consider release
   only after all required native device gates pass.

| Given / When | Then / Evidence |
| --- | --- |
| Unpaired device, wrong signature purpose or replayed nonce requests media | HTTP 401 before upgrade, with no helper, input or frames. |
| Host cancels, grants viewing only, or client selects an absent source | Cancellation does not capture; viewing cannot inject input; invalid sources cannot start a helper. |
| Phone selects a source and host explicitly approves | A native client presents continuously changing real test-desktop frames. Retain desktop/mobile screenshots and video, not mocks. |
| Authorized client clicks, drags, types and scrolls | Test-application event counters and content change, correlated with input IDs. |
| Another device/connection or repeated start attempts reuse | Existing consent cannot be taken over and a second capture process cannot start. |
| Device revoked, host stops, client backgrounds/switches/leaves/disconnects | Old frames clear, held input releases and helper exits. The old connection cannot regain access. |
| Slow receiver, malformed frame, oversized input or helper crash | Bounded memory and deterministic closure/error state without chat write-lock contention. |
| Rotation, zoom, reconnect or source-size change | Correct coordinates, no old-generation state, and fresh consent on reconnect. |

## Verification And Performance

Required gates include affected full Go and race tests, Bun behavior tests,
TypeScript, native contract tests, route hygiene, both Expo exports, and native
Android/iOS builds and runtime tests. Do not call real AI, control existing
daemon/Metro processes, inspect personal screens, alter user networking or
access signing secrets. Use `$ZEN_BUILD_TMPDIR` for large builds. Test state,
ports, desktops and processes belong to this Session and are cleaned up.

The focused input model can also run without JIT or Node's WebAssembly-based
TypeScript loader: `node --jitless --max-old-space-size=96
scripts/test-desktop-input.cjs`. It transpiles only the named model and test
files using the installed TypeScript library and executes the actual tests
with `node:test` in one process. This is behavior evidence, not a substitute
for typechecking or native runtime gates. Apply process CPU/data/time caps
appropriate to the assigned executor; do not treat a runner abort before
tests as a code assertion failure or increase resource limits to force it.

The initial LAN1080p60 targets are sustained motion presentation of at least
55 fps and input-to-visible p95 at most 100 ms over at least 200 samples.
Record resolution, encoder/hardware, bitrate, received/decoded/presented FPS,
drops, bandwidth, host/client CPU, temperature and duration. A 720p30 software
run cannot meet or replace that target. Measure tunnel throughput, recovery
and interaction tails separately, alongside chat/Terminal response latency.

Input-to-visible measurements need a client monotonic clock, unique input ID
and identifiable test-desktop response at actual presentation, not at send or
receive. Cross-host capture-to-display measurements require clock calibration
and stated uncertainty. Without a high-speed camera, input-to-photon remains
unverified. Mark unsampled metrics unknown; RTT, encoding FPS and averages
are not substitutes. Report each platform separately as documented,
implemented, built, runtime verified, performance measured and released.
