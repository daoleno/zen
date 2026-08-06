import { describe, expect, test } from "bun:test";
import {
  ZEN_BRAND_COLORS,
  ZEN_DARK_CHAT_PALETTE,
  ZEN_LIGHT_CHAT_PALETTE,
  ZEN_LIGHT_NEUTRALS,
  ZEN_SAGE,
} from "./primitives";

describe("chat outbound send status token", () => {
  test("outboundSentClock is dedicated high-contrast Zen status on chat.background", () => {
    // Light: deep sage on canvas (outside bubble), not bubble fill / outline.
    expect(ZEN_LIGHT_CHAT_PALETTE.outboundSentClock).toBe(ZEN_SAGE[700]);
    expect(ZEN_LIGHT_CHAT_PALETTE.outboundSentClock).not.toBe(
      ZEN_LIGHT_CHAT_PALETTE.sentBubble,
    );
    expect(ZEN_LIGHT_CHAT_PALETTE.outboundSentClock).not.toBe(
      ZEN_LIGHT_CHAT_PALETTE.background,
    );
    expect(ZEN_LIGHT_CHAT_PALETTE.background).toBe(ZEN_LIGHT_NEUTRALS.canvas);

    // Dark: bright sage on near-black environment — readable outside bubble paint.
    // Same hex as sentTimestamp is intentional: both are high-contrast sage meta,
    // but outboundSentClock is the status-affordance owner (not bubble chrome).
    expect(ZEN_DARK_CHAT_PALETTE.outboundSentClock).toBe(ZEN_SAGE[200]);
    expect(ZEN_DARK_CHAT_PALETTE.outboundSentClock).not.toBe(
      ZEN_DARK_CHAT_PALETTE.sentBubble,
    );
    expect(ZEN_DARK_CHAT_PALETTE.outboundSentClock).not.toBe(
      ZEN_DARK_CHAT_PALETTE.background,
    );
    expect(ZEN_DARK_CHAT_PALETTE.background).toBe(ZEN_BRAND_COLORS.environment);
  });
});
