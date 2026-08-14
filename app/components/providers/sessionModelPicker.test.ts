import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  MODEL_SHEET_GROUP_HEADER_HEIGHT,
  MODEL_SHEET_HEADER_HEIGHT,
  MODEL_SHEET_MAX_LIST_FRACTION,
  MODEL_SHEET_MIN_LIST_HEIGHT,
  MODEL_SHEET_ROW_HEIGHT,
  resolveModelSheetListMaxHeight,
  sessionModelSheetRowCount,
} from "./sessionModelSheetModel";
import { sessionModelSheetRows } from "../../services/providers/sessionModelHelpers";
import type { ProviderSessionSelection, ProvidersSnapshot } from "../../services/providers";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

const selection: ProviderSessionSelection = {
  session_id: "tmux:@1",
  client: "codex",
  connection_id: "c1",
  connection_name: "DeepSeek",
  model_id: "deepseek-chat",
  credential_ready: true,
  hot_switchable: true,
};

function snapshot(overrides: Partial<ProvidersSnapshot> = {}): ProvidersSnapshot {
  return {
    revision: 2,
    connections: [
      {
        id: "c1",
        name: "DeepSeek",
        clients: ["codex"],
        credential_ready: true,
        advanced: false,
        preset_id: "deepseek",
      },
      {
        id: "c2",
        name: "Claude Gateway",
        clients: ["claude"],
        credential_ready: true,
        advanced: true,
        base_url: "https://api.anthropic.com",
      },
      {
        id: "c3",
        name: "Not ready",
        clients: ["codex"],
        credential_ready: false,
        advanced: false,
        preset_id: "openai",
      },
    ],
    defaults: {
      codex: { connection_id: "c1", model_id: "deepseek-chat" },
    },
    presets: [],
    models: {
      c1: [
        { id: "deepseek-chat", available: true, source: "bundled" },
        { id: "deepseek-reasoner", available: true, source: "bundled" },
        { id: "gone", available: false, source: "lkg" },
      ],
      c3: [{ id: "gpt-x", available: true, source: "bundled" }],
    },
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

describe("SessionModelSheet native Model-only sheet", () => {
  const sheet = source("SessionModelSheet.tsx");

  test("is a compact Model picker with no Provider vocabulary", () => {
    expect(sheet).toContain("interface SessionModelSheetProps");
    expect(sheet).toContain("rows: ProviderPickerModelRow[];");
    expect(sheet).toMatch(/\n\s*Model\n\s*<\/Text>/);
    // Provider groups, names, hostnames, badges and cross-Provider inventory
    // are removed from the Composer surface.
    expect(sheet).not.toContain("Provider & Model");
    expect(sheet).not.toContain("connectionName");
    expect(sheet).not.toContain("hostname");
    expect(sheet).not.toContain("No API key");
    expect(sheet).not.toContain("No models discovered.");
    expect(sheet).not.toContain("No Provider connections for this Session yet.");
    expect(sheet).not.toContain("groups:");
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

  test("model-required state is explicit: choose-a-model request, no checked row", () => {
    expect(sheet).toContain("modelRequired");
    expect(sheet).toContain("Choose a model to continue this chat. Sending is");
    expect(sheet).toContain("const checked = row.current && !modelRequired;");
    expect(sheet).toContain("Sending is paused until");
  });

  test("marks the current pair with a checkmark and accessible state", () => {
    expect(sheet).toContain('name="checkmark"');
    expect(sheet).toContain("accessibilityState={{");
    expect(sheet).toContain("selected: checked,");
    expect(sheet).toContain("accessibilityLabel={`Use ${row.modelId}`}");
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
    expect(sheet).toContain("errorMessage && rows.length === 0");
  });

  test("scrolls the list and keeps the final item reachable", () => {
    expect(sheet).toContain("BottomSheetScrollView");
    expect(sheet).toContain("maxHeight: listMaxHeight");
    expect(sheet).toContain("resolveModelSheetListMaxHeight");
    expect(sheet).toContain("modelCount: rows.length");
  });

  test("activation carries the row's canonical ids, never a label", () => {
    expect(sheet).toContain(
      "onActivate({ connectionId: row.connectionId, modelId: row.modelId })",
    );
    expect(sheet).toContain("if (row.disabled) return;");
  });
});

describe("useSessionProviderSheet wiring", () => {
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

  test("inventory comes from the Settings-selected Provider only", () => {
    expect(hook).toContain("sessionModelSheetRows({");
    expect(hook).toContain("preferredProviderConnectionId(");
    expect(hook).toContain("sessionModelRequired({");
    expect(hook).not.toContain("sessionProviderPickerGroups");
    expect(hook).not.toContain("connectionsForSession");
  });

  test("activation refuses any connection other than the preferred Provider", () => {
    expect(hook).toContain(
      "preferredId !== choice.connectionId",
    );
    expect(hook).toContain("Choose a model for the Provider selected in Settings.");
    expect(hook).toContain("// The Composer owns Model selection only");
    expect(hook).toContain("the Composer never switches Providers.");
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
    expect(hook).toContain("preferredConnectionId,");
  });

  test("an acknowledged activation carries the model onto the preferred default", () => {
    expect(hook).toContain("carryPreferredModel(result.selection, catalog)");
    expect(hook).toContain("wsClient.setProviderDefault(serverId, {");
    expect(hook).toContain("// Best-effort only; the activation itself is already acknowledged.");
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

describe("sessionModelSheetRows (preferred-Provider Model inventory)", () => {
  test("lists only the Settings-selected Provider's enabled+available models", () => {
    const rows = sessionModelSheetRows({ snapshot: snapshot(), selection });
    expect(rows.map((row) => row.connectionId)).toEqual(["c1", "c1"]);
    expect(rows.map((row) => row.modelId)).toEqual([
      "deepseek-chat",
      "deepseek-reasoner",
    ]);
    // The other codex Provider and the claude-only Provider never appear.
    expect(rows.some((row) => row.connectionId === "c3")).toBe(false);
    expect(rows.some((row) => row.connectionId === "c2")).toBe(false);
    // Disabled catalog entries are never offered.
    expect(rows.some((row) => row.modelId === "gone")).toBe(false);
  });

  test("model rows carry the canonical ids unchanged, including the last", () => {
    const long = snapshot({
      defaults: { codex: { connection_id: "c1", model_id: LONG_IDS[0] } },
      models: {
        c1: LONG_IDS.map((id) => ({
          id,
          available: true,
          source: "bundled",
        })),
      },
    });
    const rows = sessionModelSheetRows({
      snapshot: long,
      selection: { ...selection, model_id: LONG_IDS[0] },
    });
    expect(rows).toHaveLength(LONG_IDS.length);
    rows.forEach((row, index) => {
      expect(row.modelId).toBe(LONG_IDS[index]);
      expect(row.label).toBe(LONG_IDS[index]);
      expect(row.connectionId).toBe("c1");
    });
    expect(rows[rows.length - 1].modelId).toBe(
      "openai/gpt-oss-120b-consistency",
    );
    expect(rows[rows.length - 1].disabled).toBe(false);
  });

  test("same Base URL, different keys: only the preferred key's models appear", () => {
    const dual: ProvidersSnapshot = {
      ...snapshot(),
      connections: [
        {
          id: "gate-a",
          name: "Alpha Gateway",
          clients: ["codex"],
          credential_ready: true,
          advanced: true,
          base_url: "https://gate.example.com",
        },
        {
          id: "gate-b",
          name: "Beta Gateway",
          clients: ["codex"],
          credential_ready: true,
          advanced: true,
          base_url: "https://gate.example.com",
        },
      ],
      defaults: {
        codex: { connection_id: "gate-b", model_id: "beta-1" },
      },
      models: {
        "gate-a": [{ id: "alpha-1", available: true, source: "bundled" }],
        "gate-b": [{ id: "beta-1", available: true, source: "bundled" }],
      },
    };
    const rows = sessionModelSheetRows({
      snapshot: dual,
      selection: {
        ...selection,
        connection_id: "gate-a",
        connection_name: "Alpha Gateway",
        model_id: "alpha-1",
      },
    });
    // The route runs gate-a, but the preferred Provider is gate-b: the sheet
    // lists gate-b's models only (model-required until a model is chosen).
    expect(rows.map((row) => [row.connectionId, row.modelId])).toEqual([
      ["gate-b", "beta-1"],
    ]);
  });

  test("route on the preferred Provider checks the exact running pair", () => {
    const rows = sessionModelSheetRows({ snapshot: snapshot(), selection });
    expect(rows[0].current).toBe(true);
    expect(rows[1].current).toBe(false);
  });

  test("model-required state checks nothing and keeps every row selectable", () => {
    const required = snapshot({
      defaults: { codex: { connection_id: "c3" } },
    });
    const rows = sessionModelSheetRows({
      snapshot: required,
      selection,
    });
    expect(rows.every((row) => !row.current)).toBe(true);
    // The preferred Provider is uncredentialed: honest non-selectable rows.
    expect(rows.every((row) => row.disabled)).toBe(true);
    expect(rows.map((row) => row.modelId)).toEqual(["gpt-x"]);
  });

  test("activating disables every row during the in-flight switch", () => {
    const rows = sessionModelSheetRows({
      snapshot: snapshot(),
      selection,
      activating: true,
    });
    expect(rows.every((row) => row.disabled)).toBe(true);
  });

  test("uncredentialed preferred Provider rows stay visible but non-selectable", () => {
    const unready = snapshot({
      defaults: { codex: { connection_id: "c3", model_id: "gpt-x" } },
    });
    const rows = sessionModelSheetRows({
      snapshot: unready,
      selection: { ...selection, connection_id: "c3", model_id: "gpt-x" },
    });
    expect(rows.map((row) => row.modelId)).toEqual(["gpt-x"]);
    expect(rows[0].disabled).toBe(true);
  });

  test("a running pair missing from discovery stays checked and non-selectable", () => {
    const missing = snapshot({
      models: { c1: [] },
    });
    const rows = sessionModelSheetRows({ snapshot: missing, selection });
    expect(rows).toEqual([
      {
        key: "c1:deepseek-chat:current",
        connectionId: "c1",
        modelId: "deepseek-chat",
        label: "deepseek-chat",
        current: true,
        disabled: true,
        unavailableCurrent: true,
      },
    ]);
  });

  test("empty catalog or missing preferred Provider yields no rows", () => {
    expect(sessionModelSheetRows({ snapshot: null, selection })).toEqual([]);
    expect(
      sessionModelSheetRows({ snapshot: snapshot(), selection: null }),
    ).toEqual([]);
    const noPreferred = snapshot({ defaults: {} });
    expect(
      sessionModelSheetRows({ snapshot: noPreferred, selection }),
    ).toEqual([]);
  });
});

describe("sessionModelSheetModel list height (size changes)", () => {
  test("short lists size to content with a floor", () => {
    expect(
      resolveModelSheetListMaxHeight({
        windowHeight: 800,
        groupCount: 0,
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

  test("row count is the flat model row count", () => {
    const rows = sessionModelSheetRows({ snapshot: snapshot(), selection });
    expect(sessionModelSheetRowCount(rows)).toBe(2);
  });

  test("long lists are capped so the sheet scrolls, never fullscreen", () => {
    const tall = resolveModelSheetListMaxHeight({
      windowHeight: 800,
      groupCount: 0,
      modelCount: 56,
    });
    expect(tall).toBe(Math.floor(800 * MODEL_SHEET_MAX_LIST_FRACTION));
    expect(tall).toBeLessThan(800);
  });

  test("a shorter window shrinks the cap deterministically", () => {
    const narrow = resolveModelSheetListMaxHeight({
      windowHeight: 480,
      groupCount: 0,
      modelCount: 56,
    });
    const wide = resolveModelSheetListMaxHeight({
      windowHeight: 800,
      groupCount: 0,
      modelCount: 56,
    });
    expect(narrow).toBe(Math.floor(480 * MODEL_SHEET_MAX_LIST_FRACTION));
    expect(narrow).toBeLessThan(wide);
  });

  test("very short windows keep a usable floor", () => {
    expect(
      resolveModelSheetListMaxHeight({
        windowHeight: 100,
        groupCount: 0,
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
    expect(sheetSource).not.toMatch(/start a new Session/i);
    expect(sheetSource).not.toMatch(/restart/i);
  });

  test("model-required copy is a concise choose-a-model request", () => {
    const sheetSource = source("./SessionModelSheet.tsx");
    expect(sheetSource).toContain("Sending is paused until");
    expect(sheetSource).toContain("you select one.");
  });
});
