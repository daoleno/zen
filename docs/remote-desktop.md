# Remote Desktop

## Outcome And Boundaries

Operate an explicitly selected desktop on the current Zen server from a phone.
Host priority is Linux, macOS, then Windows. Android and iOS share the same
product contract; web is outside scope. The first scope includes viewing,
mouse, keyboard, scrolling, mobile zoom and pan. Audio, clipboard, file
transfer, gamepads, multi-monitor composition, login screens, elevation and
unattended access are excluded. Full-desktop access is not application or
window isolation.

This plan distinguishes intended behavior from implementation and acceptance.
A platform is not accepted without a native build and actual capture,
presentation and input evidence on that platform. Software encoding,
emulators, Expo exports and static contracts do not replace hardware
acceptance. Private execution reports, screenshots and raw measurements
belong in the Brain worklog, not this repository. Existing releases remain
immutable; implementation does not authorize publishing or deployment.

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
Link loopback origin, without weakening certificate validation.

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
   supply executables, DISPLAY addresses, pipelines or shell commands. The
   host explicitly configures an absolute helper path. Default startup never
   captures a screen.
5. `daemon/desktop/native/` contains host capture, encoding and permission
   adapters. Helper stdout carries length-delimited packets; stdin accepts
   validated input records. EOF must release held input and terminate the
   helper. No detached descendants are permitted.
6. `app/modules/zen-remote-desktop/` owns native views, networking and decoder
   lifecycle. Android uses MediaCodec and Surface. iOS uses CoreMedia and
   AVSampleBufferDisplayLayer. Neither pixels nor encoded video cross JS.

## Session And Permission Contract

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
MIT; OkHttp is Apache-2.0; Expo is MIT. Mobile codecs come from platform SDKs.
The inspected Linux environment has GStreamer 1.28.2, GTK 3.24.52, X11 1.8.13
and Xtst 1.2.5; this does not validate all minimum versions. Do not silently
select GPL x264, GPL/AGPL remote-control stacks or restricted virtual-input
drivers. Before distribution, generate an artifact-specific SBOM and retain
actual plugin paths, versions, licenses and SDK versions. Record the selected
hardware encoder, not merely installed VAAPI/NVENC/VideoToolbox/MF capability.
The initial OpenH264 path is software encoding, not a hardware claim.

## Current Implementation Boundary

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
| Linux X11 / explicit virtual X11 | Capture, local consent and input helper; authenticated daemon endpoint | Strict-warning helper compilation and daemon test executable | Not accepted: native phone video/input assertions remain unexecuted | No accepted FPS, latency or hardware-encoder measurements | No |
| GNOME/KDE/wlroots Wayland | Explicit portal/FD/node capture and portal input integrated; unsupported portal capabilities fail closed | Linux helper compiled with strict warnings; private-bus consent/cancel/revoke/source-binding tests pass | No compositor, real PipeWire stream, phone presentation or input acceptance; no universal wlroots claim | No | No |
| macOS host | No ScreenCaptureKit/VideoToolbox/input adapter | No; requires macOS SDK and host | No; recording and Accessibility grants untested | No | No |
| Windows host | No native daemon delivery or desktop adapter | No; requires Windows toolchain and host | No | No | No |
| Android client | Native MediaCodec view, ordered imperative input and configured X11/Wayland source interface | Current module Kotlin compilation and four parser unit tests pass under a guarded offline Java 17 build. Classes JAR only, not an APK/AAR. Earlier interrupted harness APK fails signature verification and is not installable | No native UI/codec/bridge execution or device acceptance | No | No |
| iOS client | Native client source present | No native build; requires Xcode/iOS SDK | No; requires an owned simulator/device | No | No |

Android's module minimum follows the app's configured minimum (currently API
24); it does not independently raise supported-device requirements. Native
build and runtime checks must be resource-bounded and serial. A generated APK,
an export, a local server-ready message or a before-image does not establish
native video presentation or input effects. Missing OS adapters remain
unsupported rather than falling back to another desktop or permission model.

### Linux Host Configuration

`ZEN_DESKTOP_HELPER` remains an administrator-configured absolute helper path.
`ZEN_DESKTOP_BACKEND` defaults to `x11`; `ZEN_DESKTOP_DISPLAY` explicitly selects
the X display. For Wayland, set `ZEN_DESKTOP_BACKEND=wayland`, select the owned
Wayland display/socket in `ZEN_DESKTOP_DISPLAY`, and set the matching session's
explicit D-Bus address in `ZEN_DESKTOP_BUS_ADDRESS`. The helper sets GTK to
Wayland only and uses that bus for its portal and GTK session environment.
No compositor, session bus or virtual/headless desktop is discovered or
created automatically. These settings do not authorize operating a personal
desktop; host consent and an appropriate session are still required.

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
four outstanding sends and reads one access unit at a time. Backpressure
terminates the session instead of discarding reference frames. These are
bounded fail-closed policies, not adaptive bitrate or seamless recovery.
Both clients reject host-supplied `connected` state and close on terminal
status without waiting for a second transport event. Android clears retained
connection credentials on stop. Android's current native source compiles,
including the imperative view methods and source-inventory forwarding; four
Annex-B parser unit tests pass. iOS wiring is still source-reviewed only.
The shared input queue has dynamic model tests. None of these gates executes
native UI, verifies codec presentation or proves bridge/device lifecycle.

