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

export function groupRuntimeRows(rows: ProviderPickerModelRow[]) {
  const groups: Array<{ connectionId: string; connectionName: string; rows: ProviderPickerModelRow[] }> = [];
  for (const row of rows) {
    const current = groups.at(-1);
    if (!current || current.connectionId !== row.connectionId) {
      groups.push({
        connectionId: row.connectionId,
        connectionName: row.connectionName,
        rows: [row],
      });
    } else {
      current.rows.push(row);
    }
  }
  return groups;
}

export function resolveModelSheetListMaxHeight(input: {
  windowHeight: number;
  rowCount: number;
}): number {
  const cap = Math.max(120, Math.floor(input.windowHeight * 0.6));
  return Math.min(cap, Math.max(120, 56 + input.rowCount * 44));
}
