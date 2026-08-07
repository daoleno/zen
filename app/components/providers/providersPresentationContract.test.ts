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
    presetSelected: false,
    customSelected: false,
    name: "",
    baseUrl: "",
  };

  test("curated save needs a chosen supplier and an API key", () => {
    expect(providerEditorCanSave(base)).toBe(false);
    expect(providerEditorCanSave({ ...base, presetSelected: true })).toBe(true);
    expect(
      providerEditorCanSave({ ...base, presetSelected: true, apiKey: "   " }),
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

  test("custom gateway needs display name, base URL, and API key", () => {
    expect(providerEditorCanSave({ ...base, customSelected: true })).toBe(
      false,
    );
    expect(
      providerEditorCanSave({
        ...base,
        customSelected: true,
        name: "My gateway",
        baseUrl: "https://gateway.example/v1",
      }),
    ).toBe(true);
    expect(
      providerEditorCanSave({
        ...base,
        customSelected: true,
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
        presetSelected: true,
      }),
    ).toBe(false);
  });
});

describe("editor save-outcome state machine", () => {
  test("failed create keeps the add overlay open and bound to nothing new", () => {
    const before: ProvidersEditorState = { kind: "add" };
    const after = providerEditorAfterSave(before, { status: "create_failed" });
    expect(after).toEqual({ kind: "add" });
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

  test("partial create success binds the overlay to the created connection for credential retry", () => {
    const created = connection("conn-created");
    const after = providerEditorAfterSave(
      { kind: "add" },
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
    expect(providerEditorShouldResetFields("", "add")).toBe(true);
    expect(providerEditorShouldResetFields("", "credential:conn-a")).toBe(true);
  });

  test("add → credential retry preserves every entered field", () => {
    expect(
      providerEditorShouldResetFields("add", "credential:conn-created"),
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
    expect(providerEditorShouldResetFields("add", "")).toBe(false);
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
      presentationSource.indexOf("supplierGroup:"),
    );
    expect(fieldStyle).not.toMatch(/paddingVertical/);
    expect(fieldStyle).not.toMatch(/lineHeight/);
    expect(fieldStyle).not.toMatch(/height:/);
    expect(presentationSource).toContain('containerStyle={styles.field}');
  });

  test("Add and Replace key live in one overlay editor, not full-page steps", () => {
    expect(presentationSource).toContain(
      'from "../ui/RisingSheet"',
    );
    expect(presentationSource).toContain("ProviderEditorSheet");
    expect(presentationSource).not.toContain("ProviderAddForm");
    expect(presentationSource).not.toContain("ProviderCredentialForm");
    expect(presentationSource).toContain("Supplier and API key in one step");
    expect(presentationSource).toContain("avoidKeyboard");
  });

  test("keeps provider-internal concepts out of user-facing copy", () => {
    for (const banned of [
      "Manual model",
      "manual_model",
      "model_id",
      "OpenAI-compatible",
      "Anthropic-compatible",
      "Auth mode",
      "protocol",
      "envelope",
    ]) {
      expect(presentationSource).not.toContain(banned);
    }
    expect(presentationSource).not.toContain("Client:");
    expect(presentationSource).not.toContain("Clients:");
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
});
