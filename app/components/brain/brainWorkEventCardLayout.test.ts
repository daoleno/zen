import { describe, expect, test } from "bun:test";
import {
  BRAIN_WORK_CARD_MIN_TITLE_WIDTH,
  brainWorkEventCardLayout,
} from "./brainWorkEventCardLayout";
import { brainWorkTitle } from "./brainWorkEventPresentation";

describe("Brain Work card responsive geometry", () => {
  test("preserves a word-safe title column at the narrow Android width", () => {
    const layout = brainWorkEventCardLayout(360);
    const title = brainWorkTitle(
      "data-platform-dashboard-production-release",
    );

    expect(layout).toMatchObject({
      cardWidth: 358,
      contentWidth: 330,
      compactTitleWidth: 157,
      titleLines: 2,
      summaryLines: 3,
      factLines: 2,
      maxFacts: 3,
    });
    expect(title).toBe("data platform dashboard production release");
    expect(Math.max(...title.split(" ").map((word) => word.length))).toBeLessThan(
      BRAIN_WORK_CARD_MIN_TITLE_WIDTH / 8,
    );
  });

  test("expands content width without changing density limits on a wide surface", () => {
    const narrow = brainWorkEventCardLayout(360);
    const wide = brainWorkEventCardLayout(768);

    expect(wide.contentWidth).toBe(738);
    expect(wide.compactTitleWidth).toBe(565);
    expect(wide.compactTitleWidth).toBeGreaterThan(narrow.compactTitleWidth);
    expect(wide.titleLines).toBe(narrow.titleLines);
    expect(wide.summaryLines).toBe(narrow.summaryLines);
    expect(wide.maxFacts).toBe(narrow.maxFacts);
  });
});
