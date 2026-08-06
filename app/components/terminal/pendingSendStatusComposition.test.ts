import { describe, expect, test } from "bun:test";
import {
  PENDING_SEND_STATUS_MARK_SIZE,
  PENDING_SEND_STATUS_OUTSIDE_EXTENT,
  PENDING_SEND_STATUS_OUTSIDE_RIGHT,
} from "./pendingSendStatusGeometry";
import { INTERFACE_TIMELINE_HORIZONTAL_INSET } from "./interfaceTimelineGeometry";

describe("pending send status composition", () => {
  test("single bubble View owns negative-right mark; no host wrapper or border/label", async () => {
    const bubbleSource = await Bun.file(
      new URL("./InterfaceTimelineMessage.tsx", import.meta.url),
    ).text();
    const timelineSource = await Bun.file(
      new URL("./InterfaceTimelineView.tsx", import.meta.url),
    ).text();
    const footerSource = await Bun.file(
      new URL("./MessageBubbleFooter.tsx", import.meta.url),
    ).text();
    const zenUser = bubbleSource.slice(
      bubbleSource.indexOf("export function ZenUserMessage"),
      bubbleSource.indexOf("function HeartbeatWakeCard"),
    );
    const stylesAt = bubbleSource.indexOf("const styles = StyleSheet.create({");
    const stylesBlock = bubbleSource.slice(stylesAt);
    const userBubbleAt = stylesBlock.indexOf("userBubble:");
    const userBubbleStyle = stylesBlock.slice(userBubbleAt, userBubbleAt + 220);
    const chatgptBubbleAt = stylesBlock.indexOf("userBubbleChatGpt:");
    const chatgptBubbleStyle = stylesBlock.slice(
      chatgptBubbleAt,
      chatgptBubbleAt + 220,
    );
    const markStyleAt = stylesBlock.indexOf("pendingSendMark:");
    const markStyle = stylesBlock.slice(markStyleAt, markStyleAt + 280);

    // No host wrapper in tree or styles.
    expect(zenUser).not.toContain("userBubbleHost");
    expect(stylesBlock).not.toContain("userBubbleHost");
    expect(stylesBlock).not.toContain("userBubbleHostChatGpt");

    // Original bubble maxWidth restored on the single bubble styles.
    expect(userBubbleStyle).toContain('maxWidth: "86%"');
    expect(userBubbleStyle).toContain('position: "relative"');
    expect(userBubbleStyle).toContain('overflow: "visible"');
    expect(chatgptBubbleStyle).toContain('maxWidth: "88%"');
    expect(chatgptBubbleStyle).toContain('position: "relative"');
    expect(chatgptBubbleStyle).toContain('overflow: "visible"');

    // Absolute mark child of the bubble, outside background via negative right.
    expect(markStyle).toContain('position: "absolute"');
    expect(markStyle).toContain("right: PENDING_SEND_STATUS_OUTSIDE_RIGHT");
    expect(markStyle).toContain("bottom: 0");
    expect(markStyle).toContain("width: PENDING_SEND_STATUS_MARK_SIZE");
    expect(markStyle).not.toContain("right: 7");
    expect(markStyle).not.toContain("right: 0");
    expect(markStyle).not.toContain("bottom: 6");
    expect(PENDING_SEND_STATUS_OUTSIDE_RIGHT).toBe(-13);
    expect(PENDING_SEND_STATUS_OUTSIDE_EXTENT).toBeLessThanOrEqual(
      INTERFACE_TIMELINE_HORIZONTAL_INSET,
    );

    // Only Pending mounts the mark; accessibility stays on the bubble View.
    expect(zenUser).toContain("styles.pendingSendMark");
    expect(zenUser).toMatch(/showPendingSendMark \? \(/);
    expect(zenUser).toMatch(
      /PendingSendStatusMark color=\{zenTheme\.chat\.outboundSentClock\}/,
    );
    expect(zenUser).toContain(
      "accessibilityState={item.pending ? { busy: true } : undefined}",
    );

    // showTimestamps may open the footer independently; mark never enters footer.
    expect(zenUser).toContain("zenTheme.chat.showTimestamps");
    expect(zenUser).toContain("MessageBubbleFooter");
    expect(footerSource).not.toContain("pending?:");
    expect(footerSource).not.toContain("outboundSentClock");
    expect(footerSource).not.toContain("PendingSendStatusMark");

    // No Pending/Sending label, no hairline outline model.
    expect(zenUser).not.toMatch(/["']Pending["']/);
    expect(zenUser).not.toMatch(/["']Sending["']/);
    expect(zenUser).not.toContain("StyleSheet.hairlineWidth");
    expect(zenUser).not.toContain("borderColor:");
    expect(zenUser).not.toContain("borderWidth:");
    expect(zenUser).not.toContain("resolvePendingUserBubbleBorderColor");

    expect(timelineSource).toContain(
      "paddingHorizontal: INTERFACE_TIMELINE_HORIZONTAL_INSET",
    );
    expect(PENDING_SEND_STATUS_MARK_SIZE).toBe(11);
  });
});
