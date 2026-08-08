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
const settingsSource = readFileSync(
  join(import.meta.dir, "../../app/settings.tsx"),
  "utf8",
);

function connection(id = "conn-a") {
  return {
    id,
    name: "DeepSeek",
    preset_id: "deepseek",
    clients: ["codex"],
    credential_ready: false,
    advanced: false,
  };
}

describe("Providers editor save policy", () => {
  const base = {
    mutating: false,
    apiKey: "sk-live",
    credentialMode: false,
    presetMode: false,
    customMode: false,
    name: "",
    baseUrl: "",
  };

  test("preset-bound save needs only an API key", () => {
    expect(providerEditorCanSave(base)).toBe(false);
    expect(providerEditorCanSave({ ...base, presetMode: true })).toBe(true);
    expect(
      providerEditorCanSave({ ...base, presetMode: true, apiKey: "   " }),
    ).toBe(false);
  });

  test("credential replace needs only the API key", () => {
    expect(providerEditorCanSave({ ...base, credentialMode: true })).toBe(true);
    expect(
      providerEditorCanSave({
        ...base,
        credentialMode: true,
        apiKey: "",
      }),
    ).toBe(false);
  });

  test("custom endpoint needs display name, endpoint URL, and API key", () => {
    expect(providerEditorCanSave({ ...base, customMode: true })).toBe(false);
    expect(
      providerEditorCanSave({
        ...base,
        customMode: true,
        name: "My gateway",
        baseUrl: "https://gateway.example/v1",
      }),
    ).toBe(true);
    expect(
      providerEditorCanSave({
        ...base,
        customMode: true,
        name: "My gateway",
        baseUrl: "https://gateway.example/v1",
        apiKey: "",
      }),
    ).toBe(false);
  });

  test("mutating never allows a save", () => {
    expect(
      providerEditorCanSave({
        ...base,
        mutating: true,
        presetMode: true,
      }),
    ).toBe(false);
  });
});

describe("editor save-outcome state machine", () => {
  test("failed create keeps the preset-bound editor open on the same target", () => {
    const before: ProvidersEditorState = { kind: "preset", presetId: "openai" };
    const after = providerEditorAfterSave(before, { status: "create_failed" });
    expect(after).toEqual({ kind: "preset", presetId: "openai" });
  });

  test("failed create keeps the custom endpoint editor open", () => {
    const after = providerEditorAfterSave(
      { kind: "custom" },
      { status: "create_failed" },
    );
    expect(after).toEqual({ kind: "custom" });
  });

  test("failed credential on a direct replace stays on the same connection in retry mode", () => {
    const conn = connection();
    const before: ProvidersEditorState = {
      kind: "credential",
      connection: conn,
    };
    const after = providerEditorAfterSave(before, {
      status: "credential_failed",
      connection: conn,
    });
    expect(after).toEqual({
      kind: "credential",
      connection: conn,
      retry: true,
    });
  });

  test("partial create success rebinds the preset editor to the created connection for credential retry", () => {
    const created = connection("conn-created");
    const after = providerEditorAfterSave(
      { kind: "preset", presetId: "openai" },
      { status: "credential_failed", connection: created },
    );
    expect(after).toEqual({
      kind: "credential",
      connection: created,
      retry: true,
    });
  });

  test("partial custom create success rebinds to the created connection for credential retry", () => {
    const created = connection("conn-custom");
    const after = providerEditorAfterSave(
      { kind: "custom" },
      { status: "credential_failed", connection: created },
    );
    expect(after).toEqual({
      kind: "credential",
      connection: created,
      retry: true,
    });
  });

  test("retry success closes the overlay", () => {
    const conn = connection();
    expect(
      providerEditorAfterSave(
        { kind: "credential", connection: conn, retry: true },
        { status: "saved" },
      ),
    ).toBeNull();
  });

  test("manual close is a null transition that never reopens", () => {
    const conn = connection();
    const session = providerEditorSessionKey({
      kind: "credential",
      connection: conn,
      retry: true,
    });
    expect(session).toBe(`credential:${conn.id}`);
    expect(providerEditorSessionKey(null)).toBe("");
    expect(providerEditorShouldResetFields(session, "")).toBe(false);
  });
});

