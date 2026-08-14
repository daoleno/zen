/**
 * Pure Session Model sheet presentation.
 *
 * The picker is a native bottom sheet (no screen-coordinate anchoring), so
 * this module keeps the deterministic row and sizing logic free of React
 * Native imports. Tests pin exact (connection_id, model_id) propagation,
 * model-only inventory, long-list reachability, effort-section reachability,
 * and window-size behavior here; the sheet component only maps rows onto the
 * native surface.
 *
 * Product boundary: the sheet lists enabled/available Models of the
 * Settings-selected Provider only — never Provider groups, names, hostnames,
 * or a cross-Provider inventory. The Reasoning Effort section projects the
 * daemon-owned contract of the Session's model (never fabricated values).
 */

import type { ProviderPickerModelRow } from "../../services/providers/sessionModelHelpers";
import type {
  ProviderSessionSelection,
  ProvidersSnapshot,
} from "../../services/providers/types";

export type { ProviderPickerModelRow } from "../../services/providers/sessionModelHelpers";

/**
 * Flat render rows for the native model sheet. Model rows always carry the
 * exact stable (connection_id, model_id) pair from the preferred Provider's
 * catalog. There are no group headers: the sheet is Model-only.
 */
export type SessionModelSheetRow = ProviderPickerModelRow;

/**
 * Total rendered rows (flat model rows only). Deterministic and independent
 * of rendering; used by tests to pin the flat row stream the sheet maps onto
 * the native surface.
 */
export function sessionModelSheetRowCount(
  rows: ProviderPickerModelRow[],
): number {
  return rows.length;
}

/** Approximate height of one model row used by the list-height resolver. */
export const MODEL_SHEET_ROW_HEIGHT = 44;
/** Approximate height of one Reasoning Effort row. */
export const EFFORT_SHEET_ROW_HEIGHT = 40;
/** Height of the Reasoning Effort section header. */
export const EFFORT_SHEET_HEADER_HEIGHT = 34;
/** Reserved height for the sheet header (title + close). */
export const MODEL_SHEET_HEADER_HEIGHT = 56;
/** The list never exceeds this fraction of the window height. */
export const MODEL_SHEET_MAX_LIST_FRACTION = 0.6;
/** Floor that keeps a usable list on very short windows. */
export const MODEL_SHEET_MIN_LIST_HEIGHT = 120;
/** Provider group headers contribute zero rows in the model-only sheet. */
export const MODEL_SHEET_GROUP_HEADER_HEIGHT = 0;

/**
 * Resolves the scrollable list height for the sheet. The sheet sizes to its
 * content natively; the list itself is capped so it scrolls instead of
 * covering the whole screen, and the final item (model or effort) always
 * stays reachable through the scroll view. Deterministic across window-size
 * changes: a shorter window shrinks the cap; more rows grow the content
 * height up to the cap.
 */
export function resolveModelSheetListMaxHeight(input: {
  windowHeight: number;
  groupCount: number;
  modelCount: number;
  effortCount: number;
}): number {
  const { windowHeight, groupCount, modelCount, effortCount } = input;
  const cap = Math.max(
    MODEL_SHEET_MIN_LIST_HEIGHT,
    Math.floor(windowHeight * MODEL_SHEET_MAX_LIST_FRACTION),
  );
  const effortSection =
    effortCount > 0
      ? EFFORT_SHEET_HEADER_HEIGHT + effortCount * EFFORT_SHEET_ROW_HEIGHT
      : 0;
  const content =
    MODEL_SHEET_HEADER_HEIGHT +
    groupCount * MODEL_SHEET_GROUP_HEADER_HEIGHT +
    modelCount * MODEL_SHEET_ROW_HEIGHT +
    effortSection;
  return Math.min(cap, Math.max(MODEL_SHEET_MIN_LIST_HEIGHT, content));
}
