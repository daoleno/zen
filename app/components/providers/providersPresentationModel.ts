/**
 * Pure presentation policy for the one-surface Providers editor. The overlay
 * is always bound to exactly one target — a curated preset, the custom
 * endpoint form, or an existing connection's credential — so this stays free
 * of React Native imports to keep it unit-testable.
 */
import type {
  ProviderClient,
  ProviderConnection,
  ProviderModel,
} from "../../services/providers/types";

export type ProvidersEditorState =
  | { kind: "custom"; client: ProviderClient }
  | { kind: "credential"; connection: ProviderConnection; retry?: boolean }
  | null;

/**
 * Open model-sync picker: the discovery result for exactly one connection,
 * waiting for the user to choose which upstream model becomes the client
 * default. Bound only after a successful `set_provider_default` write.
 */
export type ModelSyncPickerState = {
  client: ProviderClient;
  connection: ProviderConnection;
  models: ProviderModel[];
};

export type ProviderSaveOutcome =
  | { status: "saved" }
  | { status: "create_failed" }
  | { status: "credential_failed"; connection: ProviderConnection };

/**
 * Editor transitions after a save attempt settles. A confirmed save closes the
 * overlay; a failed create keeps the same bound target open untouched; a
 * credential write that failed after the connection was created rebinds the
 * overlay to that connection so the next Save retries only the credential and
 * cannot create a duplicate.
 */
export function providerEditorAfterSave(
  current: ProvidersEditorState,
  outcome: ProviderSaveOutcome,
): ProvidersEditorState {
  switch (outcome.status) {
    case "saved":
      return null;
    case "create_failed":
      return current;
    case "credential_failed":
      return {
        kind: "credential",
        connection: outcome.connection,
        retry: true,
      };
  }
}

/**
 * Stable identity of the editor target. Used to decide when the local form
 * fields must be reset: only a fresh open or a switch to a different target
 * warrants a reset — never a create → credential retry on the same target.
 */
export function providerEditorSessionKey(
  editor: ProvidersEditorState,
): string {
  if (!editor) return "";
  switch (editor.kind) {
    case "custom":
      return `custom:${editor.client}`;
    case "credential":
      return `credential:${editor.connection.id}`;
  }
}

/**
 * Reset policy: a fresh open starts clean; closing does not reset (the close
 * handler clears fields); switching between same-kind targets resets; a
 * create → credential transition preserves every entered field for retry.
 */
export function providerEditorShouldResetFields(
  previousSession: string,
  nextSession: string,
): boolean {
  if (nextSession === "") return false;
  if (previousSession === "") return true;
  if (
    previousSession.startsWith("credential:") &&
    nextSession.startsWith("credential:")
  ) {
    return previousSession !== nextSession;
  }
  if (
    previousSession.startsWith("custom:") &&
    nextSession.startsWith("custom:")
  ) {
    return previousSession !== nextSession;
  }
  return false;
}

/**
 * Save eligibility for the bound editor: a client-scoped custom endpoint needs
 * its Base URL and API key, while an existing connection only needs the
 * replacement key.
 */
export function providerEditorCanSave(input: {
  mutating: boolean;
  apiKey: string;
  credentialMode: boolean;
  customMode: boolean;
  baseUrl: string;
}): boolean {
  if (input.mutating) return false;
  if (input.apiKey.trim().length === 0) return false;
  if (input.credentialMode) return true;
  return input.customMode && input.baseUrl.trim().length > 0;
}
