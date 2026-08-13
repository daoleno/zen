import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  createStructuredChatKeyboardLifecycleGate,
  dispatchStructuredChatKeyboardLifecycleEvent,
  reduceStructuredChatKeyboardLifecycleGate,
  structuredChatEffectiveClearanceForKeyboardLifecycle,
  structuredChatGatedOverlayTranslateY,
  structuredChatKeyboardGeometryIsOpen,
  structuredChatKeyboardLifecycleGateOpen,
  structuredChatScrollClearance,
  type StructuredChatInsetPlatform,
  type StructuredChatKeyboardLifecycleGate,
} from "./chatKeyboardOverlayPolicy";

const KEYBOARD_HEIGHT = 320;
const COMPOSER_HEIGHT = 76;

const FOCUS_BIND_OPEN = {
  type: "composer_focus_bind" as const,
  height: -KEYBOARD_HEIGHT,
  progress: 1,
};

const FOCUS_BIND_CLOSED = {
  type: "composer_focus_bind" as const,
  height: 0,
  progress: 0,
};

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

function mountedGate() {
  return createStructuredChatKeyboardLifecycleGate({
    enabled: true,
    appActive: true,
  });
}

function focusGate(gate: StructuredChatKeyboardLifecycleGate) {
  return reduceStructuredChatKeyboardLifecycleGate(gate, {
    type: "composer_focus",
    focused: true,
  });
}

function nativeSample(
  gate: StructuredChatKeyboardLifecycleGate,
  input: {
    sourceEpoch?: number;
    height?: number;
    progress?: number;
    updatesGeometry?: boolean;
  } = {},
) {
  return reduceStructuredChatKeyboardLifecycleGate(gate, {
    type: "native_sample",
    sourceEpoch: input.sourceEpoch ?? gate.epoch,
    height: input.height ?? KEYBOARD_HEIGHT,
    progress: input.progress ?? 1,
    updatesGeometry: input.updatesGeometry ?? true,
  });
}

function activeGate() {
  const focused = focusGate(mountedGate());
  return nativeSample(focused);
}

function gateHolder(gate: StructuredChatKeyboardLifecycleGate) {
  return { value: gate };
}

function dispatch(
  holder: { value: StructuredChatKeyboardLifecycleGate },
  event: Parameters<typeof dispatchStructuredChatKeyboardLifecycleEvent>[1],
) {
  return dispatchStructuredChatKeyboardLifecycleEvent(holder, event);
}

function dispatchNativeSample(
  holder: { value: StructuredChatKeyboardLifecycleGate },
  input: {
    sourceEpoch?: number;
    height?: number;
    progress?: number;
    updatesGeometry?: boolean;
  } = {},
) {
  holder.value = reduceStructuredChatKeyboardLifecycleGate(holder.value, {
    type: "native_sample",
    sourceEpoch: input.sourceEpoch ?? holder.value.epoch,
    height: input.height ?? KEYBOARD_HEIGHT,
    progress: input.progress ?? 1,
    updatesGeometry: input.updatesGeometry ?? true,
  });
}

function translation(gate: StructuredChatKeyboardLifecycleGate) {
  return structuredChatGatedOverlayTranslateY({
    gate,
    keyboardVerticalOffset: 0,
  });
}

