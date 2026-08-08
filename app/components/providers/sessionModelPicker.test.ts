import { describe, expect, test } from "bun:test";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const source = (relativePath: string) =>
  readFileSync(join(import.meta.dir, relativePath), "utf8");

describe("SessionModelSheet v2 minimal picker", () => {
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

  test("marks the current model with a checkmark", () => {
    expect(sheet).toContain("const selected = choice.current;");
    expect(sheet).toContain('name="checkmark"');
    expect(sheet).toContain("accessibilityState={{");
    expect(sheet).toContain("selected,");
  });

  test("shows loading, error, and retry only when genuinely needed", () => {
    expect(sheet).toContain("ActivityIndicator");
    expect(sheet).toContain("onRetry");
    expect(sheet).toContain("Retry");
    expect(sheet).toContain("loading && !selection");
    expect(sheet).toContain("errorMessage && choices.length === 0");
  });

  test("disables rows while switching and prevents re-selecting the current", () => {
    expect(sheet).toContain("const disabled = activating || choice.disabled || selected;");
    expect(sheet).toContain("onActivate({");
  });
});

describe("useSessionProviderSheet v2 wiring", () => {
  const hook = source("../terminal/screen/useSessionProviderSheet.ts");

  test("accepts no client hint and never fabricates a direct state", () => {
    expect(hook).not.toContain("DirectSessionClient");
    expect(hook).not.toContain("isDirectSessionClient");
    expect(hook).not.toContain("resolveDirectComposerModelControl");
    expect(hook).not.toContain('setSheetMode("direct")');
    expect(hook).not.toContain("admitCatalogLoad");
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
    // The sheet may be open when a refetch discovers the binding is no
    // longer hot-switchable: it must close instead of presenting an empty
    // fabricated "No models discovered" inventory, and the fresh non-hot
    // selection keeps the Composer control hidden.
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