describe("editor field reset policy", () => {
  test("a fresh open resets the form", () => {
    expect(providerEditorShouldResetFields("", "preset:openai")).toBe(true);
    expect(providerEditorShouldResetFields("", "custom")).toBe(true);
    expect(providerEditorShouldResetFields("", "credential:conn-a")).toBe(true);
  });

  test("preset → credential retry preserves every entered field", () => {
    expect(
      providerEditorShouldResetFields("preset:openai", "credential:conn-created"),
    ).toBe(false);
  });

  test("custom → credential retry preserves every entered field", () => {
    expect(
      providerEditorShouldResetFields("custom", "credential:conn-created"),
    ).toBe(false);
  });

  test("switching between preset targets resets", () => {
    expect(
      providerEditorShouldResetFields("preset:openai", "preset:deepseek"),
    ).toBe(true);
    expect(
      providerEditorShouldResetFields("preset:openai", "preset:openai"),
    ).toBe(false);
  });

  test("switching between credential targets resets", () => {
    expect(
      providerEditorShouldResetFields("credential:conn-a", "credential:conn-b"),
    ).toBe(true);
    expect(
      providerEditorShouldResetFields("credential:conn-a", "credential:conn-a"),
    ).toBe(false);
  });

  test("closing never resets through the effect; the close handler owns clearing", () => {
    expect(providerEditorShouldResetFields("preset:openai", "")).toBe(false);
    expect(providerEditorShouldResetFields("custom", "")).toBe(false);
    expect(providerEditorShouldResetFields("credential:conn-a", "")).toBe(
      false,
    );
  });
});

