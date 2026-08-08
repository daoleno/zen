/**
 * Pure Plus-menu Model sheet helpers. Activation never invents a generation.
 */

import {
  capabilityProviderDisagreementMessage,
  sessionAllowsModelProfileActivation,
  sessionIsManagedReadOnlyProfile,
  sessionSupportsModelProfileAction,
  type AgentSessionCapabilities,
} from "./sessionCapabilities";
import {
  modelChoicesForSession,
  type ProviderModelChoice,
} from "./presentation";
import type {
  ProviderSessionSelection,
  ProvidersSnapshot,
} from "./types";

export type SessionModelSheetMode =
  | "hidden"
  | "managed_readonly"
  | "active_switch"
  | "capability_mismatch"
  | "missing_selection";

/**
 * Concise Composer chip presentation for the current Session selection. The
 * chip exists only when this exact Session can safely activate another
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

export type ActivateSessionProviderRequest = {
  agentId: string;
  connectionId: string;
  modelId: string;
};

export function resolveSessionModelSheetMode(input: {
  capabilities?: AgentSessionCapabilities | null;
  selection: ProviderSessionSelection | null;
}): SessionModelSheetMode {
  if (!sessionSupportsModelProfileAction(input.capabilities)) {
    return "hidden";
  }
  if (!input.selection) {
    return "missing_selection";
  }
  const disagreement = capabilityProviderDisagreementMessage({
    managed: true,
    activeSwitch: sessionAllowsModelProfileActivation(input.capabilities),
    selectionFound: true,
    hotSwitchable: input.selection.hot_switchable === true,
  });
  if (disagreement) return "capability_mismatch";
  if (sessionIsManagedReadOnlyProfile(input.capabilities)) {
    return "managed_readonly";
  }
  return "active_switch";
}

export function sessionModelChoices(
  snapshot: ProvidersSnapshot | null | undefined,
  selection: ProviderSessionSelection | null | undefined,
): ProviderModelChoice[] {
  return modelChoicesForSession(snapshot, selection);
}

/** Available models on ready connections; not-ready stay visible but disabled. */
export function filterSessionModelChoices(
  choices: ProviderModelChoice[],
): ProviderModelChoice[] {
  return choices.filter((choice) => choice.model.available);
}

export function exactCurrentModelChoice(
  choices: ProviderModelChoice[],
): ProviderModelChoice | null {
  return choices.find((choice) => choice.current) ?? null;
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

export function activationAllowed(input: {
  mode: SessionModelSheetMode;
  choice: ProviderModelChoice;
  refreshRequired: boolean;
}): boolean {
  if (input.mode !== "active_switch") return false;
  if (input.refreshRequired) return false;
  if (input.choice.disabled || input.choice.current) return false;
  return true;
}

export function assertActivationPayloadHasNoGeneration(
  payload: Record<string, unknown>,
): void {
  if (Object.prototype.hasOwnProperty.call(payload, "generation")) {
    throw new Error("activate_session_provider must not send generation.");
  }
}
