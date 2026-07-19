import { describe, expect, test } from "bun:test";
import {
  createStructuredChatKeyboardLifecycleGate,
  reduceStructuredChatKeyboardLifecycleGate,
  structuredChatEffectiveClearanceForKeyboardLifecycle,
  structuredChatGatedOverlayTranslateY,
  structuredChatKeyboardLifecycleGateOpen,
  structuredChatScrollClearance,
  type StructuredChatInsetPlatform,
} from "./chatKeyboardOverlayPolicy";

const NATIVE_SHOW = {
  type: "native_sample" as const,
  height: 320,
  progress: 1,
};

function activeGate() {
  return reduceStructuredChatKeyboardLifecycleGate(
    createStructuredChatKeyboardLifecycleGate({
      enabled: true,
      appActive: true,
    }),
    NATIVE_SHOW,
  );
}

function translation(
  gate: ReturnType<typeof createStructuredChatKeyboardLifecycleGate>,
) {
  return structuredChatGatedOverlayTranslateY({
    gate,
    keyboardTranslation: -320,
    keyboardProgress: 1,
    keyboardVerticalOffset: 0,
  });
}

describe("structured chat keyboard lifecycle gate", () => {
  test.each(["android", "ios"] as const)(
    "%s neutralizes open geometry through inactive and resumes only from a new native sample",
    (platform) => {
      let gate = activeGate();
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
      expect(translation(gate)).toBe(-320);
      expect(structuredChatScrollClearance(76, translation(gate))).toBe(396);

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
      expect(translation(gate)).toBe(0);

      gate = reduceStructuredChatKeyboardLifecycleGate(gate, NATIVE_SHOW);
      expect(structuredChatKeyboardLifecycleGateOpen(gate)).toBe(true);
      expect(translation(gate)).toBe(-320);
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

    const remounted = createStructuredChatKeyboardLifecycleGate({
      enabled: true,
      appActive: true,
    });
    expect(structuredChatKeyboardLifecycleGateOpen(remounted)).toBe(false);
    expect(translation(remounted)).toBe(0);
  });
});

function lifecycleClearance(
  platform: StructuredChatInsetPlatform,
  gate: ReturnType<typeof createStructuredChatKeyboardLifecycleGate>,
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
