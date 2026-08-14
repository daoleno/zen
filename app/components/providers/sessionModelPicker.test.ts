import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  buildSessionProviderPickerRows,
  MODEL_SHEET_GROUP_HEADER_HEIGHT,
  MODEL_SHEET_HEADER_HEIGHT,
  MODEL_SHEET_MAX_LIST_FRACTION,
  MODEL_SHEET_MIN_LIST_HEIGHT,
  MODEL_SHEET_ROW_HEIGHT,
  resolveModelSheetListMaxHeight,
  sessionProviderPickerRowCount,
  type SessionProviderPickerRow,
} from "./sessionModelSheetModel";
import type { ProviderPickerGroup } from "../../services/providers/sessionModelHelpers";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

function group(
  connectionId: string,
  name: string,
  modelIds: string[],
  overrides: Partial<ProviderPickerGroup> = {},
): ProviderPickerGroup {
  return {
    key: connectionId,
    connectionId,
    connectionName: name,
    hostname: null,
    credentialReady: true,
    models: modelIds.map((modelId) => ({
      key: `${connectionId}:${modelId}`,
      connectionId,
      modelId,
      label: modelId,
      current: false,
      disabled: false,
      unavailableCurrent: false,
    })),
    ...overrides,
  };
}

const LONG_IDS = [
  "gpt-5.6-sol",
  "gpt-5.1-codex-max-longhaul-8k-context",
  "anthropic/claude-sonnet-4-5-20250929",
  "deepseek-r1-0528-ultra",
  "openai/gpt-oss-120b-consistency",
];

describe("SessionModelSheet v4 native Provider + Model sheet", () => {
  const sheet = source("SessionModelSheet.tsx");

  test("is a compact Provider + Model picker with Provider vocabulary", () => {
    expect(sheet).toContain("interface SessionModelSheetProps");
    expect(sheet).toContain("groups: ProviderPickerGroup[];");
    expect(sheet).toContain("Provider & Model");
    expect(sheet).toContain("connectionName");
    expect(sheet).toContain("No API key");
    expect(sheet).toContain("No models discovered.");
    expect(sheet).toContain("No Provider connections for this Session yet.");
  });

  test("never routes to Provider Settings", () => {
    expect(sheet).not.toContain("onOpenProvidersSettings");
    expect(sheet).not.toContain("model-profiles");
    expect(sheet).not.toContain("router.push");
    expect(sheet).not.toContain("Add API key in Settings");
  });

  test("has no direct-official-login or read-only explainer states", () => {
    expect(sheet).not.toContain("nonRouted");
    expect(sheet).not.toMatch(/\bDirect\b/);
    expect(sheet).not.toContain("managedReadOnly");
    expect(sheet).not.toContain("activationEnabled");
    expect(sheet).not.toContain("durabilityWarning");
    expect(sheet).not.toContain("requiresRefreshBeforeMutation");
  });

  test("presents through the native @expo/ui bottom sheet, never coordinates", () => {
    expect(sheet).toContain(
      'from "@expo/ui/community/bottom-sheet"',
    );
    expect(sheet).toContain("<BottomSheet");
    expect(sheet).toContain("enablePanDownToClose");
    expect(sheet).not.toContain("buildModelMenuPosition");
    expect(sheet).not.toContain("MODEL_MENU_WIDTH");
    expect(sheet).not.toContain("position: \"absolute\"");
    expect(sheet).not.toContain("MenuAnchorLayout");
    expect(sheet).not.toContain("useSafeAreaInsets");
  });

  test("visible is the single open/close truth driving ref present/dismiss", () => {
    expect(sheet).toContain("sheetRef.current?.present();");
    expect(sheet).toContain("sheetRef.current?.dismiss();");
    expect(sheet).toContain("index={-1}");
    expect(sheet).toContain("onClose={handleClose}");
    expect(sheet).toContain("onDismiss={handleClose}");
  });

  test("Provider groups render name, secondary hostname, and credential badge", () => {
    expect(sheet).toContain("row.connectionName");
    expect(sheet).toContain("row.hostname");
    expect(sheet).toContain("!row.credentialReady");
    expect(sheet).toContain("accessibilityRole=\"header\"");
  });

  test("marks the current pair with a checkmark and accessible state", () => {
    expect(sheet).toContain('name="checkmark"');
    expect(sheet).toContain("accessibilityState={{");
    expect(sheet).toContain("selected: row.selected,");
    expect(sheet).toContain("accessibilityLabel={`Use ${row.modelId}`}");
    // Switching requires tapping a model under the target Provider; the
    // running pair itself is never silently re-selected or replaced.
    expect(sheet).toContain("disabled={row.disabled}");
    expect(sheet).toContain(
      "Currently running; no longer available for switching.",
    );
  });

  test("shows loading, error, and retry only when genuinely needed", () => {
    expect(sheet).toContain("ActivityIndicator");
    expect(sheet).toContain("onRetry");
    expect(sheet).toContain("Retry");
    expect(sheet).toContain("loading && !selection");
    expect(sheet).toContain("errorMessage && groups.length === 0");
  });

  test("scrolls the list and keeps the final item reachable", () => {
    expect(sheet).toContain("BottomSheetScrollView");
    expect(sheet).toContain("maxHeight: listMaxHeight");
    expect(sheet).toContain("resolveModelSheetListMaxHeight");
    expect(sheet).toContain("sessionProviderPickerRowCount");
  });

  test("activation carries the row's canonical ids, never a label", () => {
    expect(sheet).toContain(
      "buildSessionProviderPickerRows(groups, activating)",
    );
    expect(sheet).toContain(
      "onActivate({ connectionId: row.connectionId, modelId: row.modelId })",
    );
  });
});

