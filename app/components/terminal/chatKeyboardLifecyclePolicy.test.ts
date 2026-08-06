import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  createStructuredChatKeyboardLifecycleGate,
  reduceStructuredChatKeyboardLifecycleGate,
  structuredChatEffectiveClearanceForKeyboardLifecycle,
  structuredChatGatedOverlayTranslateY,
  structuredChatKeyboardGeometryIsOpen,
  structuredChatKeyboardLifecycleGateOpen,
  structuredChatScrollClearance,
  type StructuredChatInsetPlatform,
  type StructuredChatKeyboardLifecycleGate,
} from "./chatKeyboardOverlayPolicy";

const NATIVE_SHOW = {
  type: "native_sample" as const,
  height: 320,
  progress: 1,
};

const FOCUS_BIND_OPEN = {
  type: "composer_focus_bind" as const,
  height: 320,
  progress: 1,
};

const FOCUS_BIND_CLOSED = {
  type: "composer_focus_bind" as const,
  height: 0,
  progress: 0,
};

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

function activeGate() {
  return reduceStructuredChatKeyboardLifecycleGate(
    createStructuredChatKeyboardLifecycleGate({
      enabled: true,
      appActive: true,
    }),
    NATIVE_SHOW,
  );
}

function remountedGate() {
  return createStructuredChatKeyboardLifecycleGate({
    enabled: true,
    appActive: true,
  });
}

function translation(gate: StructuredChatKeyboardLifecycleGate) {
  return structuredChatGatedOverlayTranslateY({
    gate,
    keyboardTranslation: -320,
    keyboardProgress: 1,
    keyboardVerticalOffset: 0,
  });
}

