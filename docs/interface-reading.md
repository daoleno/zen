# Interface Reading

Android and iOS share one reading policy. At the newest edge, appended and
streaming content remains followed. A manual move into history detaches that
intent. Data, layout, keyboard and connection events cannot attach it again;
an explicit return to latest or intentional send can establish a new intent.
Subsequent manual scrolling cancels pending send-focus effects.

## Position Ownership

`usePinnedTimeline` owns the reading intent and stable message anchor. The
inverted native list preserves mounted content during drag and momentum without
a native auto-follow threshold. At rest, the same hook reconciles measured
position, including Android's keyboard-decorator translation. Native screen
measurements and the inset at capture prevent a second JS compensation from
undoing a native adjustment. Old measurement callbacks are fenced on unmount
and conversation replacement. Geometry work is coalesced to an animation frame,
not a timer-based retry or repeated scroll loop.

Navigation retains bounded, process-local metadata for up to 32 server and
conversation identities: intent, message ID, intra-item offset and nearby IDs.
No transcript copy is stored. If an anchor disappears, a surviving neighbor is
preferred over the newest message. Cold process death is not a persistent
reading-position guarantee.

Brain uses its explicit thread scope across host changes. Unscoped Terminal
views bind to the confirmed provider conversation ID, retaining that identity
through transient unavailable snapshots. Existing optimistic/provider-echo
aliases rebind an anchor instead of treating the same logical message as deleted.

A restored variable-height list waits for the first nonempty history snapshot,
then measures at most eight preceding items around the saved anchor. Initial
rendering is bounded to ten rows. Scrolling toward newer content reveals the
existing rows in bounded increments; the explicit latest control removes that
window boundary. The complete conversation remains in its existing source owner.

## Image Preview

File references use the production `SessionFilePreviewSheet`, generation-bound
metadata and binary source. Its request lifetime rejects responses after close
or context replacement. No prewarming or automatic second-open retry is used.
An unavailable image remains an explicit file error rather than a blank success.

## Verification

`timelineReadingHooks.test.tsx` runs the actual React hook with controlled native
geometry. It covers history mutation, native momentum, navigation, server
isolation, empty reconnect, delayed focus, inset compensation and stale native
callbacks. `sessionFilePreviewLifecycle.test.tsx` mounts the actual preview
component for cold first open, second open and late-response rejection.

The existing development-only screenshot route has a `reading` scenario:

```text
zen://screenshot-demo?demo=1&state=reading
```

It requires `EXPO_PUBLIC_ZEN_SCREENSHOT_DEMO=1` in a development build. It uses
the production timeline/keyboard/preview components with isolated messages and
normal, large and tall PNG assets plus a large JPEG. History initially arrives after mount, like a
real subscription. Fixture actions change source data or navigation; they do not
scroll the list to manufacture a passing anchor. Only the explicit Latest
command requests latest-follow. Image metadata/binary loading is injected for
the fixture, so this does not replace authentication/transport coverage.

Run `bun test app`, `cd app && bunx tsc --noEmit`, and Android/iOS Expo exports.
Native PID/log and before/after screen evidence remain necessary for crash,
first-image, scrolling and keyboard claims. Export success is not a signed
client release or iOS device proof.
