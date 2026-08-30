import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  createStructuredChatKeyboardLifecycleGate,
  dispatchStructuredChatAuthoritativeSnapshot,
  dispatchStructuredChatKeyboardLifecycleEvent,
  reduceStructuredChatKeyboardLifecycleGate,
  structuredChatContentFadeGeometry,
  structuredChatEffectiveClearanceForKeyboardLifecycle,
  structuredChatGatedOverlayTranslateY,
  structuredChatKeyboardLifecycleGateOpen,
  structuredChatScrollClearance,
  type StructuredChatInsetPlatform,
  type StructuredChatKeyboardLifecycleGate,
} from "./chatKeyboardOverlayPolicy";

const KEYBOARD_HEIGHT = 320;
const COMPOSER_HEIGHT = 76;

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

function mountedGate() {
  return createStructuredChatKeyboardLifecycleGate({
    enabled: true,
    appActive: true,
  });
}

function focus(gate: StructuredChatKeyboardLifecycleGate) {
  return reduceStructuredChatKeyboardLifecycleGate(gate, {
    type: "composer_focus",
    focused: true,
  });
}

function blur(gate: StructuredChatKeyboardLifecycleGate) {
  return reduceStructuredChatKeyboardLifecycleGate(gate, {
    type: "composer_focus",
    focused: false,
  });
}

function snapshot(
  gate: StructuredChatKeyboardLifecycleGate,
  input: {
    sourceRevision?: number;
    imeVisible?: boolean;
    imeHeight?: number;
    composerFocused?: boolean;
    foregroundReconciliation?: boolean;
  } = {},
) {
  return reduceStructuredChatKeyboardLifecycleGate(gate, {
    type: "authoritative_snapshot",
    sourceRevision: input.sourceRevision ?? gate.revision,
    imeVisible: input.imeVisible ?? true,
    imeHeight: input.imeHeight ?? KEYBOARD_HEIGHT,
    composerFocused: input.composerFocused ?? true,
    foregroundReconciliation: input.foregroundReconciliation ?? false,
  });
}

function sample(
  gate: StructuredChatKeyboardLifecycleGate,
  input: {
    sourceRevision?: number;
    height?: number;
    progress?: number;
    updatesGeometry?: boolean;
    settled?: boolean;
  } = {},
) {
  return reduceStructuredChatKeyboardLifecycleGate(gate, {
    type: "native_sample",
    sourceRevision: input.sourceRevision ?? gate.revision,
    height: input.height ?? KEYBOARD_HEIGHT,
    progress: input.progress ?? 1,
    updatesGeometry: input.updatesGeometry ?? true,
    settled: input.settled ?? false,
  });
}

function activeGate() {
  return snapshot(focus(mountedGate()));
}

function translation(gate: StructuredChatKeyboardLifecycleGate) {
  return structuredChatGatedOverlayTranslateY({
    gate,
    keyboardVerticalOffset: 0,
  });
}

function lifecycleClearance(
  platform: StructuredChatInsetPlatform,
  gate: StructuredChatKeyboardLifecycleGate,
  requestedClearance: number,
  previousClearance: number,
) {
  return structuredChatEffectiveClearanceForKeyboardLifecycle({
    platform,
    gate,
    requestedClearance,
    rawOffset: platform === "ios" ? -previousClearance : 0,
    previousClearance,
  });
}

function backgroundThenActive(gate: StructuredChatKeyboardLifecycleGate) {
  const background = reduceStructuredChatKeyboardLifecycleGate(gate, {
    type: "app_state",
    active: false,
  });
  return reduceStructuredChatKeyboardLifecycleGate(background, {
    type: "app_state",
    active: true,
  });
}