describe("useSessionProviderSheet v4 wiring", () => {
  const hook = source("../terminal/screen/useSessionProviderSheet.ts");

  test("exposes no anchor and no coordinate open path", () => {
    expect(hook).not.toContain("MenuAnchorLayout");
    expect(hook).not.toContain("openFromAnchor");
    expect(hook).not.toContain("setAnchor(");
  });

  test("open() is a no-op for Sessions without acknowledged live switch", () => {
    expect(hook).toContain(
      "!selection?.hot_switchable",
    );
    expect(hook).toContain("return;");
  });

  test("unsupported Sessions keep the whole surface hidden", () => {
    expect(hook).toContain(
      "// Unsupported Session (direct official login, OpenCode, Pi, shell):",
    );
    expect(hook).toContain("no inventory, no switch contract. Never fabricate a model surface.");
    expect(hook).toContain("This Session does not support Model switching.");
  });

  test("a refetch that loses hot-switchability closes the open sheet", () => {
    expect(hook).toContain(
      "refetchFoundBindingNotSwitchable({",
    );
    expect(hook).toContain("activationCapable,");
    expect(hook).toContain("hotSwitchable: hot,");
    expect(hook).toContain('if (mode === "sheet") {');
    expect(hook).toContain("setVisible(false);");
    expect(hook).toContain("setSelection(nextSelection);");
    expect(hook).toContain("setCatalog(null);");
    expect(hook).not.toContain('setSheetMode("hidden")');
  });

  test("picker inventory covers every compatible Provider, grouped", () => {
    expect(hook).toContain(
      "const groups: ProviderPickerGroup[] = sessionProviderPickerGroups(",
    );
    expect(hook).toContain("sessionProviderPickerGroups");
  });

  test("activation pre-validates the exact pair and keeps the old route on refusal", () => {
    expect(hook).toContain("activationTargetModel(catalog, choice)");
    expect(hook).toContain("That model is not available for this Session.");
    expect(hook).toContain("// The catalog does not admit this exact pair");
    expect(hook).toContain("never substitute another model.");
  });

  test("composer control is the managed hot-switch truth only", () => {
    expect(hook).toContain("resolveComposerModelControl({");
    expect(hook).toContain("refreshRequired: requiresRefreshBeforeMutation,");
  });

  test("failure retains the prior selection with a recoverable error", () => {
    expect(hook).toContain(
      "// Retain the prior selection on failure; the picker keeps showing it.",
    );
    expect(hook).toContain("setError(typed);");
  });
  test("success closes only after the daemon acknowledgement", () => {
    expect(hook).toContain("classification === \"applied_durable\"");
    expect(hook).toContain("setVisible(false);");
    expect(hook).toContain("result.selection");
  });
});

