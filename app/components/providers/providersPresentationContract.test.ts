import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import {
  providerEditorAfterSave,
  providerEditorCanSave,
  providerEditorSessionKey,
  providerEditorShouldResetFields,
  type ProvidersEditorState,
} from "./providersPresentationModel";

const presentationSource = readFileSync(
  join(import.meta.dir, "./ProvidersPresentation.tsx"),
  "utf8",
);
const screenSource = readFileSync(
  join(import.meta.dir, "../../app/model-profiles.tsx"),
  "utf8",
);

function connection(id = "conn-a") {
  return {
    id,
    name: "api.example.com",
    preset_id: "custom",
    clients: ["codex"],
    credential_ready: false,
    advanced: true,
    base_url: "https://api.example.com/v1",
  };
}

describe("client-first Provider editor", () => {
  const base = {
    mutating: false,
    apiKey: "sk-live",
    credentialMode: false,
    customMode: false,
    baseUrl: "",
  };

  test("custom endpoint asks only for Base URL and API key", () => {
    expect(providerEditorCanSave(base)).toBe(false);
    expect(
      providerEditorCanSave({
        ...base,
        customMode: true,
        baseUrl: "https://api.example.com/v1",
      }),
    ).toBe(true);
    expect(
      providerEditorCanSave({
        ...base,
        customMode: true,
        baseUrl: "https://api.example.com/v1",
        apiKey: "",
      }),
    ).toBe(false);
  });

  test("credential replacement needs only a new API key", () => {
    expect(providerEditorCanSave({ ...base, credentialMode: true })).toBe(true);
    expect(
      providerEditorCanSave({
        ...base,
        credentialMode: true,
        apiKey: "",
      }),
    ).toBe(false);
  });

  test("busy editor never saves", () => {
    expect(
      providerEditorCanSave({
        ...base,
        mutating: true,
        customMode: true,
        baseUrl: "https://api.example.com/v1",
      }),
    ).toBe(false);
  });

  test("client identity is part of the editor target", () => {
    expect(
      providerEditorSessionKey({ kind: "custom", client: "codex" }),
    ).toBe("custom:codex");
    expect(
      providerEditorSessionKey({ kind: "custom", client: "claude" }),
    ).toBe("custom:claude");
    expect(
      providerEditorShouldResetFields("custom:codex", "custom:claude"),
    ).toBe(true);
  });

  test("partial create success retries only the credential", () => {
    const created = connection("conn-created");
    const before: ProvidersEditorState = {
      kind: "custom",
      client: "codex",
    };
    expect(
      providerEditorAfterSave(before, {
        status: "credential_failed",
        connection: created,
      }),
    ).toEqual({ kind: "credential", connection: created, retry: true });
    expect(
      providerEditorShouldResetFields(
        "custom:codex",
        "credential:conn-created",
      ),
    ).toBe(false);
  });

  test("successful save closes the editor", () => {
    expect(
      providerEditorAfterSave(
        { kind: "custom", client: "codex" },
        { status: "saved" },
      ),
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
    ).toBeGreaterThanOrEqual(2);
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

  test("does not expose upstream vendor presets", () => {
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
    const saveIndex = screenSource.indexOf("wsClient.setProviderCredential");
    expect(testIndex).toBeGreaterThan(0);
    expect(saveIndex).toBeGreaterThan(0);
    expect(presentationSource).toContain("latencyMs");
    expect(presentationSource).toContain("ms");
  });

  test("new connections carry the selected client and no display-name field", () => {
    expect(presentationSource).toContain(
      'onOpenEditor({ kind: "custom", client })',
    );
    expect(presentationSource).not.toContain("Display name");
    expect(screenSource).toContain(
      "customGatewayCreateInput({ client, baseUrl })",
    );
  });

  test("connection actions sync models through discovery into a picker", () => {
    expect(presentationSource).toContain('label="Sync models"');
    expect(presentationSource).toContain("onPress={onDiscover}");
    // The editor's transient pre-save test stays untouched.
    expect(presentationSource).toContain("Test connection");
    expect(screenSource).toContain("discoverProviderModels");
    expect(screenSource).toContain("setModelPicker({");
    expect(screenSource).toContain("clientForConnection(connection)");
  });

  test("picking a model binds it via set_provider_default with modelId", () => {
    expect(presentationSource).toContain("Choose model");
    expect(presentationSource).toContain("modelSyncChoices(");
    expect(presentationSource).toContain("onSelectModel(");
    expect(presentationSource).toContain("picker.client,");
    expect(screenSource).toContain("runSelectModel");
    expect(screenSource).toContain(
      "wsClient.setProviderDefault(currentServerId!",
    );
    expect(screenSource).toContain("modelId,\n        revision,");
  });

  test("rows show the bound model or a sync-models hint when none is bound", () => {
    expect(presentationSource).toContain("boundModelForConnection(");
    expect(presentationSource).toContain("connectionRequiresModelSelection(");
    expect(presentationSource).toContain("Model · ");
    expect(presentationSource).toContain("No model selected · Sync models");
    expect(screenSource).toContain(
      "onSelectModel={(client, connection, modelId)",
    );
  });
});
