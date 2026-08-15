import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  providerEditorAfterSave,
  providerEditorCanSave,
  providerEditorInitialBaseUrl,
  providerEditorInitialName,
  providerEditorRequiresBaseUrl,
  providerEditorSessionKey,
  providerEditorShouldResetFields,
  type ProvidersEditorState,
} from "./providersPresentationModel";
import { providerBaseUrlHostname, providerNameIssue } from "../../services/providers/presentation";
import type { ProvidersSnapshot } from "../../services/providers/types";

const presentationSource = readFileSync(
  join(import.meta.dir, "./ProvidersPresentation.tsx"),
  "utf8",
);
const screenSource = readFileSync(
  join(import.meta.dir, "../../app/model-profiles.tsx"),
  "utf8",
);

type ConnectionFixture = {
  id: string;
  name: string;
  preset_id: string;
  clients: string[];
  credential_ready: boolean;
  advanced: boolean;
  base_url?: string;
};

function connection(
  id = "conn-a",
  overrides: Partial<ConnectionFixture> = {},
): ConnectionFixture {
  return {
    id,
    name: "Alpha gateway",
    preset_id: "custom",
    clients: ["codex"],
    credential_ready: false,
    advanced: true,
    base_url: "https://api.example.com/v1",
    ...overrides,
  };
}

function snapshot(connections: ReturnType<typeof connection>[]): ProvidersSnapshot {
  return {
    revision: 1,
    connections,
    defaults: {},
    presets: [
      { id: "openai", label: "OpenAI", clients: ["codex"], advanced: false },
      { id: "custom", label: "Custom Gateway", clients: ["codex", "claude"], advanced: true },
    ],
    models: {},
  };
}

describe("unified Add/Edit Provider editor", () => {
  const base = {
    mutating: false,
    name: "Work gateway",
    baseUrl: "https://api.example.com/v1",
    apiKey: "sk-live",
    nameIssue: null as string | null,
    createMode: false,
    editMode: false,
    requiresBaseUrl: true,
  };

  test("create requires name, Base URL and API key", () => {
    expect(
      providerEditorCanSave({ ...base, createMode: true }),
    ).toBe(true);
    expect(
      providerEditorCanSave({ ...base, createMode: true, apiKey: "" }),
    ).toBe(false);
    expect(
      providerEditorCanSave({ ...base, createMode: true, baseUrl: "" }),
    ).toBe(false);
    expect(
      providerEditorCanSave({ ...base, createMode: true, name: "" }),
    ).toBe(false);
  });

  test("edit may save with an empty API key (preserve stored secret)", () => {
    expect(providerEditorCanSave({ ...base, editMode: true, apiKey: "" })).toBe(
      true,
    );
  });

  test("a display-name issue blocks saving in every mode", () => {
    expect(
      providerEditorCanSave({
        ...base,
        createMode: true,
        nameIssue: "Another Provider is already named “Alpha gateway”.",
      }),
    ).toBe(false);
  });

  test("busy editor never saves", () => {
    expect(
      providerEditorCanSave({ ...base, createMode: true, mutating: true }),
    ).toBe(false);
  });

  test("client identity is part of the editor target", () => {
    expect(
      providerEditorSessionKey({ kind: "create", client: "codex" }),
    ).toBe("create:codex");
    expect(
      providerEditorSessionKey({ kind: "create", client: "claude" }),
    ).toBe("create:claude");
    expect(
      providerEditorShouldResetFields("create:codex", "create:claude"),
    ).toBe(true);
    expect(
      providerEditorSessionKey({ kind: "edit", connection: connection() }),
    ).toBe("edit:conn-a");
  });

  test("edit prefills name and Base URL but never the API key", () => {
    const edit: ProvidersEditorState = {
      kind: "edit",
      connection: connection(),
    };
    expect(providerEditorInitialName(edit)).toBe("Alpha gateway");
    expect(providerEditorInitialBaseUrl(edit)).toBe(
      "https://api.example.com/v1",
    );
    expect(providerEditorRequiresBaseUrl(edit)).toBe(true);
    expect(providerEditorRequiresBaseUrl({ kind: "create", client: "codex" })).toBe(
      true,
    );
  });

  test("curated connections hide the Base URL field", () => {
    const curated: ProvidersEditorState = {
      kind: "edit",
      connection: connection("conn-curated", {
        preset_id: "openai",
        advanced: false,
        base_url: undefined,
        name: "OpenAI",
      }),
    };
    expect(providerEditorRequiresBaseUrl(curated)).toBe(false);
    expect(providerEditorInitialBaseUrl(curated)).toBe("");
    expect(
      providerEditorCanSave({
        ...base,
        editMode: true,
        requiresBaseUrl: false,
        apiKey: "",
        name: "OpenAI",
      }),
    ).toBe(true);
  });

  test("partial create success retries the same unified edit", () => {
    const created = connection("conn-created");
    const before: ProvidersEditorState = {
      kind: "create",
      client: "codex",
    };
    expect(
      providerEditorAfterSave(before, {
        status: "credential_failed",
        connection: created,
      }),
    ).toEqual({ kind: "edit", connection: created, retry: true });
    expect(
      providerEditorShouldResetFields("create:codex", "edit:conn-created"),
    ).toBe(false);
  });

  test("successful save closes the editor", () => {
    expect(
      providerEditorAfterSave(
        { kind: "create", client: "codex" },
        { status: "saved" },
      ),
    ).toBeNull();
  });
});