describe("sessionModelSheetModel rows (Provider grouping + exact IDs)", () => {
  test("flattens groups into headers interleaved with model rows", () => {
    const groups = [
      group("c1", "DeepSeek", ["deepseek-chat", "deepseek-reasoner"]),
      group("c2", "OpenAI", ["gpt-5.1-codex-max"]),
    ];
    const rows = buildSessionProviderPickerRows(groups, false);
    expect(rows.map((row) => row.kind)).toEqual([
      "group",
      "model",
      "model",
      "group",
      "model",
    ]);
    expect(rows[0]).toMatchObject({
      kind: "group",
      connectionId: "c1",
      connectionName: "DeepSeek",
    });
  });

  test("model rows carry the canonical ids unchanged, including the last", () => {
    const rows = buildSessionProviderPickerRows(
      [group("c1", "DeepSeek", LONG_IDS)],
      false,
    );
    const models = rows.filter((row) => row.kind === "model");
    expect(models).toHaveLength(LONG_IDS.length);
    models.forEach((row, index) => {
      if (row.kind === "model") {
        expect(row.modelId).toBe(LONG_IDS[index]);
        expect(row.label).toBe(LONG_IDS[index]);
        expect(row.connectionId).toBe("c1");
      }
    });
    const last = models[models.length - 1];
    expect(last.kind === "model" && last.modelId).toBe(
      "openai/gpt-oss-120b-consistency",
    );
    expect(last.kind === "model" && last.disabled).toBe(false);
  });

  test("cross-Provider rows keep each Provider's own stable connection id", () => {
    const rows = buildSessionProviderPickerRows(
      [
        group("gate-a", "Alpha Gateway", ["alpha-1"], {
          hostname: "gate.example.com",
        }),
        group("gate-b", "Beta Gateway", ["beta-1"], {
          hostname: "gate.example.com",
        }),
      ],
      false,
    );
    const pairs = rows
      .filter((row) => row.kind === "model")
      .map((row) =>
        row.kind === "model" ? [row.connectionId, row.modelId] : [],
      );
    expect(pairs).toEqual([
      ["gate-a", "alpha-1"],
      ["gate-b", "beta-1"],
    ]);
    const headers = rows.filter((row) => row.kind === "group");
    expect(headers.map((row) => row.kind === "group" && row.hostname)).toEqual([
      "gate.example.com",
      "gate.example.com",
    ]);
  });

  test("long lists produce unique keys; the final row stays reachable", () => {
    const many = Array.from({ length: 60 }, (_, i) => `model-${i}`);
    const rows = buildSessionProviderPickerRows(
      [group("c1", "DeepSeek", many)],
      false,
    );
    expect(rows).toHaveLength(61);
    expect(new Set(rows.map((row) => row.key)).size).toBe(61);
    const last = rows[rows.length - 1];
    expect(last.kind === "model" && last.modelId).toBe("model-59");
    expect(last.kind === "model" && last.disabled).toBe(false);
  });

  test("selection marks exactly the current pair", () => {
    const rows = buildSessionProviderPickerRows(
      [
        group("c1", "DeepSeek", ["a", "b", "c"], {
          models: [
            {
              key: "c1:a",
              connectionId: "c1",
              modelId: "a",
              label: "a",
              current: true,
              disabled: false,
              unavailableCurrent: false,
            },
            {
              key: "c1:b",
              connectionId: "c1",
              modelId: "b",
              label: "b",
              current: false,
              disabled: false,
              unavailableCurrent: false,
            },
            {
              key: "c1:c",
              connectionId: "c1",
              modelId: "c",
              label: "c",
              current: false,
              disabled: false,
              unavailableCurrent: false,
            },
          ],
        }),
      ],
      false,
    );
    const selected = rows
      .filter((row) => row.kind === "model")
      .map((row) => row.kind === "model" && row.selected);
    expect(selected).toEqual([true, false, false]);
  });

  test("activating disables every non-selected row; the current stays marked", () => {
    const rows = buildSessionProviderPickerRows(
      [group("c1", "DeepSeek", ["a", "b"])],
      true,
    );
    const models = rows.filter((row) => row.kind === "model");
    expect(models.every((row) => row.kind === "model" && row.disabled)).toBe(
      true,
    );
  });

  test("uncredentialed and unavailable rows stay visible but non-selectable", () => {
    const rows = buildSessionProviderPickerRows(
      [
        group("c3", "Not ready", ["gpt-x"], {
          credentialReady: false,
          models: [
            {
              key: "c3:gpt-x",
              connectionId: "c3",
              modelId: "gpt-x",
              label: "gpt-x",
              current: false,
              disabled: true,
              unavailableCurrent: false,
            },
          ],
        }),
        group("c1", "DeepSeek", [], {
          models: [
            {
              key: "c1:deepseek-chat:current",
              connectionId: "c1",
              modelId: "deepseek-chat",
              label: "deepseek-chat",
              current: true,
              disabled: true,
              unavailableCurrent: true,
            },
          ],
        }),
      ],
      false,
    );
    const models = rows.filter((row) => row.kind === "model");
    expect(models).toHaveLength(2);
    expect(models.every((row) => row.kind === "model" && row.disabled)).toBe(
      true,
    );
    expect(
      models.some(
        (row) => row.kind === "model" && row.unavailableCurrent,
      ),
    ).toBe(true);
  });
});

