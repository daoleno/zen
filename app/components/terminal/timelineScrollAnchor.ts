export type TimelineScrollAnchor = {
  itemId: string;
  relativeOffset: number;
};

/** Capture the reader's position relative to a stable rendered message cell. */
export function captureTimelineScrollAnchor(
  itemId: string,
  contentOffset: number,
  itemOffset: number,
): TimelineScrollAnchor | null {
  if (
    !itemId ||
    !Number.isFinite(contentOffset) ||
    !Number.isFinite(itemOffset)
  ) {
    return null;
  }
  return { itemId, relativeOffset: contentOffset - itemOffset };
}

/** Resolve a content offset after the anchored message has been re-laid out. */
export function resolveTimelineScrollAnchorOffset(
  anchor: TimelineScrollAnchor,
  itemOffset: number,
  maxOffset?: number,
) {
  if (
    !Number.isFinite(itemOffset) ||
    !Number.isFinite(anchor.relativeOffset)
  ) {
    return null;
  }
  const offset = itemOffset + anchor.relativeOffset;
  if (maxOffset === undefined || !Number.isFinite(maxOffset)) {
    return offset;
  }
  return Math.min(Math.max(0, offset), Math.max(0, maxOffset));
}