describe("Provider display-name helpers", () => {
  test("Base URL hostname is the secondary identity", () => {
    expect(providerBaseUrlHostname("https://api.example.com/v1")).toBe(
      "api.example.com",
    );
    expect(providerBaseUrlHostname("http://192.168.1.20:8080/v1")).toBe(
      "192.168.1.20",
    );
    expect(providerBaseUrlHostname("")).toBe("");
    expect(providerBaseUrlHostname("not a url")).toBe("not a url");
  });

  test("names are trimmed, bounded and case-insensitively unique", () => {
    const catalog = snapshot([
      connection("conn-a", { name: "Alpha gateway" }),
    ]);
    expect(providerNameIssue({ name: "  ", snapshot: catalog })).toMatch(
      /required/,
    );
    expect(
      providerNameIssue({ name: "x".repeat(65), snapshot: catalog }),
    ).toMatch(/64/);
    expect(
      providerNameIssue({ name: "alpha GATEWAY", snapshot: catalog }),
    ).toMatch(/already named/);
    expect(
      providerNameIssue({
        name: "alpha gateway",
        snapshot: catalog,
        exceptConnectionId: "conn-a",
      }),
    ).toBeNull();
    expect(
      providerNameIssue({ name: "Beta gateway", snapshot: catalog }),
    ).toBeNull();
    expect(
      providerNameIssue({ name: "Beta gateway", snapshot: null }),
    ).toBeNull();
  });
});

