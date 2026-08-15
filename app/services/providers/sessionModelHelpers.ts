import {
  sessionAllowsModelProfileActivation,
  type AgentSessionCapabilities,
} from "./sessionCapabilities";
import type {
  ThreadRuntimeSelection,
  ProvidersSnapshot,
  ThreadRuntimeChoice,
} from "./types";
import { normalizeProviderClient, normalizeProviderId } from "./types";

export type ComposerModelControlPresentation = {
  label: string;
  accessibilityLabel: string;
};

export type ProviderPickerModelRow = {
  key: string;
  connectionId: string;
  modelId: string;
  label: string;
  current: boolean;
  disabled: boolean;
  unsupported: boolean;
  unavailableCurrent: boolean;
  effectDefault: string;
  effects: string[];
  currentEffect: string;
};

export const CODEX_REASONING_EFFECT_VOCABULARY = [
  "minimal",
  "low",
  "medium",
  "high",
  "xhigh",
  "max",
] as const;

function modelEffectChoices(client: string, effects: string[]): string[] {
  if (client !== "codex") return [];
  const choices = effects.length > 0
    ? effects
    : [...CODEX_REASONING_EFFECT_VOCABULARY];
  return ["", ...choices.filter((effect, index) =>
    effect.trim() !== "" && choices.indexOf(effect) === index
  )];
}

export type SessionEffortContract = {
  current: string;
  override: string;
  defaultEffort: string;
  supported: string[];
};

export function threadRuntimeRows(input: {
  snapshot: ProvidersSnapshot | null | undefined;
  selection: ThreadRuntimeSelection | null | undefined;
  activating?: boolean;
}): ProviderPickerModelRow[] {
  const { snapshot, selection } = input;
  if (!snapshot || !selection) return [];
  const client = normalizeProviderClient(selection.client);
  const currentConnectionId = normalizeProviderId(selection.connection_id);
  const rows: ProviderPickerModelRow[] = [];
  for (const connection of snapshot.connections) {
    if (!connection.clients.map(normalizeProviderClient).includes(client)) continue;
    const connectionId = normalizeProviderId(connection.id);
    if (connectionId !== currentConnectionId) continue;
    for (const model of snapshot.models[connectionId] ?? []) {
      if (!model.available) continue;
      const effects = modelEffectChoices(client, [...(model.reasoning_efforts ?? [])]);
      rows.push({
        key: `${connectionId}:${model.id}`,
        connectionId,
        modelId: model.id,
        label: model.id,
        current:
          connectionId === normalizeProviderId(selection.connection_id) &&
          normalizeProviderId(model.id) === normalizeProviderId(selection.model_id),
        disabled:
          !connection.credential_ready || Boolean(input.activating),
        unsupported: false,
        unavailableCurrent: false,
        effectDefault: model.reasoning_effort_default?.trim() ?? "",
        effects,
        currentEffect:
          connectionId === normalizeProviderId(selection.connection_id) &&
          normalizeProviderId(model.id) === normalizeProviderId(selection.model_id)
            ? selection.reasoning_effort?.trim() || ""
            : "",
      });
    }
  }
  const currentShown = rows.some((row) => row.current);
  if (!currentShown && selection.connection_id && selection.model_id) {
    const currentConnection = snapshot.connections.find(
      (connection) => normalizeProviderId(connection.id) === currentConnectionId,
    );
    rows.unshift({
      key: `${selection.connection_id}:${selection.model_id}:current`,
      connectionId: selection.connection_id,
      modelId: selection.model_id,
      label: selection.model_id,
      current: true,
      disabled:
        currentConnection?.credential_ready !== true || Boolean(input.activating),
      unsupported: false,
      unavailableCurrent: true,
      effectDefault: selection.reasoning_effort_default?.trim() ?? "",
      effects: modelEffectChoices(client, [...(selection.reasoning_efforts ?? [])]),
      currentEffect:
        selection.reasoning_effort?.trim() || "",
    });
  }
  return rows;
}

export function runtimeChoiceForRow(
  row: ProviderPickerModelRow,
  effect?: string,
): ThreadRuntimeChoice | null {
  const selectedEffect = effect?.trim() ?? "";
  if (selectedEffect && !row.effects.includes(selectedEffect)) return null;
  return {
    connectionId: row.connectionId,
    modelId: row.modelId,
    ...(effect === ""
      ? { useDefaultEffect: true }
      : selectedEffect
        ? { effect: selectedEffect }
        : {}),
  };
}

export function resolveComposerModelControl(input: {
  capabilities?: AgentSessionCapabilities | null;
  connectionConnected: boolean;
  selection: ThreadRuntimeSelection | null;
  refreshRequired: boolean;
}): ComposerModelControlPresentation | null {
  if (!sessionAllowsModelProfileActivation(input.capabilities)) return null;
  if (!input.connectionConnected || input.refreshRequired) return null;
  const selection = input.selection;
  if (!selection || selection.hot_switchable !== true) return null;
  const label = selection.model_id.trim();
  if (!label) return null;
  return {
    label,
    accessibilityLabel: `Open model and effect, ${label}`,
  };
}

export function refetchFoundBindingNotSwitchable(input: {
  activationCapable: boolean;
  hotSwitchable: boolean;
}): boolean {
  return input.activationCapable && !input.hotSwitchable;
}

export function sessionEffortContract(
  selection: ThreadRuntimeSelection | null | undefined,
): SessionEffortContract | null {
  if (!selection) return null;
  const supported = selection.reasoning_efforts ?? [];
  const defaultEffort = selection.reasoning_effort_default?.trim() ?? "";
  if (supported.length === 0 || !defaultEffort) return null;
  const override = selection.reasoning_effort?.trim() ?? "";
  return {
    current: override || defaultEffort,
    override,
    defaultEffort,
    supported,
  };
}

export function reasoningEffortLabel(value: string): string {
  switch (value.trim().toLowerCase()) {
    case "":
      return "Default";
    case "minimal":
      return "Minimal";
    case "low":
      return "Low";
    case "medium":
      return "Medium";
    case "high":
      return "High";
    case "xhigh":
      return "XHigh";
    case "max":
      return "Max";
    default:
      return value.trim();
  }
}
