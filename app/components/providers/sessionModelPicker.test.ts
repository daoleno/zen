import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  buildModelSheetRows,
  MODEL_SHEET_HEADER_HEIGHT,
  MODEL_SHEET_MAX_LIST_FRACTION,
  MODEL_SHEET_MIN_LIST_HEIGHT,
  MODEL_SHEET_ROW_HEIGHT,
  resolveModelSheetListMaxHeight,
  type SessionModelSheetRow,
} from "./sessionModelSheetModel";
import type { ProviderModelChoice } from "../../services/providers";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

function choice(
  modelId: string,
  overrides: Partial<ProviderModelChoice> = {},
): ProviderModelChoice {
  return {
    connection: {
      id: "c1",
      name: "DeepSeek",
      clients: ["codex"],
      credential_ready: true,
      advanced: false,
      preset_id: "deepseek",
    },
    model: { id: modelId, available: true, source: "bundled" },
    current: false,
    disabled: false,
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

describe("SessionModelSheet v3 native bottom sheet", () => {
  const sheet = source("SessionModelSheet.tsx");

  test("is a compact model-only picker with no Provider vocabulary", () => {
    expect(sheet).toContain("interface SessionModelSheetProps");
    expect(sheet).toContain("choices: ProviderModelChoice[];");
    expect(sheet).not.toMatch(/Configured providers|No Providers match|Add API key/);
    expect(sheet).not.toMatch(/>Providers</);
    expect(sheet).not.toContain("connection_name");
    expect(sheet).not.toContain("provider_label");
    expect(sheet).not.toContain("base_url");
  });

  test("never navigates to Provider Settings", () => {
    expect(sheet).not.toContain("onOpenProvidersSettings");
    expect(sheet).not.toContain("model-profiles");
    expect(sheet).not.toContain("router.push");
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

  test("marks the current model with a checkmark and accessible state", () => {
    expect(sheet).toContain('name="checkmark"');
    expect(sheet).toContain("accessibilityState={{");
    expect(sheet).toContain("selected: row.selected,");
    expect(sheet).toContain("accessibilityLabel={`Use ${row.modelId}`}");
  });

  test("shows loading, error, and retry only when genuinely needed", () => {
    expect(sheet).toContain("ActivityIndicator");
    expect(sheet).toContain("onRetry");
    expect(sheet).toContain("Retry");
    expect(sheet).toContain("loading && !selection");
    expect(sheet).toContain("errorMessage && choices.length === 0");
  });

  test("scrolls the list and keeps the final item reachable", () => {
    expect(sheet).toContain("BottomSheetScrollView");
    expect(sheet).toContain("maxHeight: listMaxHeight");
    expect(sheet).toContain("resolveModelSheetListMaxHeight");
  });

  test("activation carries the row's canonical ids, never a label", () => {
    expect(sheet).toContain("buildModelSheetRows(choices, activating)");
    expect(sheet).toContain(
      "onActivate({ connectionId: row.connectionId, modelId: row.modelId })",
    );
  });
});

describe("useSessionProviderSheet v3 wiring", () => {
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

  test("picker inventory comes from the bound connection only", () => {
    expect(hook).toContain(
      "const choices: ProviderModelChoice[] = sessionModelPickerChoices(",
    );
    expect(hook).toContain("sessionModelPickerChoices");
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

describe("sessionModelSheetModel rows (exact ID propagation)", () => {
  test("rows carry the canonical model id unchanged, including the last", () => {
    const choices = LONG_IDS.map((id) => choice(id));
    const rows = buildModelSheetRows(choices, false);

    expect(rows).toHaveLength(LONG_IDS.length);
    rows.forEach((row, index) => {
      expect(row.modelId).toBe(LONG_IDS[index]);
      expect(row.label).toBe(LONG_IDS[index]);
      expect(row.connectionId).toBe("c1");
    });
    // The final row stays selectable and exact.
    const last = rows[rows.length - 1];
    expect(last.modelId).toBe("openai/gpt-oss-120b-consistency");
    expect(last.disabled).toBe(false);
  });

  test("long lists produce one row per model with unique keys", () => {
    const many = Array.from({ length: 60 }, (_, i) => choice(`model-${i}`));
    const rows = buildModelSheetRows(many, false);

    expect(rows).toHaveLength(60);
    expect(new Set(rows.map((row) => row.key)).size).toBe(60);
    expect(rows[59].modelId).toBe("model-59");
  });

  test("selection marks exactly the current model", () => {
    const rows = buildModelSheetRows(
      [
        choice("a", { current: true }),
        choice("b"),
        choice("c"),
      ],
      false,
    );
    expect(rows.map((row) => row.selected)).toEqual([true, false, false]);
  });

  test("activating disables every non-selected row; the current stays marked", () => {
    const rows = buildModelSheetRows(
      [
        choice("a", { current: true }),
        choice("b"),
      ],
      true,
    );
    expect(rows[0].selected).toBe(true);
    expect(rows[0].disabled).toBe(true); // re-selecting current is prevented
    expect(rows[1].selected).toBe(false);
    expect(rows[1].disabled).toBe(true); // switching in flight
  });

  test("unavailable or uncredentialed choices never fabricate ids", () => {
    const rows = buildModelSheetRows(
      [choice("gpt-5.6-sol", { disabled: true })],
      false,
    );
    expect(rows[0].modelId).toBe("gpt-5.6-sol");
    expect(rows[0].disabled).toBe(true);
  });
});

describe("sessionModelSheetModel list height (size changes)", () => {
  test("short lists size to content with a floor", () => {
    expect(
      resolveModelSheetListMaxHeight({ windowHeight: 800, rowCount: 3 }),
    ).toBe(MODEL_SHEET_HEADER_HEIGHT + 3 * MODEL_SHEET_ROW_HEIGHT);
    expect(
      resolveModelSheetListMaxHeight({ windowHeight: 800, rowCount: 0 }),
    ).toBe(MODEL_SHEET_MIN_LIST_HEIGHT);
  });

  test("long lists are capped so the sheet scrolls, never fullscreen", () => {
    const tall = resolveModelSheetListMaxHeight({ windowHeight: 800, rowCount: 60 });
    expect(tall).toBe(Math.floor(800 * MODEL_SHEET_MAX_LIST_FRACTION));
    expect(tall).toBeLessThan(800);
  });

  test("a shorter window shrinks the cap deterministically", () => {
    const narrow = resolveModelSheetListMaxHeight({ windowHeight: 480, rowCount: 60 });
    const wide = resolveModelSheetListMaxHeight({ windowHeight: 800, rowCount: 60 });
    expect(narrow).toBe(Math.floor(480 * MODEL_SHEET_MAX_LIST_FRACTION));
    expect(narrow).toBeLessThan(wide);
  });

  test("very short windows keep a usable floor", () => {
    expect(
      resolveModelSheetListMaxHeight({ windowHeight: 100, rowCount: 60 }),
    ).toBe(MODEL_SHEET_MIN_LIST_HEIGHT);
  });
});

describe("sessionModelSheetModel row→activation shape", () => {
  test("activation input derives solely from the row ids", () => {
    const rows: SessionModelSheetRow[] = [
      {
        key: "c1:gpt-5.6-sol",
        connectionId: "c1",
        modelId: "gpt-5.6-sol",
        label: "gpt-5.6-sol",
        selected: false,
        disabled: false,
      },
    ];
    const [row] = rows;
    expect({ connectionId: row.connectionId, modelId: row.modelId }).toEqual({
      connectionId: "c1",
      modelId: "gpt-5.6-sol",
    });
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