describe("client-first Providers surface contract", () => {
  test("uses the shared mobile input and never raw TextInput", () => {
    expect(presentationSource).not.toMatch(/<TextInput\b/);
    expect(presentationSource).toContain(
      'from "../ui/MobileSingleLineInput"',
    );
    expect(
      (presentationSource.match(/<MobileSingleLineInput\b/g) ?? []).length,
    ).toBeGreaterThanOrEqual(3);
  });

  test("makes clients first-level and official login explicitly direct", () => {
    expect(presentationSource).toContain(
      'const CLIENTS: ProviderClient[] = ["codex", "claude"]',
    );
    expect(presentationSource).toContain("Official login");
    expect(presentationSource).toContain("No Zen routing");
    expect(presentationSource).toContain("onUseDirect(client)");
  });

  test("uses the official Codex and Claude marks", () => {
    expect(presentationSource).toContain(
      'import { Claude, Codex } from "@lobehub/icons-rn"',
    );
    expect(presentationSource).toContain("<Codex.Color size={22} />");
    expect(presentationSource).toContain("<Claude.Color size={22} />");
  });

  test("does not expose upstream vendor presets as hardcoded copy", () => {
    for (const vendor of ["OpenAI", "OpenRouter", "Anthropic", "DeepSeek"]) {
      expect(presentationSource).not.toContain(`>${vendor}<`);
    }
    expect(presentationSource).not.toContain("PresetConnectRow");
    expect(presentationSource).not.toContain("onSaveCurated");
  });

  test("adds a transient connection test before persistence", () => {
    expect(presentationSource).toContain("Test connection");
    expect(presentationSource).toContain("onTestConnection");
    expect(screenSource).toContain("wsClient.testProviderConnection");
    const testIndex = screenSource.indexOf("wsClient.testProviderConnection");
    const saveIndex = screenSource.indexOf("wsClient.upsertProviderConnection");
    expect(testIndex).toBeGreaterThan(0);
    expect(saveIndex).toBeGreaterThan(0);
    expect(presentationSource).toContain("latencyMs");
    expect(presentationSource).toContain("ms");
  });

  test("unified editor starts with a prominent Name field", () => {
    expect(presentationSource).toContain("Provider name");
    expect(presentationSource).toContain('accessibilityLabel="Provider name"');
    expect(presentationSource).toContain("onSaveProvider(");
    expect(presentationSource).toContain("providerNameIssue(");
    // Name is the first field: it appears before the Base URL field.
    const nameIndex = presentationSource.indexOf(">Provider name<");
    const urlIndex = presentationSource.indexOf(">Base URL<");
    expect(nameIndex).toBeGreaterThan(0);
    expect(urlIndex).toBeGreaterThan(nameIndex);
    expect(screenSource).toContain("advancedConnectionInput(");
  });

  test("exactly one unified Edit action; Replace/Add/Clear key flows are gone", () => {
    expect(presentationSource).toContain('label="Edit"');
    expect(presentationSource).not.toMatch(/Replace key/);
    expect(presentationSource).not.toMatch(/Add API key/);
    expect(presentationSource).not.toMatch(/Edit key/);
    expect(presentationSource).not.toMatch(/Clear key/i);
    expect(presentationSource).not.toContain("onSaveCredential");
    expect(presentationSource).not.toContain("onSaveCustom");
    expect(presentationSource).not.toContain("onOpenCredential");
    expect(presentationSource).not.toContain("onClearCredential");
    expect(screenSource).not.toContain("setProviderCredential");
    expect(screenSource).not.toContain("clearProviderCredential");
    // The unified form explains the empty-key preserve rule.
    expect(presentationSource).toContain(
      "Leave empty to keep the current key.",
    );
  });

  test("connection rows show the name and Base-URL hostname, keyed by id", () => {
    expect(presentationSource).toContain("providerBaseUrlHostname(");
    expect(presentationSource).toContain("connectionSubtitle(");
    expect(presentationSource).toContain("key={connection.id}");
    expect(presentationSource).toContain("<Text style={styles.rowTitle}");
  });

  test("connection actions sync models through discovery into a picker", () => {
    expect(presentationSource).toContain('label="Sync models"');
    expect(presentationSource).toContain("onPress={onDiscover}");
    expect(presentationSource).toContain("Test connection");
    expect(screenSource).toContain("discoverProviderModels");
    expect(screenSource).toContain("setModelPicker({");
    expect(screenSource).toContain("clientForConnection(");
  });

  test("model chips select the exact default model for the current Provider", () => {
    expect(presentationSource).toContain("Models");
    expect(presentationSource).toContain("modelSupportChoices(");
    expect(presentationSource).toContain("onSelectModel(");
    expect(presentationSource).toContain("picker.client,");
    expect(presentationSource).not.toContain("Choose model");
    expect(presentationSource).not.toMatch(/>Default</);
    expect(presentationSource).not.toContain("pickerCurrentLabel");
    expect(presentationSource).toContain("modelChipSelected");
    expect(presentationSource).toContain("chipWrap");
    expect(presentationSource).toContain(
      "Choose the model for new ${providerClientLabel(picker.client)} sessions.",
    );
    expect(presentationSource).toContain('accessibilityRole={selectingDefault ? "radio" : "checkbox"}');
    expect(presentationSource).toContain("providerClientLabel(picker.client)");
    expect(presentationSource).toContain("const selected = selectingDefault");
    expect(screenSource).toContain("runSetModels");
    expect(screenSource).toContain("wsClient.setProviderModels");
    expect(screenSource).toContain("wsClient.setProviderDefault");
    expect(screenSource).toContain("toggleModelSupport(");
    expect(screenSource).not.toContain("firstSupportedModel(");
  });

  test("rows never display a single model or model-selection warning", () => {
    // A Provider owns a supported-model catalog — one model on the row is
    // misleading. Model selection lives in the Session model/provider picker.
    expect(presentationSource).not.toContain("boundModelForConnection(");
    expect(presentationSource).not.toContain("Model · ");
    expect(presentationSource).not.toContain("No model selected · Sync models");
    expect(screenSource).toContain(
      "onSelectModel={(client, connection, modelId)",
    );
  });

  test("every saved Provider overflow menu tests the exact stored connection", () => {
    expect(presentationSource).toContain('"Test Connection"');
    expect(presentationSource).toContain("onTestConnectionById(");
    expect(presentationSource).toContain("handleTestConnection");
    // The daemon resolves the persisted Base URL, protocol and active stored
    // credential ref by stable Provider ID; the App never sends a key back.
    expect(screenSource).toContain("wsClient.testSavedProviderConnection");
    expect(screenSource).toContain("connection.id");
    expect(presentationSource).toContain("Connected · ");
    expect(presentationSource).toContain("models found");
  });

  test("Edit shows a masked stored-key hint; the input stays logically empty", () => {
    expect(presentationSource).toContain("Stored key · ");
    expect(presentationSource).toContain("connection.credential_hint");
    // The hint is adjacent presentation, never the TextInput value, and the
    // unified save payload carries only name/baseUrl/apiKey.
    expect(presentationSource).toContain("name: name.trim()");
    expect(presentationSource).not.toContain("credential_hint: apiKey");
    expect(presentationSource).not.toMatch(/Replace key/);
    expect(presentationSource).not.toMatch(/Edit key/);
    expect(presentationSource).not.toMatch(/Clear key/i);
  });
});
