import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  effectRowsForRuntime,
  resolveModelSheetListMaxHeight,
  runtimeRowSurfaceKey,
} from "./sessionModelSheetModel";
import type { ProviderPickerModelRow } from "../../services/providers/sessionModelHelpers";

const rows: ProviderPickerModelRow[] = [
  { key: "a:m1", connectionId: "a", modelId: "m1", label: "m1", current: true, disabled: false, unsupported: false, unavailableCurrent: false, effectDefault: "medium", currentEffect: "", effects: ["", "low", "medium"] },
  { key: "a:m2", connectionId: "a", modelId: "m2", label: "m2", current: false, disabled: false, unsupported: false, unavailableCurrent: false, effectDefault: "", currentEffect: "", effects: [] },
  { key: "b:m3", connectionId: "b", modelId: "m3", label: "m3", current: false, disabled: false, unsupported: false, unavailableCurrent: false, effectDefault: "", currentEffect: "", effects: [] },
];

const sheetSource = readFileSync(
  join(import.meta.dir, "./SessionModelSheet.tsx"),
  "utf8",
);
const sessionProviderHookSource = readFileSync(
  join(import.meta.dir, "../terminal/screen/useSessionProviderSheet.ts"),
  "utf8",
);

describe("SessionModelSheet hierarchy", () => {
  test("Effect is a separate drill-down and never a model-list row", () => {
    expect(rows.map((row) => row.modelId)).toEqual(["m1", "m2", "m3"]);
    expect(effectRowsForRuntime(rows[0])).toEqual([
      { key: "default", effect: "", selected: true },
      { key: "low", effect: "low", selected: false },
      { key: "medium", effect: "medium", selected: false },
    ]);
    expect(sheetSource).toContain("setEffectTarget(row)");
    expect(sheetSource).toContain("if (row.effects.length > 0)");
    expect(sheetSource.indexOf("setEffectTarget(row)")).toBeLessThan(
      sheetSource.indexOf("runtimeChoiceForRow(row)"),
    );
  });

  test("Provider identity is not part of the Interface hierarchy", () => {
    expect(rows.map((row) => row.label)).toEqual(["m1", "m2", "m3"]);
    expect(sheetSource).not.toContain("connectionName");
    expect(sheetSource).not.toContain("Provider & Model");
    expect(sheetSource).not.toContain("connection.name");
    expect(sheetSource).not.toContain("connection_name");
  });

  test("focus, reconnect, resume, and eager refresh never auto-open the sheet", () => {
    const openStart = sessionProviderHookSource.indexOf("const open = useCallback");
    const fetchStart = sessionProviderHookSource.indexOf(
      "const fetchProjection = useCallback",
      openStart,
    );
    const visibleTrue = sessionProviderHookSource.indexOf("setVisible(true)");
    expect(openStart).toBeGreaterThanOrEqual(0);
    expect(visibleTrue).toBeGreaterThan(openStart);
    expect(visibleTrue).toBeLessThan(fetchStart);
    expect(sessionProviderHookSource.match(/setVisible\(true\)/g)).toHaveLength(1);
  });

  test("selected rows use accent-soft and unselected rows use neutral surface", () => {
    expect(runtimeRowSurfaceKey(true)).toBe("accentSoft");
    expect(runtimeRowSurfaceKey(false)).toBe("surfaceMuted");
  });

  test("list height remains bounded", () => {
    expect(resolveModelSheetListMaxHeight({ windowHeight: 800, rowCount: 30 })).toBe(480);
  });
});