### Checkpoint Gaps

This is a local WIP checkpoint, not acceptance or release. Before acceptance:

- The JS queue now has dynamic tests for same-tick down/up, payload snapshots,
  count/byte saturation, timeout, rejection and lifecycle cancellation without
  reconnect replay. Android imperative method wiring now compiles; iOS still
  needs native compilation, and both need device verification. Model/parser
  tests do not prove live bridge calls or native UI.
- Prove background, focus loss, surface recreation and current-server switch
  cannot reopen old credentials or deliver stale callbacks. Verify input
  release at the owned host, not just a disconnected phone label.
- The daemon input admission barrier has dynamic in-flight writer tests and
  isolated helper lifecycle tests for revocation/shutdown. Measure the
  remaining pre-retirement pipe/OS input effects and actual held-key release
  on an owned desktop; do not infer physical-effect timing from socket close.
- Exercise malformed status, terminal status followed by video, queue
  saturation and no-first-frame timeout. Android counts presented frames;
  iOS readiness/submission and receive age are not presented-frame metrics.

The local Android module gate has passed using the existing generated project,
Java 17, Gradle 9.3.1, SDK 36/NDK 27.1, offline mode, one CPU/worker, parallel
execution disabled and Kotlin in-process. Gradle heap was 768 MiB, metaspace
256 MiB, direct/code cache 64 MiB each; test heap was 128 MiB with one fork.
Only module Kotlin compilation and unit tests were requested, plus necessary
dependency compilation. No APK, app assemble, Expo bundle or native UI ran.
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
`-PreactNativeArchitectures=arm64-v8a`. This is a documented compile target;
choosing it does not require a device or imply an installation. Retain the
successful module JVM/worker settings and the original aggregate memory and
host-headroom guard. Explicitly constrain actual Ninja invocations to `-j1`;
`CMAKE_BUILD_PARALLEL_LEVEL=1` alone was not sufficient in the earlier build.
Use isolated build/temp output and verified existing arm64 native libraries,
not a new Ghostty build or a different resource pool hidden in the gate.

For a self-contained debug APK, a temporary packaging init script must clear
React's debuggable-variant exclusion and set the offline JS bundle worker
count to one. That is build-time JS bundling, not starting or reusing a Metro
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
Any eventual APK must pass integrity, arm64 library inventory, embedded JS
and `apksigner verify` checks before installation is considered.

Isolated builds set `EXPO_NO_DOTENV=1`; the app's custom config loader now
honors that flag rather than importing personal `.env.local` values into the
build environment. Normal local configuration remains unchanged when the
flag is absent. Debug packaging uses only the existing/local debug key, never
release signing credentials. The generated project's cached version may lag
tracked `app/app.base.json`; packaging must set and verify the current debug
identity without silently accepting a stale cached APK.
Runtime needs an explicitly owned physical
Android device identified by ADB serial; no device is assigned. The only
installed API35 emulator enforces a 2 GiB guest minimum, so repeating the
previous lower-memory boot is not a valid plan. An iOS build needs an assigned
macOS/Xcode executor and an owned device/simulator identified by UDID; neither
is available in the current environment. Use a newly owned virtual X11 desktop
and fixture process for video/input verification, never the personal desktop.
Do not create resource pools, change live services or raise limits to bypass
these missing assignments. Wayland still needs an owned GNOME/KDE compositor
and portal backend for actual integration execution; macOS/Windows hosting and hardware encoding
remain separate implementation and OS-specific verification gates.

The current keyboard supports printable ASCII and explicit special-key
events, not Unicode composition or arbitrary keyboard layouts. A dedicated
drag mode holds the primary pointer button until gesture end; pinch and
gesture cancellation release it. Device interaction verification remains
necessary for zoom, rotation, keyboard focus and background transitions.

## Mobile Interface

Enter Remote Desktop through existing navigation and show the current host.
Source and view/control selection must be visible. Never show a connected
placeholder. The primary surface is an unframed native view with essential
exit, stop, keyboard, pointer/pan and zoom-reset controls. Use the existing
icon library and accessibility labels. Single-finger pointer movement and
tap, long-press right-click, scrolling, pan and pinch must not accidentally
trigger each other. Verify keyboard, dragging, rotation and letterbox-aware
coordinates through actual interactions, not the presence of buttons.

## Implementation Stages And BDD

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