describe("authoritative structured-chat keyboard lifecycle", () => {
  test("background -> active -> current-handler stale open -> authoritative hidden remains closed", () => {
    let gate = backgroundThenActive(activeGate());
    const revision = gate.revision;

    gate = sample(gate, { sourceRevision: revision });
    expect(gate.authoritativeRevision).toBe(0);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

    gate = snapshot(gate, {
      sourceRevision: revision,
      imeVisible: false,
      imeHeight: 0,
      composerFocused: true,
      foregroundReconciliation: true,
    });
    expect(gate.composerFocused).toBe(false);
    expect(gate.authoritativeRevision).toBe(revision);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
    expect(translation(gate)).toBe(0);
  });

  test("background -> active -> focus -> stale open -> authoritative hidden remains closed", () => {
    let gate = focus(backgroundThenActive(activeGate()));
    gate = sample(gate);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

    gate = snapshot(gate, {
      imeVisible: false,
      imeHeight: 0,
      composerFocused: true,
      foregroundReconciliation: true,
    });
    expect(gate.composerFocused).toBe(false);
    expect(gate.keyboardTranslation).toBe(0);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
  });

  test("authoritative visible current-target snapshot before React focus opens when focus confirms", () => {
    let gate = snapshot(mountedGate());
    expect(gate.nativeImeVisible).toBe(true);
    expect(gate.nativeComposerFocused).toBe(true);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

    gate = focus(gate);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
    expect(translation(gate)).toBe(-KEYBOARD_HEIGHT);
  });

  test("React focus before authoritative visible current-target snapshot opens on snapshot", () => {
    let gate = focus(mountedGate());
    gate = sample(gate);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

    gate = snapshot(gate);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
    expect(translation(gate)).toBe(-KEYBOARD_HEIGHT);
  });

  test("old native provenance and a lagging callback revision cannot reopen a new lifecycle", () => {
    const open = activeGate();
    const oldRevision = open.revision;
    let gate = focus(backgroundThenActive(open));

    gate = snapshot(gate, { sourceRevision: oldRevision });
    gate = sample(gate, { sourceRevision: oldRevision });
    expect(gate.authoritativeRevision).toBe(0);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

    gate = snapshot(gate, {
      imeVisible: false,
      imeHeight: 0,
      foregroundReconciliation: true,
    });
    gate = sample(gate, { sourceRevision: oldRevision });
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
    expect(translation(gate)).toBe(0);
  });

  test.each(["android", "ios"] as const)(
    "%s foreground hidden transaction publishes only Composer geometry",
    (platform) => {
      let gate = backgroundThenActive(activeGate());
      gate = snapshot(gate, {
        imeVisible: false,
        imeHeight: 0,
        composerFocused: true,
        foregroundReconciliation: true,
      });
      const composerTranslation = translation(gate);
      const requestedClearance = structuredChatScrollClearance(
        COMPOSER_HEIGHT,
        composerTranslation,
      );
      const fade = structuredChatContentFadeGeometry(
        COMPOSER_HEIGHT,
        composerTranslation,
      );

      expect(composerTranslation).toBe(0);
      expect(fade.opaqueBottomInset).toBe(COMPOSER_HEIGHT * 0.5);
      expect(fade.transparentBottomInset).toBeCloseTo(COMPOSER_HEIGHT * 0.1);
      expect(requestedClearance).toBe(COMPOSER_HEIGHT);
      expect(
        lifecycleClearance(
          platform,
          gate,
          requestedClearance,
          COMPOSER_HEIGHT + KEYBOARD_HEIGHT,
        ),
      ).toBe(COMPOSER_HEIGHT);
    },
  );

  test.each(["android", "ios"] as const)(
    "%s lifecycle close contracts immediately while ordinary contraction retains occupied range",
    (platform) => {
      let ordinary = activeGate();
      ordinary = sample(ordinary, { height: 0, progress: 0 });
      const requested = structuredChatScrollClearance(
        COMPOSER_HEIGHT,
        translation(ordinary),
      );
      expect(requested).toBe(COMPOSER_HEIGHT);
      expect(
        lifecycleClearance(
          platform,
          ordinary,
          requested,
          COMPOSER_HEIGHT + KEYBOARD_HEIGHT,
        ),
      ).toBe(COMPOSER_HEIGHT + KEYBOARD_HEIGHT);

      ordinary = snapshot(ordinary, {
        imeVisible: false,
        imeHeight: 0,
        composerFocused: true,
      });
      expect(structuredChatKeyboardLifecycleGateOpen(ordinary)).toBe(false);
      expect(
        lifecycleClearance(
          platform,
          ordinary,
          COMPOSER_HEIGHT,
          COMPOSER_HEIGHT + KEYBOARD_HEIGHT,
        ),
      ).toBe(COMPOSER_HEIGHT + KEYBOARD_HEIGHT);

      const lifecycleClosed = reduceStructuredChatKeyboardLifecycleGate(
        activeGate(),
        { type: "app_state", active: false },
      );
      expect(
        lifecycleClearance(
          platform,
          lifecycleClosed,
          COMPOSER_HEIGHT,
          COMPOSER_HEIGHT + KEYBOARD_HEIGHT,
        ),
      ).toBe(COMPOSER_HEIGHT);
    },
  );

  test("focus and height alone never prove OPEN", () => {
    let gate = focus(mountedGate());
    gate = sample(gate);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

    gate = snapshot(gate, { composerFocused: false });
    expect(gate.nativeImeVisible).toBe(true);
    expect(gate.nativeComposerFocused).toBe(false);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
  });

  test("animation geometry updates only after current native authority admission", () => {
    let gate = focus(mountedGate());
    gate = sample(gate, { height: 160, progress: 0.5 });
    expect(gate.keyboardTranslation).toBe(0);

    gate = snapshot(gate);
    gate = sample(gate, { height: 160, progress: 0.5 });
    expect(gate.keyboardTranslation).toBe(-160);
    expect(gate.keyboardProgress).toBe(0.5);
  });

  test("settled hide sample clears a stale non-zero keyboard height", () => {
    const open = activeGate();
    const gate = reduceStructuredChatKeyboardLifecycleGate(open, {
      type: "native_sample",
      sourceRevision: open.revision,
      height: KEYBOARD_HEIGHT,
      progress: 0,
      updatesGeometry: true,
      settled: true,
    });

    expect(gate.nativeImeVisible).toBe(false);
    expect(gate.nativeComposerFocused).toBe(false);
    expect(gate.keyboardTranslation).toBe(0);
    expect(gate.keyboardProgress).toBe(0);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
  });

  test("an early authoritative hidden focus snapshot remains eligible for event-driven visible reconciliation", () => {
    const frame = source("InterfaceChatKeyboardFrame.tsx");

    expect(
      frame.match(/!keyboardLifecycleGate\.value\.nativeImeVisible/g)?.length,
    ).toBe(2);
    expect(
      frame.match(/runOnJS\(requestAuthoritativeSnapshot\)/g)?.length,
    ).toBe(3);
  });

  test("ordinary authoritative hide requests one native blur without marking lifecycle contraction", () => {
    const holder = { value: activeGate() };
    const result = dispatchStructuredChatAuthoritativeSnapshot(holder, {
      type: "authoritative_snapshot",
      sourceRevision: holder.value.revision,
      imeVisible: false,
      imeHeight: 0,
      composerFocused: true,
      foregroundReconciliation: false,
    });

    expect(result.accepted).toBe(true);
    expect(result.shouldBlurComposer).toBe(true);
    expect(holder.value.forceLifecycleContractionRevision).toBe(0);
    const revision = holder.value.revision;
    holder.value = blur(holder.value);
    expect(holder.value.revision).toBe(revision);
  });

  test("dispatch reports lifecycle revisions", () => {
    const holder = { value: activeGate() };
    const background = dispatchStructuredChatKeyboardLifecycleEvent(holder, {
      type: "app_state",
      active: false,
    });
    expect(background.revisionChanged).toBe(true);
    expect(background.invalidateReason).toBe("app");
    expect(background.revision).toBe(holder.value.revision);

    const active = dispatchStructuredChatKeyboardLifecycleEvent(holder, {
      type: "app_state",
      active: true,
    });
    expect(active.revisionChanged).toBe(true);
    expect(active.invalidateReason).toBeNull();
  });
});

