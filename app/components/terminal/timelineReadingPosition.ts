import type { MutableRefObject } from "react";

export interface TimelineCellFrame {
  offset: number;
  length: number;
}
export type MeasureTimelineCell = (
  callback: (screenY: number, height: number) => void,
) => void;
export interface TimelineReadingAnchor {
  id: string;
  // Distance from the visual top of an inverted cell to the reading edge.
  intraOffset: number;
  nativeInset?: number;
  index: number;
  neighbors: string[];
}
export interface TimelineReadingBookmark {
  mode: "attached" | "detached";
  anchor?: TimelineReadingAnchor;
}
export interface TimelineReadingPosition {
  scope: string;
  initialAnchor?: TimelineReadingAnchor;
  revealLatest: MutableRefObject<(() => boolean) | null>;
  onItems(ids: string[], anchorAlias?: string): void;
  onCellLayout(
    id: string,
    frame: TimelineCellFrame,
    measure?: MeasureTimelineCell,
  ): void;
  onViewportOrigin(screenY: number): void;
  onCellUnmount(id: string): void;
  onInsetChange(inset: number): void;
  userScrolling(): boolean;
  current(): TimelineReadingBookmark & {
    contentOffset: number;
    viewportHeight: number;
    observedIntraOffset?: number;
  };
}

const bookmarks = new Map<string, TimelineReadingBookmark>();
const MAX_READING_BOOKMARKS = 32;

export function recallTimelineReadingPosition(
  scope: string,
): TimelineReadingBookmark {
  return bookmarks.get(scope) ?? { mode: "attached" };
}
export function rememberTimelineReadingPosition(
  scope: string,
  bookmark: TimelineReadingBookmark,
) {
  bookmarks.delete(scope);
  bookmarks.set(scope, bookmark);
  while (bookmarks.size > MAX_READING_BOOKMARKS)
    bookmarks.delete(bookmarks.keys().next().value!);
}

export function captureTimelineReadingAnchor(
  frames: ReadonlyMap<string, TimelineCellFrame>,
  ids: readonly string[],
  offset: number,
  viewport: number,
  topInset: number,
  contentTranslation = 0,
): TimelineReadingAnchor | undefined {
  if (viewport <= 0) return undefined;
  const readingEdge = offset + viewport - topInset - contentTranslation;
  let selected: { id: string; frame: TimelineCellFrame } | undefined;
  for (const [id, frame] of frames) {
    if (
      id.startsWith("date:") ||
      frame.length <= 0 ||
      frame.offset >= readingEdge ||
      frame.offset + frame.length <= offset
    )
      continue;
    if (!selected || frame.offset > selected.frame.offset)
      selected = { id, frame };
  }
  if (!selected) return undefined;
  const index = ids.indexOf(selected.id);
  if (index < 0) return undefined;
  return {
    id: selected.id,
    index,
    intraOffset: selected.frame.offset + selected.frame.length - readingEdge,
    neighbors: ids
      .slice(Math.max(0, index - 2), index + 3)
      .filter((id) => id !== selected!.id),
  };
}

export function resolveTimelineReadingAnchor(
  anchor: TimelineReadingAnchor,
  ids: readonly string[],
): TimelineReadingAnchor | undefined {
  if (ids.includes(anchor.id)) return anchor;
  const id =
    anchor.neighbors.find((id) => ids.includes(id)) ??
    ids[Math.min(anchor.index, ids.length - 1)];
  if (!id) return undefined;
  return { id, index: ids.indexOf(id), intraOffset: 0, neighbors: [] };
}

export function timelineReadingOffset(
  anchor: TimelineReadingAnchor,
  frame: TimelineCellFrame,
  viewport: number,
  topInset: number,
  contentTranslation = 0,
) {
  return (
    frame.offset +
    frame.length +
    contentTranslation -
    viewport +
    topInset -
    anchor.intraOffset
  );
}

export function initialTimelineReadingWindow(
  ids: readonly string[],
  anchor?: TimelineReadingAnchor,
): { start?: string; initialCount: number } | null {
  if (anchor && ids.length === 0) return null;
  const resolved = anchor
    ? resolveTimelineReadingAnchor(anchor, ids)
    : undefined;
  const index = resolved ? ids.indexOf(resolved.id) : -1;
  const start = Math.max(0, index - 8);
  return {
    start: start ? ids[start] : undefined,
    initialCount: Math.max(8, Math.min(10, index - start + 2)),
  };
}