describe("structured chat keyboard lifecycle gate", () => {
  test.each(["android", "ios"] as const)(
    "%s first mount binds pre-existing open geometry only under Composer focus",
    (platform) => {
      const mounted = mountedGate();
      expect(structuredChatKeyboardLifecycleGateOpen(mounted)).toBe(false);
      expect(translation(mounted)).toBe(0);

      const unfocusedBind = reduceStructuredChatKeyboardLifecycleGate(
        mounted,
        FOCUS_BIND_OPEN,
      );
      expect(unfocusedBind).toBe(mounted);

      let focused = focusGate(mounted);
      focused = reduceStructuredChatKeyboardLifecycleGate(
        focused,
        FOCUS_BIND_CLOSED,
      );
      expect(structuredChatKeyboardLifecycleGateOpen(focused)).toBe(false);

      focused = reduceStructuredChatKeyboardLifecycleGate(
        focused,
        FOCUS_BIND_OPEN,
      );
      expect(structuredChatKeyboardGeometryIsOpen(KEYBOARD_HEIGHT, 1)).toBe(
        true,
      );
      expect(structuredChatKeyboardLifecycleGateOpen(focused)).toBe(true);
      expect(translation(focused)).toBe(-KEYBOARD_HEIGHT);
      expect(
        structuredChatScrollClearance(COMPOSER_HEIGHT, translation(focused)),
      ).toBe(COMPOSER_HEIGHT + KEYBOARD_HEIGHT);
      expect(
        lifecycleClearance(platform, focused, {
          requestedClearance: COMPOSER_HEIGHT,
          previousClearance: COMPOSER_HEIGHT,
        }),
      ).toBe(COMPOSER_HEIGHT);
    },
  );

  test.each(["android", "ios"] as const)(
    "%s open and focused -> background -> foreground without an IME event stays closed",
    (platform) => {
      let gate = activeGate();
      const oldEpoch = gate.epoch;
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: false,
      });
      expect(gate.epoch).toBeGreaterThan(oldEpoch);
      expect(gate.composerFocused).toBe(false);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: true,
      });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      expect(translation(gate)).toBe(0);
      expect(
        structuredChatScrollClearance(COMPOSER_HEIGHT, translation(gate)),
      ).toBe(COMPOSER_HEIGHT);
      expect(
        lifecycleClearance(platform, gate, {
          requestedClearance: COMPOSER_HEIGHT,
          previousClearance: COMPOSER_HEIGHT + KEYBOARD_HEIGHT,
        }),
      ).toBe(COMPOSER_HEIGHT);
    },
  );

  test.each(["android", "ios"] as const)(
    "%s rejects stale close/open callbacks captured by the old app epoch",
    (_platform) => {
      let gate = activeGate();
      const staleCallbackEpoch = gate.epoch;
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: false,
      });
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: true,
      });
      gate = focusGate(gate);

      const beforeStaleClose = gate;
      gate = nativeSample(gate, {
        sourceEpoch: staleCallbackEpoch,
        height: 0,
        progress: 0,
      });
      expect(gate).toBe(beforeStaleClose);

      const beforeStaleOpen = gate;
      gate = nativeSample(gate, { sourceEpoch: staleCallbackEpoch });
      expect(gate).toBe(beforeStaleOpen);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      expect(translation(gate)).toBe(0);
    },
  );

  test.each(["android", "ios"] as const)(
    "%s foreground with genuinely restored focus/IME accepts a new-epoch visible sample",
    (_platform) => {
      let gate = activeGate();
      const staleCallbackEpoch = gate.epoch;
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: false,
      });
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: true,
      });

      // A current-epoch sample may record geometry before JS focus, but it
      // cannot translate the overlay until Composer focus in this epoch.
      gate = nativeSample(gate, { sourceEpoch: staleCallbackEpoch });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      expect(translation(gate)).toBe(0);

      gate = nativeSample(gate, { sourceEpoch: gate.epoch });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      expect(translation(gate)).toBe(0);

      gate = focusGate(gate);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
      expect(translation(gate)).toBe(-KEYBOARD_HEIGHT);
    },
  );

  test("Android IME open samples in the current epoch still translate when they race ahead of Composer focus", () => {
    let gate = mountedGate();
    const openEpoch = gate.epoch;

    // KeyboardController onStart publishes destination geometry, but Android
    // only commits live translation on move/end. Those native events commonly
    // finish before React TextInput onFocus reaches the overlay gate.
    gate = nativeSample(gate, {
      sourceEpoch: openEpoch,
      updatesGeometry: false,
    });
    gate = nativeSample(gate, {
      sourceEpoch: openEpoch,
      height: 160,
      progress: 0.5,
      updatesGeometry: true,
    });
    gate = nativeSample(gate, {
      sourceEpoch: openEpoch,
      height: KEYBOARD_HEIGHT,
      progress: 1,
      updatesGeometry: true,
    });
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
    expect(translation(gate)).toBe(0);

    gate = focusGate(gate);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
    expect(translation(gate)).toBe(-KEYBOARD_HEIGHT);
    expect(
      structuredChatScrollClearance(COMPOSER_HEIGHT, translation(gate)),
    ).toBe(COMPOSER_HEIGHT + KEYBOARD_HEIGHT);
  });

  test("Android IME open samples after resume invalidation still translate when they race ahead of Composer focus", () => {
    let gate = activeGate();
    gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
      type: "app_state",
      active: false,
    });
    gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
      type: "app_state",
      active: true,
    });
    const openEpoch = gate.epoch;
    expect(gate.mayBindPreexistingGeometry).toBe(false);

    gate = nativeSample(gate, {
      sourceEpoch: openEpoch,
      updatesGeometry: false,
    });
    gate = nativeSample(gate, {
      sourceEpoch: openEpoch,
      height: KEYBOARD_HEIGHT,
      progress: 1,
      updatesGeometry: true,
    });
    gate = reduceStructuredChatKeyboardLifecycleGate(gate, FOCUS_BIND_OPEN);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
    expect(translation(gate)).toBe(0);

    gate = focusGate(gate);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
    expect(translation(gate)).toBe(-KEYBOARD_HEIGHT);
  });

  test.each(["android", "ios"] as const)(
    "%s route disable/enable cannot bind residual geometry",
    (_platform) => {
      let gate = activeGate();
      const staleCallbackEpoch = gate.epoch;
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "set_enabled",
        enabled: false,
      });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "set_enabled",
        enabled: true,
      });
      gate = focusGate(gate);
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, FOCUS_BIND_OPEN);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

      gate = nativeSample(gate, { sourceEpoch: staleCallbackEpoch });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      gate = nativeSample(gate);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
    },
  );

  test.each(["android", "ios"] as const)(
    "%s Composer and timeline consume the same invalidated geometry",
    (platform) => {
      let gate = activeGate();
      expect(translation(gate)).toBe(-KEYBOARD_HEIGHT);
      expect(
        structuredChatScrollClearance(COMPOSER_HEIGHT, translation(gate)),
      ).toBe(COMPOSER_HEIGHT + KEYBOARD_HEIGHT);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: false,
      });
      const composerTranslation = translation(gate);
      const requestedTimelineClearance = structuredChatScrollClearance(
        COMPOSER_HEIGHT,
        composerTranslation,
      );
      expect(composerTranslation).toBe(0);
      expect(requestedTimelineClearance).toBe(COMPOSER_HEIGHT);
      expect(
        lifecycleClearance(platform, gate, {
          requestedClearance: requestedTimelineClearance,
          previousClearance: COMPOSER_HEIGHT + KEYBOARD_HEIGHT,
        }),
      ).toBe(COMPOSER_HEIGHT);
    },
  );

  test.each([
    { height: 0, progress: 0 },
    { height: KEYBOARD_HEIGHT, progress: 0 },
    { height: 0, progress: 1 },
  ])(
    "closed native sample $height/$progress cannot open the focused current epoch",
    ({ height, progress }) => {
      const focused = focusGate(mountedGate());
      const sampled = nativeSample(focused, { height, progress });
      expect(structuredChatKeyboardGeometryIsOpen(height, progress)).toBe(
        false,
      );
      expect(structuredChatKeyboardLifecycleGateOpen(sampled)).toBe(false);
      expect(translation(sampled)).toBe(0);
    },
  );

  test("focus loss starts a new epoch and ordinary show/hide geometry remains coherent", () => {
    let gate = activeGate();
    const focusedEpoch = gate.epoch;
    expect(translation(gate)).toBe(-KEYBOARD_HEIGHT);

    gate = nativeSample(gate, { height: 160, progress: 0.5 });
    expect(translation(gate)).toBe(-160);
    gate = nativeSample(gate, { height: 0, progress: 0 });
    expect(translation(gate)).toBe(0);

    gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
      type: "composer_focus",
      focused: false,
    });
    expect(gate.epoch).toBe(focusedEpoch + 1);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

    gate = focusGate(gate);
    gate = nativeSample(gate, { sourceEpoch: focusedEpoch });
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
    gate = nativeSample(gate);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
  });

  test("Android move/end and iOS start cadence update the same accepted geometry owner", () => {
    let androidGate = focusGate(mountedGate());
    androidGate = nativeSample(androidGate, { updatesGeometry: false });
    expect(structuredChatKeyboardLifecycleGateOpen(androidGate)).toBe(true);
    expect(translation(androidGate)).toBe(0);
    androidGate = nativeSample(androidGate, {
      height: 160,
      progress: 0.5,
      updatesGeometry: true,
    });
    expect(translation(androidGate)).toBe(-160);
    androidGate = nativeSample(androidGate, {
      height: KEYBOARD_HEIGHT,
      progress: 1,
      updatesGeometry: true,
    });
    expect(translation(androidGate)).toBe(-KEYBOARD_HEIGHT);
    androidGate = nativeSample(androidGate, {
      height: 0,
      progress: 0,
      updatesGeometry: false,
    });
    expect(translation(androidGate)).toBe(-KEYBOARD_HEIGHT);
    androidGate = nativeSample(androidGate, {
      height: 0,
      progress: 0,
      updatesGeometry: true,
    });
    expect(translation(androidGate)).toBe(0);

    let iosGate = focusGate(mountedGate());
    iosGate = nativeSample(iosGate, { updatesGeometry: true });
    expect(structuredChatKeyboardLifecycleGateOpen(iosGate)).toBe(true);
    expect(translation(iosGate)).toBe(-KEYBOARD_HEIGHT);
    iosGate = nativeSample(iosGate, {
      height: 0,
      progress: 0,
      updatesGeometry: true,
    });
    expect(translation(iosGate)).toBe(0);
  });

  test("seeded lifecycle sequences never open without route, app, focus, and callback-epoch ownership", () => {
    let seed = 0x5eeda11;
    let gate = mountedGate();
    let callbackEpoch = gate.epoch;

    for (let index = 0; index < 1_000; index += 1) {
      seed = (seed * 1_664_525 + 1_013_904_223) >>> 0;
      switch (seed % 7) {
        case 0:
          gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
            type: "app_state",
            active: false,
          });
          break;
        case 1:
          gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
            type: "app_state",
            active: true,
          });
          break;
        case 2:
          gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
            type: "set_enabled",
            enabled: !gate.enabled,
          });
          break;
        case 3:
          gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
            type: "composer_focus",
            focused: !gate.composerFocused,
          });
          break;
        case 4:
          // Model the handler re-registration eventually catching up.
          callbackEpoch = gate.epoch;
          break;
        case 5:
          gate = nativeSample(gate, { sourceEpoch: callbackEpoch });
          break;
        case 6:
          gate = nativeSample(gate, {
            sourceEpoch: callbackEpoch,
            height: 0,
            progress: 0,
          });
          break;
      }

      if (structuredChatKeyboardLifecycleGateOpen(gate)) {
        expect(gate.enabled).toBe(true);
        expect(gate.appActive).toBe(true);
        expect(gate.composerFocused).toBe(true);
        expect(gate.acceptedNativeSampleEpoch).toBe(gate.epoch);
      } else {
        expect(translation(gate)).toBe(0);
      }
    }
  });

  test("invalidate helper is declared before reduce so worklets capture a defined factory", () => {
    const overlay = source("chatKeyboardOverlayPolicy.ts");
    expect(
      overlay.indexOf("function invalidateStructuredChatKeyboardLifecycleGate"),
    ).toBeGreaterThan(-1);
    expect(
      overlay.indexOf("function invalidateStructuredChatKeyboardLifecycleGate"),
    ).toBeLessThan(
      overlay.indexOf(
        "export function reduceStructuredChatKeyboardLifecycleGate",
      ),
    );
  });

  test("dispatch applies a focus event to the live gate and preserves accepted Android geometry", () => {
    // Emulator-observed Android cadence for a normal IME open: Start publishes
    // destination geometry, then a zero-height Move, the full-height Move, and
    // the End sample. All of it is applied to the UI-owned gate before the JS
    // focus event lands.
    const sv = gateHolder(focusGate(mountedGate()));
    dispatchNativeSample(sv, { updatesGeometry: false });
    dispatchNativeSample(sv, { height: 0, progress: 0 });
    dispatchNativeSample(sv);
    dispatchNativeSample(sv);
    expect(translation(sv.value)).toBe(-KEYBOARD_HEIGHT);

    // The JS-thread focus event must reduce ON TOP of the live gate. A stale
    // JS-side snapshot write-back would drop acceptedNativeSampleEpoch and the
    // keyboard translation, pinning the Composer to the screen bottom while
    // the IME is open (the reported regression).
    const result = dispatch(sv, { type: "composer_focus", focused: true });
    expect(result.epochChanged).toBe(false);
    expect(sv.value.composerFocused).toBe(true);
    expect(sv.value.acceptedNativeSampleEpoch).toBe(sv.value.epoch);
    expect(structuredChatKeyboardLifecycleGateOpen(sv.value)).toBe(true);
    expect(translation(sv.value)).toBe(-KEYBOARD_HEIGHT);
    expect(
      structuredChatScrollClearance(COMPOSER_HEIGHT, translation(sv.value)),
    ).toBe(COMPOSER_HEIGHT + KEYBOARD_HEIGHT);
  });

  test("dispatch reports epoch transitions and their route/app invalidation reasons", () => {
    const sv = gateHolder(activeGate());
    const background = dispatch(sv, { type: "app_state", active: false });
    expect(background.epochChanged).toBe(true);
    expect(background.invalidateReason).toBe("app");
    expect(background.epoch).toBe(sv.value.epoch);
    expect(structuredChatKeyboardLifecycleGateOpen(sv.value)).toBe(false);

    const refocus = dispatch(sv, { type: "composer_focus", focused: true });
    expect(refocus.epochChanged).toBe(false);
    expect(refocus.invalidateReason).toBeNull();
    expect(refocus.epoch).toBe(sv.value.epoch);

    const route = dispatch(sv, { type: "set_enabled", enabled: false });
    expect(route.epochChanged).toBe(true);
    expect(route.invalidateReason).toBe("route");
    expect(route.epoch).toBe(sv.value.epoch);

    const refocusAfterRoute = dispatch(sv, {
      type: "composer_focus",
      focused: true,
    });
    expect(refocusAfterRoute.epochChanged).toBe(false);
    expect(refocusAfterRoute.invalidateReason).toBeNull();
  });

  test("a stale JS-side gate snapshot must never overwrite UI-accepted geometry", () => {
    // UI runtime: the open samples were accepted on the gate it owns.
    const ui = gateHolder(activeGate());
    expect(translation(ui.value)).toBe(-KEYBOARD_HEIGHT);

    // The JS runtime's copy of the same shared value is stale (sync lag): it
    // never saw the native samples. The pre-fix focus effect reduced on this
    // stale copy and wrote the result back to the shared value, dropping the
    // accepted epoch and the keyboard translation. That read-modify-write must
    // never run on the JS thread; the dispatch API applies the focus event to
    // the live gate instead.
    const staleJsSnapshot = createStructuredChatKeyboardLifecycleGate({
      enabled: true,
      appActive: true,
    });
    const jsWriteBack = reduceStructuredChatKeyboardLifecycleGate(
      staleJsSnapshot,
      { type: "composer_focus", focused: true },
    );
    expect(structuredChatKeyboardLifecycleGateOpen(jsWriteBack)).toBe(false);
    expect(translation(jsWriteBack)).toBe(0);

    dispatch(ui, { type: "composer_focus", focused: true });
    expect(structuredChatKeyboardLifecycleGateOpen(ui.value)).toBe(true);
    expect(translation(ui.value)).toBe(-KEYBOARD_HEIGHT);
  });
  test("frame dispatches all gate events through the UI runtime and never read-modify-writes the gate from JS", () => {
    const frame = source("InterfaceChatKeyboardFrame.tsx");
    // The gate shared value is owned by the UI runtime: JS effects schedule
    // events there and only observe the resulting epoch via the dispatch
    // result. The pre-fix wiring read the stale JS copy, reduced on it and
    // wrote it back, clobbering UI-accepted geometry on every focus.
    expect(frame).toContain("dispatchStructuredChatKeyboardLifecycleEvent");
    expect(frame).toContain("runOnUIAsync");
    expect(frame).not.toMatch(/keyboardLifecycleGate\.value = next;/);
    expect(frame).not.toMatch(/const previous = keyboardLifecycleGate\.value;/);
    expect(frame).toContain("runOnUI(dispatchComposerFocusBind)");
    // The overlay transform reads the gate directly: an invalidation that
    // lands while backgrounded can lose its style apply, and a deduplicated
    // numeric write (0 === 0) would never re-apply it after the suspension.
    expect(frame).toContain(
      "translateY: structuredChatGatedOverlayTranslateY({",
    );
    expect(frame).not.toMatch(
      /transform: \[\{ translateY: overlayTranslateY\.value/,
    );
  });

  test("frame captures callback epochs and never refocuses or remounts to recover", () => {
    const frame = source("InterfaceChatKeyboardFrame.tsx");
    const body = source("InterfaceChatBody.tsx");
    const surfaceState = source("useInterfaceChatSurfaceState.ts");
    const lifecycleInvalidationStart = surfaceState.indexOf(
      "const handleKeyboardLifecycleInvalidate",
    );
    const lifecycleInvalidation = surfaceState.slice(
      lifecycleInvalidationStart,
      surfaceState.indexOf("useEffect", lifecycleInvalidationStart),
    );

    expect(body).toContain("composerFocused={composerFocused}");
    expect(frame).toContain("nativeCallbackEpoch");
    expect(frame).toContain("sourceEpoch");
    expect(frame).toContain('type: "composer_focus"');
    expect(frame).toContain('type: "composer_focus_bind"');
    expect(frame).toContain("useGenericKeyboardHandler(");
    // The focus-geometry bind reads the live native values on the UI runtime;
    // a JS-side read would see the stale copy and could bind wrong geometry.
    expect(frame).toContain("runOnUI(dispatchComposerFocusBind)");
    expect(frame).toContain("reanimated.height,");
    expect(frame).toContain("reanimated.progress,");
    expect(frame).not.toContain("reanimated.height.value");
    expect(frame).not.toContain("reanimated.progress.value");
    expect(frame).not.toContain("setTimeout");
    expect(frame).not.toContain("requestAnimationFrame");
    expect(frame).not.toMatch(/autoFocus|remountKey|key=\{.*focus/);
    expect(lifecycleInvalidation).toContain("composerInput.blur()");
    expect(lifecycleInvalidation).not.toMatch(/setDraft|restoreDraft/);
    expect(lifecycleInvalidation).not.toContain("setAttachments");
  });
});

function lifecycleClearance(
  platform: StructuredChatInsetPlatform,
  gate: StructuredChatKeyboardLifecycleGate,
  geometry: {
    requestedClearance: number;
    previousClearance: number;
  },
) {
  return structuredChatEffectiveClearanceForKeyboardLifecycle({
    platform,
    gate,
    requestedClearance: geometry.requestedClearance,
    rawOffset: platform === "ios" ? -geometry.previousClearance : 0,
    previousClearance: geometry.previousClearance,
  });
}
