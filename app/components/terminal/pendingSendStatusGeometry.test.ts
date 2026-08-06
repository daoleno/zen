import { describe, expect, test } from "bun:test";
import {
  PENDING_SEND_CLOCK_FAST_HAND_REST_DEG,
  PENDING_SEND_CLOCK_FAST_PERIOD_MS,
  PENDING_SEND_CLOCK_SLOW_HAND_REST_DEG,
  PENDING_SEND_CLOCK_SLOW_PERIOD_MS,
  PENDING_SEND_STATUS_GAP,
  PENDING_SEND_STATUS_MARK_SIZE,
  PENDING_SEND_STATUS_OUTSIDE_EXTENT,
  PENDING_SEND_STATUS_OUTSIDE_RIGHT,
  pendingSendStatusFitsTimelineInset,
} from "./pendingSendStatusGeometry";
import { INTERFACE_TIMELINE_HORIZONTAL_INSET } from "./interfaceTimelineGeometry";

describe("pending send status geometry", () => {
  test("mark+gap fits the timeline outer inset and encodes the negative right anchor", () => {
    expect(PENDING_SEND_STATUS_MARK_SIZE).toBe(11);
    expect(PENDING_SEND_STATUS_GAP).toBe(2);
    expect(PENDING_SEND_STATUS_OUTSIDE_EXTENT).toBe(
      PENDING_SEND_STATUS_MARK_SIZE + PENDING_SEND_STATUS_GAP,
    );
    expect(PENDING_SEND_STATUS_OUTSIDE_RIGHT).toBe(
      -PENDING_SEND_STATUS_OUTSIDE_EXTENT,
    );
    expect(PENDING_SEND_STATUS_OUTSIDE_RIGHT).toBe(-13);
    expect(INTERFACE_TIMELINE_HORIZONTAL_INSET).toBe(14);
    expect(pendingSendStatusFitsTimelineInset()).toBe(true);
    expect(
      pendingSendStatusFitsTimelineInset(
        PENDING_SEND_STATUS_OUTSIDE_EXTENT,
        INTERFACE_TIMELINE_HORIZONTAL_INSET,
      ),
    ).toBe(true);
    expect(pendingSendStatusFitsTimelineInset(15, 14)).toBe(false);
  });

  test("clock hands rest orthogonal and slow period is 3× fast (MsgClockDrawable)", () => {
    expect(PENDING_SEND_CLOCK_FAST_HAND_REST_DEG).toBe(0);
    expect(PENDING_SEND_CLOCK_SLOW_HAND_REST_DEG).toBe(90);
    expect(
      Math.abs(
        PENDING_SEND_CLOCK_SLOW_HAND_REST_DEG -
          PENDING_SEND_CLOCK_FAST_HAND_REST_DEG,
      ),
    ).toBe(90);
    expect(PENDING_SEND_CLOCK_FAST_PERIOD_MS).toBe(1500);
    expect(PENDING_SEND_CLOCK_SLOW_PERIOD_MS).toBe(
      PENDING_SEND_CLOCK_FAST_PERIOD_MS * 3,
    );
  });

  test("hand bars pivot from dial center (bottom:center, left:center-stroke/2)", async () => {
    const markSource = await Bun.file(
      new URL("./PendingSendStatusMark.tsx", import.meta.url),
    ).text();
    expect(markSource).toContain("bottom: center");
    expect(markSource).toContain("left: handLeft");
    expect(markSource).toContain(
      "center - PENDING_SEND_CLOCK_HAND_STROKE / 2",
    );
    expect(markSource).not.toContain("marginBottom");
    expect(markSource).not.toContain("justifyContent");
    expect(markSource).not.toContain("alignItems");
    expect(markSource).toContain('position: "absolute"');
  });
});
