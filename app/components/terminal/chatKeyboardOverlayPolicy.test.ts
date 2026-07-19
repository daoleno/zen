import { describe, expect, test } from "bun:test";
import {
  structuredChatContentFadeGeometry,
  structuredChatEffectiveClearance,
  structuredChatFocusSample,
  structuredChatLatestOffset,
  structuredChatLogicalOffset,
  structuredChatNativeMaskColors,
  structuredChatOverlayGeometry,
  structuredChatOverlayTranslateY,
  structuredChatScrollClearance,
  resolveInterfaceChatCanvasColor,
} from "./chatKeyboardOverlayPolicy";

describe("structured chat overlay geometry", () => {
  test.each([320, 390, 430])(
    "keeps the %ipx timeline canvas fixed through keyboard open and close",
    (width) => {
      const canvasHeight = width * 2;
      const closed = structuredChatOverlayGeometry({
        canvasHeight,
        composerHeight: 76,
        keyboardHeight: 0,
      });
      const open = structuredChatOverlayGeometry({
        canvasHeight,
        composerHeight: 76,
        keyboardHeight: 280,
      });

      expect(closed).toMatchObject({
        timelineHeight: canvasHeight,
        contentOffsetDelta: 0,
        scrollClearance: 76,
      });
      expect(open).toMatchObject({
        timelineHeight: canvasHeight,
        contentOffsetDelta: 0,
        scrollClearance: 356,
      });
      expect(open.composerTop).toBe(canvasHeight - 356);
    },
  );

  test("keyboard height changes extend range without moving the visible anchor", () => {
    const first = structuredChatOverlayGeometry({
      canvasHeight: 844,
      composerHeight: 88,
      keyboardHeight: 280,
    });
    const changed = structuredChatOverlayGeometry({
      canvasHeight: 844,
      composerHeight: 88,
      keyboardHeight: 336,
    });

    expect(changed.timelineHeight).toBe(first.timelineHeight);
    expect(changed.contentOffsetDelta).toBe(0);
    expect(changed.scrollClearance - first.scrollClearance).toBe(56);
  });

  test("multiline and attachment growth change only overlay clearance", () => {
    const compact = structuredChatOverlayGeometry({
      canvasHeight: 844,
      composerHeight: 76,
      keyboardHeight: 300,
    });
    const multiline = structuredChatOverlayGeometry({
      canvasHeight: 844,
      composerHeight: 124,
      keyboardHeight: 300,
    });
    const attachment = structuredChatOverlayGeometry({
      canvasHeight: 844,
      composerHeight: 188,
      keyboardHeight: 300,
    });

    expect(
      [compact, multiline, attachment].map((state) => ({
        timelineHeight: state.timelineHeight,
        contentOffsetDelta: state.contentOffsetDelta,
      })),
    ).toEqual([
      { timelineHeight: 844, contentOffsetDelta: 0 },
      { timelineHeight: 844, contentOffsetDelta: 0 },
      { timelineHeight: 844, contentOffsetDelta: 0 },
    ]);
    expect([
      compact.scrollClearance,
      multiline.scrollClearance,
      attachment.scrollClearance,
    ]).toEqual([376, 424, 488]);
  });

  test.each([320, 390, 430])(
    "derives the %ipx content fade from the measured Composer footprint",
    (width) => {
      const composerHeight = width === 320 ? 76 : width === 390 ? 124 : 188;
      const fade = structuredChatContentFadeGeometry(composerHeight, 0);

      expect(fade.opaqueBottomInset).toBe(composerHeight * 0.5);
      expect(fade.transparentBottomInset).toBeCloseTo(composerHeight * 0.1);
      expect(fade.fadeHeight).toBeCloseTo(composerHeight * 0.4);
    },
  );

  test("moves the content fade with the same keyboard lift as the Composer", () => {
    const translation = structuredChatOverlayTranslateY(-300, 1, 24);
    const fade = structuredChatContentFadeGeometry(88, translation);

    expect(fade.opaqueBottomInset).toBe(320);
    expect(fade.transparentBottomInset).toBeCloseTo(284.8);
    expect(fade.fadeHeight).toBeCloseTo(35.2);
  });

  test.each([
    {
      name: "dark direct background",
      appBackground: "#0F0F14",
      surface: "#25252D",
      expectedCanvas: "#0F0F14",
      expectedMask: {
        visible: "rgba(15, 15, 20, 1)",
        hidden: "rgba(15, 15, 20, 0)",
      },
    },
    {
      name: "light direct background",
      appBackground: "#F7F8F6",
      surface: "#ECEDEB",
      expectedCanvas: "#F7F8F6",
      expectedMask: {
        visible: "rgba(247, 248, 246, 1)",
        hidden: "rgba(247, 248, 246, 0)",
      },
    },
    {
      name: "transparent background surface fallback",
      appBackground: "transparent",
      surface: "#25252D",
      expectedCanvas: "#25252D",
      expectedMask: {
        visible: "rgba(37, 37, 45, 1)",
        hidden: "rgba(37, 37, 45, 0)",
      },
    },
  ])(
    "resolves $name before deriving native mask alpha endpoints",
    ({ appBackground, surface, expectedCanvas, expectedMask }) => {
      const canvasColor = resolveInterfaceChatCanvasColor(
        appBackground,
        surface,
      );

      expect(canvasColor).toBe(expectedCanvas);
      expect(structuredChatNativeMaskColors(canvasColor)).toEqual(expectedMask);
    },
  );

  test("adds no fixed clearance beyond measured Composer and keyboard geometry", () => {
    expect(structuredChatScrollClearance(0, 0)).toBe(0);
    expect(structuredChatScrollClearance(76, 0)).toBe(76);
    expect(structuredChatScrollClearance(124, -280)).toBe(404);
  });

  test("uses the same offset for the sticky transform and scroll obstruction", () => {
    const translation = structuredChatOverlayTranslateY(-300, 1, 24);
    expect(translation).toBe(-276);
    expect(structuredChatScrollClearance(88, translation)).toBe(364);
    expect(
      structuredChatOverlayGeometry({
        canvasHeight: 844,
        composerHeight: 88,
        keyboardHeight: 300,
        keyboardVerticalOffset: 24,
      }).scrollClearance,
    ).toBe(364);
  });

  test("does not contract native range beneath the occupied logical offset", () => {
    expect(
      structuredChatEffectiveClearance({
        platform: "ios",
        requestedClearance: 76,
        rawOffset: -356,
        previousClearance: 356,
      }),
    ).toBe(356);
    expect(
      structuredChatEffectiveClearance({
        platform: "android",
        requestedClearance: 76,
        rawOffset: 0,
        previousClearance: 356,
      }),
    ).toBe(356);
  });

  test("contracts after the reader returns inside the requested range", () => {
    expect(
      structuredChatEffectiveClearance({
        platform: "ios",
        requestedClearance: 76,
        rawOffset: -76,
        previousClearance: 356,
      }),
    ).toBe(76);
    expect(
      structuredChatEffectiveClearance({
        platform: "android",
        requestedClearance: 76,
        rawOffset: 280,
        previousClearance: 356,
      }),
    ).toBe(76);
  });

  test("normalizes Android decorator compensation to one stable anchor", () => {
    expect(structuredChatLogicalOffset(656, 356, "android")).toBe(300);
    expect(structuredChatLogicalOffset(376, 76, "android")).toBe(300);
    expect(structuredChatLogicalOffset(300, 76, "ios")).toBe(300);
  });

  test("uses inset-aware latest targets without geometry-driven scrolling", () => {
    expect(structuredChatLatestOffset(356, "ios")).toBe(-356);
    expect(structuredChatLatestOffset(356, "android")).toBe(0);
    expect(structuredChatLatestOffset(356, "web")).toBe(0);
  });

  test("samples focus clearance and its platform latest offset atomically", () => {
    expect(structuredChatFocusSample(7, 356, "ios")).toEqual({
      intentToken: 7,
      clearance: 356,
      latestOffset: -356,
    });
    expect(structuredChatFocusSample(7, 356, "android")).toEqual({
      intentToken: 7,
      clearance: 356,
      latestOffset: 0,
    });
  });
});
