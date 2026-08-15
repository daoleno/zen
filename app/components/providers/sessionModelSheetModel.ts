import type { ProviderPickerModelRow } from "../../services/providers/sessionModelHelpers";

export type SessionModelSheetRow = ProviderPickerModelRow;
export type { ProviderPickerModelRow } from "../../services/providers/sessionModelHelpers";

export type RuntimeRowSurfaceKey = "accentSoft" | "surfaceMuted";

export function runtimeRowSurfaceKey(selected: boolean): RuntimeRowSurfaceKey {
  return selected ? "accentSoft" : "surfaceMuted";
}

export function effectRowsForRuntime(row: ProviderPickerModelRow) {
  return row.effects.map((effect) => ({
    key: effect,
    effect,
    selected: row.current && row.currentEffect === effect,
  }));
}

export function resolveModelSheetListMaxHeight(input: {
  windowHeight: number;
  rowCount: number;
}): number {
  const cap = Math.max(120, Math.floor(input.windowHeight * 0.6));
  return Math.min(cap, Math.max(120, 56 + input.rowCount * 44));
}
