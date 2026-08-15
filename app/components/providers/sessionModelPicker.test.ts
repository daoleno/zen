import { describe, expect, test } from "bun:test";
import {
  effectRowsForRuntime,
  groupRuntimeRows,
  resolveModelSheetListMaxHeight,
  runtimeRowSurfaceKey,
} from "./sessionModelSheetModel";
import type { ProviderPickerModelRow } from "../../services/providers/sessionModelHelpers";

const rows: ProviderPickerModelRow[] = [
  { key: "a:m1", connectionId: "a", connectionName: "Alpha", modelId: "m1", label: "m1", current: true, disabled: false, unsupported: false, unavailableCurrent: false, effectDefault: "medium", currentEffect: "medium", effects: ["low", "medium"] },
  { key: "a:m2", connectionId: "a", connectionName: "Alpha", modelId: "m2", label: "m2", current: false, disabled: false, unsupported: false, unavailableCurrent: false, effectDefault: "", currentEffect: "", effects: [] },
  { key: "b:m3", connectionId: "b", connectionName: "Beta", modelId: "m3", label: "m3", current: false, disabled: false, unsupported: false, unavailableCurrent: false, effectDefault: "", currentEffect: "", effects: [] },
];

describe("SessionModelSheet hierarchy", () => {
  test("groups Provider and Model at the first level", () => {
    expect(groupRuntimeRows(rows).map((group) => [group.connectionName, group.rows.length])).toEqual([
      ["Alpha", 2],
      ["Beta", 1],
    ]);
  });

  test("Effect is a separate drill-down and never a model-list row", () => {
    const modelRows = groupRuntimeRows(rows).flatMap((group) => group.rows);
    expect(modelRows.map((row) => row.modelId)).toEqual(["m1", "m2", "m3"]);
    expect(effectRowsForRuntime(rows[0])).toEqual([
      { key: "low", effect: "low", selected: false },
      { key: "medium", effect: "medium", selected: true },
    ]);
  });

  test("selected rows use accent-soft and unselected rows use neutral surface", () => {
    expect(runtimeRowSurfaceKey(true)).toBe("accentSoft");
    expect(runtimeRowSurfaceKey(false)).toBe("surfaceMuted");
  });

  test("list height remains bounded", () => {
    expect(resolveModelSheetListMaxHeight({ windowHeight: 800, rowCount: 30 })).toBe(480);
  });
});
