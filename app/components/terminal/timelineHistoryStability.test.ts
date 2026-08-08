// @ts-nocheck
import { describe, expect, test } from "bun:test";
import type { CodexConversationEvent } from "../../services/codexConversation";
import {
  buildZenTimeline,
} from "./InterfaceTimelineModel";
import {
  TIMELINE_BOTTOM_THRESHOLD,
  timelineListStabilityProps,
} from "./timelineScrollPolicy";

function assistant(
  id: string,
  seq: number,
  body: string,
): CodexConversationEvent {
  return {
    id,
    seq,
    kind: "assistant_message",
    role: "assistant",
    body,
    timestamp: `2026-08-04T00:00:0${seq}.000Z`,
  };
}

describe("timeline history viewport stability", () => {
  test("canonical append retains history identity under the native anchor owner", () => {
    const before = buildZenTimeline([
      assistant("oldest", 0, "oldest"),
      assistant("history-anchor", 2, "settled history"),
    ]);
    const after = buildZenTimeline([
      ...[
        assistant("oldest", 0, "oldest"),
        assistant("history-anchor", 2, "settled history"),
      ],
      assistant("newest", 4, "live append"),
    ]);

    expect(after.find((item) => item.id === "history-anchor")?.id).toBe(
      before.find((item) => item.id === "history-anchor")?.id,
    );
    expect(after).not.toBe(before);
    expect(timelineListStabilityProps(false)).toMatchObject({
      maintainVisibleContentPosition: { minIndexForVisible: 0 },
    });
  });

  test("same-ID streaming update keeps the mounted anchor key", () => {
    const partial = buildZenTimeline([
      assistant("history-anchor", 1, "history"),
      assistant("streaming-assistant", 2, "partial"),
    ]);
    const complete = buildZenTimeline([
      assistant("history-anchor", 1, "history"),
      assistant(
        "streaming-assistant",
        2,
        "complete response with enough content to grow the row",
      ),
    ]);

    expect(complete.map((item) => item.id)).toEqual(
      partial.map((item) => item.id),
    );
    expect(complete).not.toBe(partial);
    expect(timelineListStabilityProps(false)).toMatchObject({
      maintainVisibleContentPosition: { minIndexForVisible: 0 },
    });
  });

  test("newest-edge follow belongs to native and suspends for accepted interaction", () => {
    expect(
      timelineListStabilityProps(false).maintainVisibleContentPosition,
    ).toEqual({
      minIndexForVisible: 0,
      autoscrollToTopThreshold: TIMELINE_BOTTOM_THRESHOLD,
    });
    expect(
      timelineListStabilityProps(true).maintainVisibleContentPosition,
    ).toEqual({
      minIndexForVisible: 0,
    });
    // Bounded virtualization: the detached window is a fixed viewport-multiple
    // constant, never the history length, and virtualization stays enabled.
    expect(timelineListStabilityProps(true).windowSize).toBe(21);
    expect(timelineListStabilityProps(false).windowSize).toBe(5);
    expect(timelineListStabilityProps(true)).not.toHaveProperty(
      "disableVirtualization",
    );
  });

  test("content-size and item-count mutations cannot issue an imperative follow", async () => {
    const hooksSource = await Bun.file(
      new URL("./InterfaceChatSurfaceHooks.ts", import.meta.url),
    ).text();
    const contentSizeOwner = sourceBetween(
      hooksSource,
      "const handleContentSizeChange = useCallback(",
      "const handleLayout = useCallback(",
    );
    const itemCountOwner = sourceBetween(
      hooksSource,
      "useEffect(() => {\n    if (itemCount === 0)",
      "return {\n    scrollRef,",
    );

    expect(contentSizeOwner).not.toContain("scrollToLatest(");
    expect(itemCountOwner).not.toContain("scrollToLatest(");
    expect(contentSizeOwner).toContain("updateJumpButton();");
    expect(itemCountOwner).toContain("updateJumpButton();");
  });

  test("touch, selection, drag and momentum all drive the native follow suspension", async () => {
    const hooksSource = await Bun.file(
      new URL("./InterfaceChatSurfaceHooks.ts", import.meta.url),
    ).text();

    expect(hooksSource).toContain(
      "nativeFollowSuspended,",
    );
    expect(hooksSource.match(/syncNativeFollowSuspension\(\);/g)?.length).toBe(
      8,
    );
    expect(hooksSource).toContain(
      "textSelectionActiveRef.current = true;\n    syncNativeFollowSuspension();",
    );
    expect(hooksSource).toContain(
      "timelineTouchActiveRef.current = active;\n      syncNativeFollowSuspension();",
    );
    expect(hooksSource).toContain(
      "userDraggingRef.current = true;\n    syncNativeFollowSuspension();",
    );
    expect(hooksSource).toContain(
      "userMomentumRef.current = true;\n    syncNativeFollowSuspension();",
    );
    expect(hooksSource).toContain(
      "userMomentumRef.current = timelineDragContinuesWithMomentum(",
    );
  });

  test("a suspended canonical mutation truthfully detaches the reader", async () => {
    const hooksSource = await Bun.file(
      new URL("./InterfaceChatSurfaceHooks.ts", import.meta.url),
    ).text();
    const viewSource = await Bun.file(
      new URL("./InterfaceTimelineView.tsx", import.meta.url),
    ).text();
    const mutationOwner = sourceBetween(
      hooksSource,
      "const handleTimelineItemsMutated = useCallback(",
      "const scrollToLatest = useCallback(",
    );

    expect(mutationOwner).toContain("if (implicitAnchorSuspended())");
    expect(mutationOwner).toContain("detachFromLatest();");
    expect(viewSource).toContain(
      "previousItemsRef.current = items;\n    onItemsMutated?.();",
    );
    expect(hooksSource).toContain(
      'implicitAnchorSuspended() || scrollStateRef.current.mode === "detached"',
    );
  });
});

function sourceBetween(source: string, start: string, end: string) {
  const startIndex = source.indexOf(start);
  const endIndex = source.indexOf(end, startIndex);
  expect(startIndex).toBeGreaterThanOrEqual(0);
  expect(endIndex).toBeGreaterThan(startIndex);
  return source.slice(startIndex, endIndex);
}
