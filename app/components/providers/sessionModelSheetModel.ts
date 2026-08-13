/**
 * Pure Session model-sheet presentation.
 *
 * The model picker is a native bottom sheet (no screen-coordinate anchoring),
 * so this module keeps the deterministic row and sizing logic free of React
 * Native imports. Tests pin exact model-ID propagation, long-list
 * reachability, and window-size behavior here; the sheet component only maps
 * rows onto the native surface.
 */

import type { ProviderModelChoice } from "../../services/providers";

export type SessionModelSheetRow = {
  /** Stable React key: bound connection id + canonical model id. */
  key: string;
  /** Bound connection id; never fabricated. */
  connectionId: string;
  /**
   * Canonical model id. Always exactly the catalog id — never truncated,
   * aliased, or replaced by a display label.
   */
  modelId: string;
  /** Display label; the row may truncate it visually, never by data. */
  label: string;
  selected: boolean;
  disabled: boolean;
};

/**
 * Maps picker choices to sheet rows. Selection activation must carry the
 * canonical model id, so the row keeps both the exact id and the bound
 * connection id alongside the display label.
 */
export function buildModelSheetRows(
  choices: ProviderModelChoice[],
  activating: boolean,
): SessionModelSheetRow[] {
  return choices.map((choice) => ({
    key: `${choice.connection.id}:${choice.model.id}`,
    connectionId: choice.connection.id,
    modelId: choice.model.id,
    label: choice.model.id,
    selected: choice.current,
    disabled: activating || choice.disabled || choice.current,
  }));
}

/** Approximate height of one model row used by the list-height resolver. */
export const MODEL_SHEET_ROW_HEIGHT = 44;
/** Reserved height for the sheet header (title + close). */
export const MODEL_SHEET_HEADER_HEIGHT = 56;
/** The list never exceeds this fraction of the window height. */
export const MODEL_SHEET_MAX_LIST_FRACTION = 0.6;
/** Floor that keeps a usable list on very short windows. */
export const MODEL_SHEET_MIN_LIST_HEIGHT = 120;

/**
 * Resolves the scrollable list height for the sheet. The sheet sizes to its
 * content natively; the list itself is capped so it scrolls instead of
 * covering the whole screen, and the final model item always stays reachable
 * through the scroll view. Deterministic across window-size changes: a
 * shorter window shrinks the cap; more rows grow the content height up to the
 * cap.
 */
export function resolveModelSheetListMaxHeight(input: {
  windowHeight: number;
  rowCount: number;
}): number {
  const { windowHeight, rowCount } = input;
  const cap = Math.max(
    MODEL_SHEET_MIN_LIST_HEIGHT,
    Math.floor(windowHeight * MODEL_SHEET_MAX_LIST_FRACTION),
  );
  const content =
    MODEL_SHEET_HEADER_HEIGHT + rowCount * MODEL_SHEET_ROW_HEIGHT;
  return Math.min(cap, Math.max(MODEL_SHEET_MIN_LIST_HEIGHT, content));
}