describe("shared revision consumer wiring", () => {
  test("Composer, fade, requested clearance, effective inset, clipping, and logical offset read one gate", () => {
    const frame = source("InterfaceChatKeyboardFrame.tsx");
    const fade = source("StructuredChatContentFade.tsx");
    const inset = source("StructuredChatInsetScrollView.tsx");

    expect(frame).toContain("gate: keyboardLifecycleGate.value");
    expect(frame).toContain("keyboardLifecycleGate={keyboardLifecycleGate}");
    expect(fade).toContain("gate: keyboardLifecycleGate.value");
    expect(inset).toContain("gate: keyboardLifecycleGate.value");
    expect(inset).toContain("keyboardLifecycleGate.value.revision;");
    expect(inset).toContain("animatedProps={animatedProps}");
    expect(inset).toContain("<ReanimatedClippingScrollView");
  });

  test("a numerically unchanged CLOSED revision still remaps every native consumer", () => {
    const frame = source("InterfaceChatKeyboardFrame.tsx");
    const fade = source("StructuredChatContentFade.tsx");
    const inset = source("StructuredChatInsetScrollView.tsx");

    expect(frame).toContain(
      "translateY: structuredChatGatedOverlayTranslateY({",
    );
    expect(frame).not.toContain("translateY: overlayTranslateY.value");
    expect(fade.match(/gate: keyboardLifecycleGate\.value/g)?.length).toBe(5);
    expect(
      inset.match(/keyboardLifecycleGate\.value/g)?.length,
    ).toBeGreaterThanOrEqual(3);
  });

  test("foreground transaction reasserts adjustNothing and never refocuses", () => {
    const frame = source("InterfaceChatKeyboardFrame.tsx");
    const windowMode = source("chatKeyboardWindowMode.ts");

    expect(frame).toContain("reapplyStructuredChatWindowModeLease()");
    expect(windowMode).toContain("activeStructuredChatLeases > 0");
    expect(frame).not.toMatch(
      /\.focus\(|autoFocus|setTimeout|requestAnimationFrame/,
    );
  });

  test("the superseded cached Keyboard visibility focus owner is absent", () => {
    const hooks = source("InterfaceChatSurfaceHooks.ts");

    expect(hooks).not.toContain("Keyboard.isVisible()");
    expect(hooks).not.toContain("COMPOSER_STALE_HIDE_GRACE_MS");
    expect(hooks).not.toContain("keyboardDidShow");
    expect(hooks).not.toContain("keyboardDidHide");
  });

  test("Brain and Session both reach the same InterfaceChatKeyboardFrame owner", () => {
    const brain = source("../../app/(primary)/index.tsx");
    const session = source("TerminalViewport.tsx");
    const surface = source("InterfaceChatSurface.tsx");
    const body = source("InterfaceChatBody.tsx");

    expect(brain).toContain("<InterfaceChatSurface");
    expect(session).toContain("<InterfaceChatSurface");
    expect(surface).toContain("<InterfaceChatBody");
    expect(body).toContain("<InterfaceChatKeyboardFrame");
  });
});