describe("sessionModelSheetModel row→activation shape", () => {
  test("activation input derives solely from the row ids", () => {
    const rows: SessionProviderPickerRow[] = [
      {
        kind: "group",
        key: "group:c1",
        connectionId: "c1",
        connectionName: "DeepSeek",
        hostname: null,
        credentialReady: true,
      },
      {
        kind: "model",
        key: "c1:gpt-5.6-sol",
        connectionId: "c1",
        modelId: "gpt-5.6-sol",
        label: "gpt-5.6-sol",
        selected: false,
        disabled: false,
        unavailableCurrent: false,
      },
    ];
    const model = rows.find((row) => row.kind === "model");
    if (model?.kind !== "model") throw new Error("expected model row");
    expect({ connectionId: model.connectionId, modelId: model.modelId }).toEqual({
      connectionId: "c1",
      modelId: "gpt-5.6-sol",
    });
  });
});

describe("sessionModelSheetModel list height (size changes)", () => {
  test("short lists size to content with a floor", () => {
    expect(
      resolveModelSheetListMaxHeight({
        windowHeight: 800,
        groupCount: 1,
        modelCount: 2,
      }),
    ).toBe(
      MODEL_SHEET_HEADER_HEIGHT +
        MODEL_SHEET_GROUP_HEADER_HEIGHT +
        2 * MODEL_SHEET_ROW_HEIGHT,
    );
    expect(
      resolveModelSheetListMaxHeight({
        windowHeight: 800,
        groupCount: 0,
        modelCount: 0,
      }),
    ).toBe(MODEL_SHEET_MIN_LIST_HEIGHT);
  });

  test("row count includes Provider group headers plus model rows", () => {
    const groups = [
      group("c1", "DeepSeek", ["a", "b"]),
      group("c2", "OpenAI", ["c"]),
    ];
    expect(sessionProviderPickerRowCount(groups)).toBe(5);
    expect(groups.reduce((n, g) => n + 1 + g.models.length, 0)).toBe(5);
  });

  test("long lists are capped so the sheet scrolls, never fullscreen", () => {
    const tall = resolveModelSheetListMaxHeight({
      windowHeight: 800,
      groupCount: 4,
      modelCount: 56,
    });
    expect(tall).toBe(Math.floor(800 * MODEL_SHEET_MAX_LIST_FRACTION));
    expect(tall).toBeLessThan(800);
  });

  test("a shorter window shrinks the cap deterministically", () => {
    const narrow = resolveModelSheetListMaxHeight({
      windowHeight: 480,
      groupCount: 4,
      modelCount: 56,
    });
    const wide = resolveModelSheetListMaxHeight({
      windowHeight: 800,
      groupCount: 4,
      modelCount: 56,
    });
    expect(narrow).toBe(Math.floor(480 * MODEL_SHEET_MAX_LIST_FRACTION));
    expect(narrow).toBeLessThan(wide);
  });

  test("very short windows keep a usable floor", () => {
    expect(
      resolveModelSheetListMaxHeight({
        windowHeight: 100,
        groupCount: 4,
        modelCount: 56,
      }),
    ).toBe(MODEL_SHEET_MIN_LIST_HEIGHT);
  });
});

describe("Session model sheet communicates next-message semantics", () => {
  test("the sheet copy says the change applies to the next message", () => {
    const sheetSource = source("./SessionModelSheet.tsx");
    expect(sheetSource).toContain("Applies to the next message");
    expect(sheetSource).toContain("Switching…");
    // Switching never implies Session/process recreation and the Provider row
    // never surfaces a single model.
    expect(sheetSource).not.toMatch(/start a new Session/i);
    expect(sheetSource).not.toMatch(/restart/i);
  });
});
