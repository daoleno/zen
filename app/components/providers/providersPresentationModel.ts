/**
 * Pure presentation policy for the one-surface Providers editor. The overlay
 * is always bound to exactly one target — a new custom endpoint or an
 * existing Provider connection — so this stays free of React Native imports
 * to keep it unit-testable.
 *
 * There is exactly one Edit action per Provider. The unified form edits Name,
 * Base URL and API key together and saves them atomically; an empty API-key
 * field preserves the stored secret (never a separate Replace/Clear key flow).
 */
import type {
  ProviderClient,
  ProviderConnection,
  ProviderModel,
} from "../../services/providers/types";

export type ProvidersEditorState =
  | { kind: "create"; client: ProviderClient }
  | { kind: "edit"; connection: ProviderConnection; retry?: boolean }
  | null;

/**
 * Open model-sync picker: the discovery result for exactly one connection,
 * rendered as compact support chips. Selected chips are the models the
 * gateway exposes (the client-owned enable allowlist); tapping toggles
 * support. Persisted only after a successful `set_provider_models` write.
 */
export type ModelSyncPickerState = {
  purpose: "support" | "default";
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
 * overlay to that connection so the next Save retries the same unified edit
 * and cannot create a duplicate.
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
        kind: "edit",
        connection: outcome.connection,
        retry: true,
      };
  }
}

/**
 * Stable identity of the editor target. Used to decide when the local form
 * fields must be reset: only a fresh open or a switch to a different target
 * warrants a reset — never a create → edit retry on the same connection.
 */
export function providerEditorSessionKey(
  editor: ProvidersEditorState,
): string {
  if (!editor) return "";
  switch (editor.kind) {
    case "create":
      return `create:${editor.client}`;
    case "edit":
      return `edit:${editor.connection.id}`;
  }
}

/**
 * Reset policy: a fresh open starts clean; closing does not reset (the close
 * handler clears fields); switching between same-kind targets resets; a
 * create → edit transition preserves every entered field for retry.
 */
export function providerEditorShouldResetFields(
  previousSession: string,
  nextSession: string,
): boolean {
  if (nextSession === "") return false;
  if (previousSession === "") return true;
  if (
    previousSession.startsWith("edit:") &&
    nextSession.startsWith("edit:")
  ) {
    return previousSession !== nextSession;
  }
  if (
    previousSession.startsWith("create:") &&
    nextSession.startsWith("create:")
  ) {
    return previousSession !== nextSession;
  }
  return false;
}

export type ProviderEditorFieldState = {
  mutating: boolean;
  name: string;
  baseUrl: string;
  apiKey: string;
  /** Inline display-name validation result; null means the name is valid. */
  nameIssue: string | null;
  /** New custom endpoint: name, Base URL and API key are all required. */
  createMode: boolean;
  /** Existing connection: empty API key preserves the stored secret. */
  editMode: boolean;
  /** Advanced/Custom connections always require a Base URL. */
  requiresBaseUrl: boolean;
};

/**
 * Save eligibility for the unified Add/Edit Provider form. In create mode the
 * API key is required; in edit mode an empty key means "keep the stored
 * secret" and is always a valid save (the daemon replaces only on non-empty).
 * A display-name issue (empty, over-long, duplicate) blocks saving so the
 * inline validation and the daemon agree before any write.
 */
export function providerEditorCanSave(
  input: ProviderEditorFieldState,
): boolean {
  if (input.mutating) return false;
  if (input.name.trim().length === 0) return false;
  if (input.nameIssue !== null) return false;
  if (input.requiresBaseUrl && input.baseUrl.trim().length === 0) return false;
  if (input.createMode && input.apiKey.trim().length === 0) return false;
  return input.createMode || input.editMode;
}

/**
 * Initial display name for a fresh form: the connection's current name when
 * editing, otherwise empty.
 */
export function providerEditorInitialName(
  editor: ProvidersEditorState,
): string {
  return editor?.kind === "edit" ? editor.connection.name : "";
}

/**
 * Initial Base URL for a fresh form: the connection's current gateway when
 * editing an advanced connection, otherwise empty. Curated connections have
 * no editable gateway.
 */
export function providerEditorInitialBaseUrl(
  editor: ProvidersEditorState,
): string {
  return editor?.kind === "edit" ? (editor.connection.base_url ?? "") : "";
}

/**
 * Whether the unified form shows an editable Base URL field for this target:
 * new custom endpoints and advanced connections only. Curated connections own
 * the official endpoint and never expose it.
 */
export function providerEditorRequiresBaseUrl(
  editor: ProvidersEditorState,
): boolean {
  if (!editor) return false;
  if (editor.kind === "create") return true;
  return Boolean(editor.connection.base_url);
}
