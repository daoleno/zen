import { afterAll, describe, expect, mock, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const tokenFixture = {
  TypeScale: {
    caption: {
      fontFamily: "SourceHanSansSC-Regular",
      fontSize: 12,
      fontWeight: "400",
      lineHeight: 17,
      letterSpacing: 0,
    },
  },
  Typography: {
    uiFontMedium: "SourceHanSansSC-Medium",
    terminalFont: "MapleMono-CN-Regular",
  },
  UiTextMetrics: {
    includeFontPadding: false,
    textAlignVertical: "center",
  },
} as const;

// The production metrics module consumes the canonical React Native token
// owner. Bun unit tests must replace that native boundary before importing the
// module; directly loading tokens.ts makes Bun parse React Native's Flow entry.
mock.module("../../constants/tokens", () => tokenFixture);

const {
  ACTIVITY_HEADER_DETAIL_FONT,
  ACTIVITY_HEADER_DETAIL_GAP,
  ACTIVITY_HEADER_FONT_SIZE,
  ACTIVITY_HEADER_ICON_SLOT,
  ACTIVITY_HEADER_LETTER_SPACING,
  ACTIVITY_HEADER_LINE_HEIGHT,
  ACTIVITY_HEADER_ROW_MIN_HEIGHT,
  ACTIVITY_HEADER_ROW_PADDING_VERTICAL,
  ACTIVITY_HEADER_TITLE_FONT,
  activityHeaderCentersWithIconSlot,
  activityHeaderCopyLineBoxHeight,
  activityHeaderSharedTextStyle,
} = await import("./activityHeaderTextMetrics");

afterAll(() => {
  mock.restore();
});

const headerSource = readFileSync(
  join(import.meta.dir, "InterfaceTimelineActivityHeader.tsx"),
  "utf8",
);
const metricsSource = readFileSync(
  join(import.meta.dir, "activityHeaderTextMetrics.ts"),
  "utf8",
);

describe("activity header text metrics", () => {
  test("title and detail share one caption line box from TypeScale", () => {
    expect(ACTIVITY_HEADER_FONT_SIZE).toBe(
      tokenFixture.TypeScale.caption.fontSize,
    );
    expect(ACTIVITY_HEADER_LINE_HEIGHT).toBe(
      tokenFixture.TypeScale.caption.lineHeight,
    );
    expect(ACTIVITY_HEADER_LETTER_SPACING).toBe(
      tokenFixture.TypeScale.caption.letterSpacing,
    );
    expect(activityHeaderSharedTextStyle.fontSize).toBe(ACTIVITY_HEADER_FONT_SIZE);
    expect(activityHeaderSharedTextStyle.lineHeight).toBe(
      ACTIVITY_HEADER_LINE_HEIGHT,
    );
    expect(activityHeaderSharedTextStyle.includeFontPadding).toBe(false);
    expect(activityHeaderCopyLineBoxHeight()).toBe(ACTIVITY_HEADER_LINE_HEIGHT);
  });

  test("preserves Source Han Sans title and Maple Mono detail fonts", () => {
    expect(ACTIVITY_HEADER_TITLE_FONT).toBe(
      tokenFixture.Typography.uiFontMedium,
    );
    expect(ACTIVITY_HEADER_DETAIL_FONT).toBe(
      tokenFixture.Typography.terminalFont,
    );
    expect(ACTIVITY_HEADER_TITLE_FONT).toBe("SourceHanSansSC-Medium");
    expect(ACTIVITY_HEADER_DETAIL_FONT).toBe("MapleMono-CN-Regular");
  });

  test("line box shares visual center with icon slot without row inflation", () => {
    expect(ACTIVITY_HEADER_ICON_SLOT).toBe(18);
    expect(ACTIVITY_HEADER_ROW_MIN_HEIGHT).toBe(28);
    expect(ACTIVITY_HEADER_ROW_PADDING_VERTICAL).toBe(2);
    expect(ACTIVITY_HEADER_DETAIL_GAP).toBe(6);
    expect(activityHeaderCentersWithIconSlot()).toBe(true);
    expect(
      activityHeaderCentersWithIconSlot(ACTIVITY_HEADER_LINE_HEIGHT, 20),
    ).toBe(false);
    expect(
      activityHeaderCentersWithIconSlot(
        ACTIVITY_HEADER_LINE_HEIGHT,
        ACTIVITY_HEADER_ICON_SLOT,
        16,
        0,
      ),
    ).toBe(false);
  });

  test("header styles consume shared metrics rather than platform offsets", () => {
    expect(headerSource).toContain("activityHeaderSharedTextStyle");
    expect(headerSource).toContain("ACTIVITY_HEADER_TITLE_FONT");
    expect(headerSource).toContain("ACTIVITY_HEADER_DETAIL_FONT");
    expect(headerSource).toContain("activityHeaderCopyLineBoxHeight()");
    expect(headerSource).toContain("ACTIVITY_HEADER_ROW_MIN_HEIGHT");
    expect(headerSource).toContain("ACTIVITY_HEADER_DETAIL_GAP");
    expect(headerSource).not.toContain("marginTop");
    expect(headerSource).not.toContain("marginBottom");
    expect(headerSource).not.toMatch(/\btransform\s*:/);
    expect(headerSource).not.toContain("Platform.select");
    expect(headerSource).not.toContain("Platform.OS");
    expect(metricsSource).toContain("UiTextMetrics");
    expect(metricsSource).not.toContain("marginTop");
    expect(metricsSource).not.toMatch(/\btransform\s*:/);
    const toneIconSource = readFileSync(
      join(import.meta.dir, "InterfaceTimelineActivityToneIcon.tsx"),
      "utf8",
    );
    expect(toneIconSource).toContain("ACTIVITY_HEADER_ICON_SLOT");
  });
});
