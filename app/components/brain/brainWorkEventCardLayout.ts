export const BRAIN_WORK_CARD_HORIZONTAL_PADDING = 14;
export const BRAIN_WORK_CARD_GAP = 7;
export const BRAIN_WORK_CARD_MIN_TITLE_WIDTH = 96;
export const BRAIN_WORK_CARD_TITLE_LINES = 2;
export const BRAIN_WORK_CARD_SUMMARY_LINES = 3;
export const BRAIN_WORK_CARD_FACT_LINES = 2;
export const BRAIN_WORK_CARD_MAX_FACTS = 3;

const COMPACT_FIXED_CONTENT_WIDTH =
  17 + // status icon
  72 + // longest lifecycle label at the capped caption scale
  42 + // compact time
  14 + // disclosure icon
  BRAIN_WORK_CARD_GAP * 4;

export function brainWorkEventCardLayout(viewportWidth: number) {
  const cardWidth = Math.max(0, viewportWidth - 2);
  const contentWidth = Math.max(
    0,
    cardWidth - BRAIN_WORK_CARD_HORIZONTAL_PADDING * 2,
  );
  const compactTitleWidth = Math.max(
    BRAIN_WORK_CARD_MIN_TITLE_WIDTH,
    contentWidth - COMPACT_FIXED_CONTENT_WIDTH,
  );
  return {
    cardWidth,
    contentWidth,
    compactTitleWidth,
    titleLines: BRAIN_WORK_CARD_TITLE_LINES,
    summaryLines: BRAIN_WORK_CARD_SUMMARY_LINES,
    factLines: BRAIN_WORK_CARD_FACT_LINES,
    maxFacts: BRAIN_WORK_CARD_MAX_FACTS,
  };
}