describe("structured chat keyboard lifecycle gate", () => {
  test.each(["android", "ios"] as const)(
    "%s first mount with pre-existing open geometry binds only under Composer focus",
    (platform) => {
      const remounted = remountedGate();
      expect(structuredChatKeyboardLifecycleGateOpen(remounted)).toBe(false);
      expect(translation(remounted)).toBe(0);
      expect(
        lifecycleClearance(platform, remounted, {
          requestedClearance: 76,
          previousClearance: 76,
        }),
      ).toBe(76);

      const withoutFocus = reduceStructuredChatKeyboardLifecycleGate(
        remounted,
        FOCUS_BIND_CLOSED,
      );
      expect(structuredChatKeyboardLifecycleGateOpen(withoutFocus)).toBe(false);
      expect(translation(withoutFocus)).toBe(0);

      const focusedOpen = reduceStructuredChatKeyboardLifecycleGate(
        remounted,
        FOCUS_BIND_OPEN,
      );
      expect(structuredChatKeyboardGeometryIsOpen(320, 1)).toBe(true);
      expect(structuredChatKeyboardLifecycleGateOpen(focusedOpen)).toBe(true);
      expect(translation(focusedOpen)).toBe(-320);
      expect(structuredChatScrollClearance(76, translation(focusedOpen))).toBe(
        396,
      );
      expect(
        lifecycleClearance(platform, focusedOpen, {
          requestedClearance: 76,
          previousClearance: 76,
        }),
      ).toBe(76);
    },
  );

  test.each(["android", "ios"] as const)(
    "%s unfocused residual open geometry cannot bind a remounted epoch",
    (platform) => {
      let gate = remountedGate();
      // Pre-existing KeyboardController geometry is visible to gated translate
      // inputs, but without a focus bind or live sample the epoch stays closed.
      expect(translation(gate)).toBe(0);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "set_enabled",
        enabled: false,
      });
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, FOCUS_BIND_OPEN);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      expect(translation(gate)).toBe(0);
      expect(
        lifecycleClearance(platform, gate, {
          requestedClearance: 76,
          previousClearance: 396,
        }),
      ).toBe(76);
    },
  );

  test.each(["android", "ios"] as const)(
    "%s route and app invalidation drop ownership until a fresh bind",
    (platform) => {
      let gate = activeGate();
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
      expect(translation(gate)).toBe(-320);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "set_enabled",
        enabled: false,
      });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      expect(translation(gate)).toBe(0);
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, FOCUS_BIND_OPEN);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "set_enabled",
        enabled: true,
      });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, FOCUS_BIND_OPEN);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
      expect(translation(gate)).toBe(-320);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: false,
      });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      expect(translation(gate)).toBe(0);
      expect(
        lifecycleClearance(platform, gate, {
          requestedClearance: 76,
          previousClearance: 396,
        }),
      ).toBe(76);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: true,
      });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      // App resume requires a live sample or a later focus bind; residual
      // geometry alone must not revive the prior epoch.
      expect(translation(gate)).toBe(0);
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, NATIVE_SHOW);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
      expect(translation(gate)).toBe(-320);
    },
  );

  test.each(["android", "ios"] as const)(
    "%s focus transition binds open geometry and ignores closed geometry",
    (_platform) => {
      let gate = remountedGate();
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, FOCUS_BIND_CLOSED);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      expect(translation(gate)).toBe(0);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, FOCUS_BIND_OPEN);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
      expect(translation(gate)).toBe(-320);

      // Live animation samples remain the shared path for subsequent motion.
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: false,
      });
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: true,
      });
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, NATIVE_SHOW);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
      expect(translation(gate)).toBe(-320);
    },
  );

  test.each(["android", "ios"] as const)(
    "%s exit and re-entry remount shares the same focus-bind lifecycle",
    (_platform) => {
      let gate = reduceStructuredChatKeyboardLifecycleGate(
        remountedGate(),
        FOCUS_BIND_OPEN,
      );
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "set_enabled",
        enabled: false,
      });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);

      // Re-entry is a new mount identity with the same ownership rules.
      const reentered = reduceStructuredChatKeyboardLifecycleGate(
        remountedGate(),
        FOCUS_BIND_OPEN,
      );
      expect(structuredChatKeyboardLifecycleGateOpen(reentered)).toBe(true);
      expect(translation(reentered)).toBe(-320);
    },
  );

  test.each([
    { height: 0, progress: 0 },
    { height: 320, progress: 0 },
    { height: 0, progress: 1 },
  ])(
    "closed native sample $height/$progress cannot reopen a resumed epoch",
    ({ height, progress }) => {
      let gate = activeGate();
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: false,
      });
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: true,
      });
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "native_sample",
        height,
        progress,
      });
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "composer_focus_bind",
        height,
        progress,
      });

      expect(structuredChatKeyboardGeometryIsOpen(height, progress)).toBe(false);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      expect(translation(gate)).toBe(0);
    },
  );

  test("repeated lifecycle and disabled epochs cannot revive an older sample", () => {
    let gate = activeGate();
    const firstAcceptedEpoch = gate.acceptedNativeSampleEpoch;

    for (let cycle = 0; cycle < 2; cycle += 1) {
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: false,
      });
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: false,
      });
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
        type: "app_state",
        active: true,
      });
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
      gate = reduceStructuredChatKeyboardLifecycleGate(gate, NATIVE_SHOW);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
    }

    gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
      type: "set_enabled",
      enabled: false,
    });
    gate = reduceStructuredChatKeyboardLifecycleGate(gate, NATIVE_SHOW);
    gate = reduceStructuredChatKeyboardLifecycleGate(gate, FOCUS_BIND_OPEN);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
    gate = reduceStructuredChatKeyboardLifecycleGate(gate, {
      type: "set_enabled",
      enabled: true,
    });
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(false);
    expect(gate.acceptedNativeSampleEpoch).toBeGreaterThanOrEqual(
      firstAcceptedEpoch,
    );

    gate = reduceStructuredChatKeyboardLifecycleGate(gate, NATIVE_SHOW);
    expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);

    const remounted = remountedGate();
    expect(structuredChatKeyboardLifecycleGateOpen(remounted)).toBe(false);
    expect(translation(remounted)).toBe(0);
    const remountedFocused = reduceStructuredChatKeyboardLifecycleGate(
      remounted,
      FOCUS_BIND_OPEN,
    );
    expect(structuredChatKeyboardLifecycleGateOpen(remountedFocused)).toBe(
      true,
    );
    expect(translation(remountedFocused)).toBe(-320);
  });

  test("frame binds current KeyboardController geometry on Composer focus", () => {
    const frame = source("InterfaceChatKeyboardFrame.tsx");
    const body = source("InterfaceChatBody.tsx");

    expect(body).toContain("composerFocused={composerFocused}");
    expect(frame).toContain("composerFocused");
    expect(frame).toContain("composer_focus_bind");
    expect(frame).toContain("reanimated.height.value");
    expect(frame).toContain("reanimated.progress.value");
    expect(frame).toContain("if (!enabled || !composerFocused)");
    expect(frame).not.toContain("setTimeout");
    expect(frame).not.toContain("requestAnimationFrame");
    expect(frame).not.toMatch(/autoFocus|remountKey|key=\{.*focus/);
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
