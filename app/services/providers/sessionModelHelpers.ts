/**
 * Pure Composer model-picker helpers. The picker exists only for Sessions
 * whose current model switch is a daemon-acknowledged live route activation
 * (model_profile_managed + model_profile_active_switch + hot_switchable
 * selection). Every other Session — official-direct Codex/Claude logins,
 * OpenCode, Pi, managed read-only, capability mismatch — hides the control:
 * a dead picker is never rendered.
 *
 * Inventory is truthful per-Session: only the models of the bound Provider
 * connection are offered. Activation never invents a generation and never
 * touches Provider defaults.
 */

import {
  sessionAllowsModelProfileActivation,
  type AgentSessionCapabilities,
} from "./sessionCapabilities";
import type {
  ProviderModelChoice,
} from "./presentation";
import type {
  ProviderSessionSelection,
  ProvidersSnapshot,
} from "./types";

export type SessionModelSheetMode =
  | "hidden"
  | "switchable"
  | "error";

/**
 * Concise Composer control presentation for the current Session selection.
 * The control exists only when this exact Session can safely activate another
 * Provider Model right now; every other state omits the control entirely.
 */
export type ComposerModelControlPresentation = {
  label: string;
  accessibilityLabel: string;
};

export function resolveComposerModelControl(input: {
  capabilities?: AgentSessionCapabilities | null;
  connectionConnected: boolean;
  selection: ProviderSessionSelection | null;
  refreshRequired: boolean;
}): ComposerModelControlPresentation | null {
  if (!sessionAllowsModelProfileActivation(input.capabilities)) {
    return null;
  }
  if (!input.connectionConnected) {
    return null;
  }
  if (input.refreshRequired) {
    return null;
  }
  const selection = input.selection;
  if (!selection || selection.hot_switchable !== true) {
    return null;
  }
  const label = selection.model_id.trim() || selection.connection_name.trim();
  if (!label) {
    return null;
  }
  return {
    label,
    accessibilityLabel: `Open model selection, ${label}, ${selection.connection_name}`,
  };
}

/**
 * The picker sheet opens only for the acknowledged live-switch Session; any
 * other capability state keeps the model surface hidden entirely.
 */
export function resolveSessionModelSheetMode(input: {
  capabilities?: AgentSessionCapabilities | null;
  selection: ProviderSessionSelection | null;
  refreshRequired: boolean;
}): SessionModelSheetMode {
  if (!sessionAllowsModelProfileActivation(input.capabilities)) {
    return "hidden";
  }
  if (input.refreshRequired) {
    return "hidden";
  }
  if (!input.selection || input.selection.hot_switchable !== true) {
    return "hidden";
  }
  return "switchable";
}

/**
 * Inventory for this Session's picker: only the models of the bound Provider
 * connection. Other connections, Provider names, Base URLs, and global
 * defaults never appear here.
 */
export function sessionModelPickerChoices(
  snapshot: ProvidersSnapshot | null | undefined,
  selection: ProviderSessionSelection | null | undefined,
): ProviderModelChoice[] {
  if (!snapshot || !selection) return [];
  const connection = snapshot.connections.find(
    (item) => item.id === selection.connection_id,
  );
  if (!connection) return [];
  return (snapshot.models[connection.id] ?? [])
    .filter((model) => model.available)
    .map((model) => ({
      connection,
      model,
      current:
        selection.connection_id === connection.id &&
        selection.model_id === model.id,
      disabled: !connection.credential_ready,
    }));
}

export type ActivateSessionProviderRequest = {
  agentId: string;
  connectionId: string;
  modelId: string;
};

export function activationAllowed(input: {
  mode: SessionModelSheetMode;
  choice: ProviderModelChoice;
  refreshRequired: boolean;
}): boolean {
  if (input.mode !== "switchable") return false;
  if (input.refreshRequired) return false;
  if (input.choice.disabled || input.choice.current) return false;
  return true;
}

/**
 * Build the activate_session_provider request. Intentionally omits generation
 * and any Profile-era fields.
 */
export function buildActivateSessionProviderRequest(input: {
  agentId: string;
  connectionId: string;
  modelId: string;
}): ActivateSessionProviderRequest {
  const agentId = input.agentId.trim();
  const connectionId = input.connectionId.trim();
  const modelId = input.modelId.trim();
  if (!agentId || !connectionId || !modelId) {
    throw new Error("agent_id, connection_id, and model_id are required.");
  }
  return { agentId, connectionId, modelId };
}

export function assertActivationPayloadHasNoGeneration(
  payload: Record<string, unknown>,
): void {
  if (Object.prototype.hasOwnProperty.call(payload, "generation")) {
    throw new Error("activate_session_provider must not send generation.");
  }
}
