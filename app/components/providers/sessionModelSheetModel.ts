/**
 * Pure Session Provider+Model sheet presentation.
 *
 * The picker is a native bottom sheet (no screen-coordinate anchoring), so
 * this module keeps the deterministic row and sizing logic free of React
 * Native imports. Tests pin exact (connection_id, model_id) propagation,
 * Provider grouping, long-list reachability, and window-size behavior here;
 * the sheet component only maps rows onto the native surface.
 */

import type { ProviderPickerGroup } from "../../services/providers/sessionModelHelpers";

/**
 * Flat render row for the native sheet: either a Provider group header or a
 * selectable model row under it. Model rows always carry the exact stable
 * (connection_id, model_id) pair from the catalog.
 */
export type SessionProviderPickerRow =
  | {
      kind: "group";
      /** Stable React key: `group:<connectionId>`. */
      key: string;
      connectionId: string;
      /** Provider display name. */
      connectionName: string;
      /** Base-URL hostname secondary identity (advanced/custom only). */
      hostname: string | null;
      credentialReady: boolean;
    }
  | {
      kind: "model";
      /** Stable React key: connection id + canonical model id. */
      key: string;
      connectionId: string;
      /**
       * Canonical model id. Always exactly the catalog id — never truncated,
       * aliased, or replaced by a display label.
       */
      modelId: string;
      /** Display label; the row may truncate it visually, never by data. */
      label: string;
      /** True only for the Session's exact current (connection_id, model_id). */
      selected: boolean;
      /** Non-selectable: activating, uncredentialed, or the current pair. */
      disabled: boolean;
      /** Running pair whose model is no longer available for switching. */
      unavailableCurrent: boolean;
    };

/**
 * Flattens picker groups into sheet rows (group headers interleaved with
 * their model rows). Activation must carry the canonical ids, so each model
 * row keeps the exact connection id and model id alongside the label.
 * Re-selecting the current pair and rows during an in-flight switch are
 * non-selectable; honest states (uncredentialed, unavailable) stay visible.
 */
export function buildSessionProviderPickerRows(
  groups: ProviderPickerGroup[],
  activating: boolean,
): SessionProviderPickerRow[] {
  const rows: SessionProviderPickerRow[] = [];
  for (const group of groups) {
    rows.push({
      kind: "group",
      key: `group:${group.key}`,
      connectionId: group.connectionId,
      connectionName: group.connectionName,
      hostname: group.hostname,
      credentialReady: group.credentialReady,
    });
    for (const model of group.models) {
      rows.push({
        kind: "model",
        key: model.key,
        connectionId: model.connectionId,
        modelId: model.modelId,
        label: model.label,
        selected: model.current,
        disabled: activating || model.disabled || model.current,
        unavailableCurrent: model.unavailableCurrent,
      });
    }
  }
  return rows;
}

/** Approximate height of one model row used by the list-height resolver. */
export const MODEL_SHEET_ROW_HEIGHT = 44;
/** Approximate height of one Provider group header row. */
export const MODEL_SHEET_GROUP_HEADER_HEIGHT = 36;
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
 * through the scroll view. Provider group headers and model rows each
 * contribute their own approximate height. Deterministic across window-size
 * changes: a shorter window shrinks the cap; more rows grow the content
 * height up to the cap.
 */
export function resolveModelSheetListMaxHeight(input: {
  windowHeight: number;
  groupCount: number;
  modelCount: number;
}): number {
  const { windowHeight, groupCount, modelCount } = input;
  const cap = Math.max(
    MODEL_SHEET_MIN_LIST_HEIGHT,
    Math.floor(windowHeight * MODEL_SHEET_MAX_LIST_FRACTION),
  );
  const content =
    MODEL_SHEET_HEADER_HEIGHT +
    groupCount * MODEL_SHEET_GROUP_HEADER_HEIGHT +
    modelCount * MODEL_SHEET_ROW_HEIGHT;
  return Math.min(cap, Math.max(MODEL_SHEET_MIN_LIST_HEIGHT, content));
}

/**
 * Total rendered rows (every Provider group header plus every model row).
 * Deterministic and independent of rendering; used by tests to pin the flat
 * row stream the sheet maps onto the native surface.
 */
export function sessionProviderPickerRowCount(groups: ProviderPickerGroup[]): number {
  return groups.reduce((count, group) => count + 1 + group.models.length, 0);
}
