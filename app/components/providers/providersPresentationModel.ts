/**
 * Pure presentation policy for the one-surface Providers editor. Supplier
 * selection and credentials always coexist in the same overlay, so this stays
 * free of React Native imports to keep it unit-testable.
 */
import type { ProviderConnection } from "../../services/providers/types";

export type ProvidersEditorState =
  | { kind: "add" }
  | { kind: "credential"; connection: ProviderConnection; retry?: boolean }
  | null;

export type ProviderSaveOutcome =
  | { status: "saved" }
  | { status: "create_failed" }
  | { status: "credential_failed"; connection: ProviderConnection };

/**
 * Editor transitions after a save attempt settles. A confirmed save closes the
 * overlay; a failed create keeps it open untouched; a credential write that
 * failed after the connection was created binds the overlay to that connection
 * so the next Save retries only the credential and cannot create a duplicate.
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
 * fields must be reset: only a fresh open or a switch to a different
 * credential target warrants a reset — never an add → credential retry.
 */
export function providerEditorSessionKey(
  editor: ProvidersEditorState,
): string {
  if (!editor) return "";
  return editor.kind === "credential"
    ? `credential:${editor.connection.id}`
    : "add";
}

/**
 * Reset policy: a fresh open starts clean; closing does not reset (the close
 * handler clears fields); switching between credential targets resets; an
 * add → credential transition preserves every entered field for retry.
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
  return false;
}

/**
 * Save eligibility for the one-surface editor: supplier and credential always
 * coexist, so a curated save needs only a chosen supplier + API key, Custom
 * Gateway adds display name + base URL, and credential replace needs the key.
 */
export function providerEditorCanSave(input: {
  mutating: boolean;
  apiKey: string;
  credentialMode: boolean;
  presetSelected: boolean;
  customSelected: boolean;
  name: string;
  baseUrl: string;
}): boolean {
  if (input.mutating) return false;
  if (input.apiKey.trim().length === 0) return false;
  if (input.credentialMode) return true;
  if (input.presetSelected) return true;
  return (
    input.customSelected &&
    input.name.trim().length > 0 &&
    input.baseUrl.trim().length > 0
  );
}