describe("Providers presentation source contract", () => {
  test("never renders a raw single-line TextInput", () => {
    expect(presentationSource).not.toMatch(/<TextInput\b/);
    expect(presentationSource).not.toMatch(/import\s*\{[^}]*TextInput/);
    expect(screenSource).not.toMatch(/<TextInput\b/);
    expect(screenSource).not.toMatch(/import\s*\{[^}]*TextInput/);
  });

  test("routes every Provider field through the shared single-line input owner", () => {
    expect(presentationSource).toContain(
      'from "../ui/MobileSingleLineInput"',
    );
    const mobileInputCount = (
      presentationSource.match(/<MobileSingleLineInput\b/g) ?? []
    ).length;
    expect(mobileInputCount).toBeGreaterThanOrEqual(3);
    const secureCount = (
      presentationSource.match(/secureTextEntry/g) ?? []
    ).length;
    expect(secureCount).toBeGreaterThanOrEqual(1);
  });

  test("does not fight the shared control with per-screen padding or lineHeight", () => {
    expect(presentationSource).not.toMatch(/lineHeight:\s*[0-9]/);
    expect(presentationSource).not.toMatch(/height:\s*48/);
    const fieldStyle = presentationSource.slice(
      presentationSource.indexOf("field: {"),
      presentationSource.indexOf("saveBtn: {"),
    );
    expect(fieldStyle).not.toMatch(/paddingVertical/);
    expect(fieldStyle).not.toMatch(/lineHeight/);
    expect(fieldStyle).not.toMatch(/height:/);
    expect(presentationSource).toContain('containerStyle={styles.field}');
  });

  test("the generic Add Provider picker step is gone", () => {
    expect(presentationSource).not.toContain("Add Provider");
    expect(presentationSource).not.toContain("Choose a Provider");
    expect(presentationSource).not.toMatch(/kind: "add"/);
    expect(screenSource).not.toMatch(/kind: "add"/);
  });

  test("every curated preset row opens the editor already bound to that preset", () => {
    expect(presentationSource).toContain(
      'onOpenEditor({ kind: "preset", presetId: preset.id })',
    );
    expect(presentationSource).toMatch(/Connect \$\{preset\.label\}/);
    expect(presentationSource).toContain("Connect a service");
  });

  test("custom endpoint setup is secondary and exposes only name, endpoint, key", () => {
    expect(
      presentationSource.indexOf("Connect a service"),
    ).toBeGreaterThanOrEqual(0);
    expect(presentationSource.indexOf("Use custom endpoint")).toBeGreaterThan(
      presentationSource.indexOf("Connect a service"),
    );
    expect(presentationSource).toContain('onOpenEditor({ kind: "custom" })');
    expect(presentationSource).toContain("Display name");
    expect(presentationSource).toContain("Endpoint URL");
    expect(presentationSource).not.toContain("Base URL");
    expect(presentationSource).not.toContain("Custom Gateway");
  });

  test("connected rows use user-facing Connected / Needs API key copy", () => {
    expect(presentationSource).toContain('"Connected"');
    expect(presentationSource).toContain('"Needs API key"');
    expect(presentationSource).not.toContain('"Ready"');
    expect(presentationSource).not.toContain("API key needed");
  });

  test("keeps provider-internal and client-compatibility copy out of preset and connected rows", () => {
    for (const banned of [
      "Default for ",
      "Manual model",
      "manual_model",
      "model_id",
      "OpenAI-compatible",
      "Anthropic-compatible",
      "Auth mode",
      "protocol",
      "envelope",
      "Clients:",
      "Client:",
    ]) {
      expect(presentationSource).not.toContain(banned);
    }
    expect(presentationSource).not.toMatch(/preset\.clients/);
    expect(presentationSource).not.toMatch(/connection\.clients/);
  });

  test("Settings entry copy is exactly Providers / Connect services and choose models", () => {
    expect(settingsSource).toContain("Providers");
    expect(settingsSource).toContain("Connect services and choose models");
    expect(settingsSource).not.toContain(
      "Connections and models for Codex and Claude Code",
    );
  });

  test("editor stays one overlay bound to its target with a Connect action", () => {
    expect(presentationSource).toContain('from "../ui/RisingSheet"');
    expect(presentationSource).toContain("ProviderEditorSheet");
    expect(presentationSource).not.toContain("ProviderAddForm");
    expect(presentationSource).not.toContain("ProviderCredentialForm");
    expect(presentationSource).toContain("avoidKeyboard");
    expect(presentationSource).toContain(
      'accessibilityLabel={connection ? "Save" : "Connect"}',
    );
    expect(presentationSource).toContain('? "Save"\n                : "Connect"');
  });

  test("screen wires the editor state without list/add/credential page modes", () => {
    expect(screenSource).toContain("ProvidersEditorState");
    expect(screenSource).toContain("setEditor");
    expect(screenSource).not.toContain("ProvidersScreenMode");
    expect(screenSource).not.toContain("ProviderAddForm");
    expect(screenSource).not.toContain("ProviderCredentialForm");
    expect(screenSource).toContain("onSaveCurated");
    expect(screenSource).toContain("onSaveCustom");
    expect(screenSource).toContain("onSaveCredential");
  });

  test("save failures keep the overlay open: key is never cleared before await", () => {
    expect(presentationSource).not.toContain("const key = apiKey;");
    expect(presentationSource).toContain('outcome?.status === "saved"');
    expect(presentationSource).not.toMatch(
      /setApiKey\(""\)\s*;\s*\n\s*if \(connection\)/,
    );
  });

  test("explicit user close owns field clearing and the overlay stays bound on retry", () => {
    expect(presentationSource).toContain("handleClose");
    expect(presentationSource).toContain("onClose={handleClose}");
    expect(presentationSource).toContain("Retry API key");
    expect(presentationSource).toContain("The API key wasn't saved.");
  });

  test("screen transitions editor state from save outcomes instead of closing unconditionally", () => {
    expect(screenSource).toContain("providerEditorAfterSave");
    expect(screenSource).toContain("applySaveOutcome");
    expect(screenSource).toContain("editorOpenRef");
    expect(screenSource).toContain('return { status: "create_failed" as const }');
    expect(screenSource).toContain('{ status: "credential_failed", connection }');
    expect(screenSource).not.toMatch(/saveCredential[\s\S]{0,120}closeEditor\(\)/);
  });

  test("the API key input is the shared vertically-centered control with bounded scaling", () => {
    expect(presentationSource).toContain("secureTextEntry");
    expect(presentationSource).toContain("placeholder=\"Paste API key\"");
    expect(presentationSource).toContain("apiKeyAutoFocus");
  });
});
