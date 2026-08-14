/**
 * Pure Composer model-picker helpers. The picker exists only for Sessions
 * whose current model switch is a daemon-acknowledged live route activation
 * (model_profile_managed + model_profile_active_switch + hot_switchable
 * selection). Every other Session — official-direct Codex/Claude logins,
 * OpenCode, Pi, managed read-only, capability mismatch — hides the control:
 * a dead picker is never rendered.
 *
 * Product boundary: Settings owns Provider selection; the Composer/Interface
 * Input owns only Model selection for the Provider selected in Settings.
 * Inventory is therefore the enabled+available model list of the preferred
 * Provider ONLY (the catalog client default) — never every saved Provider,
 * never Provider groups/names/hostnames, never a cross-Provider inventory.
 * Each selectable row carries the exact stable (connection_id, model_id)
 * pair from the catalog; activation never invents a generation, never
 * substitutes a model (no `gpt-5` fallback), and never touches other
 * Providers, other Sessions, or the preferred Provider itself.
 *
 * When the Session route still runs a different Provider than the preferred
 * one (a Settings switch whose model is unsupported, or a switch that never
 * activated), the Composer enters an explicit model-required state: sending
 * is blocked and every row stays unselected until the user picks a model,
 * which activates the exact preferred connection_id + model_id on the
 * current Session.
 */

import {
  sessionAllowsModelProfileActivation,
  type AgentSessionCapabilities,
} from "./sessionCapabilities";
import { providerClientForCommand } from "./presentation";
import type {
  ProviderModel,
  ProviderSessionSelection,
  ProvidersSnapshot,
} from "./types";
import {
  normalizeProviderClient,
  normalizeProviderId,
} from "./types";

/**
 * Concise Composer control presentation for the current Session selection.
 * The control exists only when this exact Session can safely activate
 * another Provider Model right now; every other state omits the control
 * entirely. The label is Model only — never a Provider name or hostname.
 */
export type ComposerModelControlPresentation = {
  /** Model id only (or the model-required request). */
  label: string;
  accessibilityLabel: string;
  /** True when a Settings switch is pending an explicit model choice. */
  modelRequired: boolean;
  /** The exact preferred Provider connection id the sheet must list. */
  preferredConnectionId: string;
};

/**
 * One selectable model under the preferred Provider. `modelId` is always the
 * exact stable catalog id — never truncated, aliased, or replaced by a
 * display label. `current` is true only for the exact (connection_id,
 * model_id) pair the Session is running on the preferred Provider.
 */
export type ProviderPickerModelRow = {
  /** Stable React key: connection id + canonical model id. */
  key: string;
  /** Stable preferred connection id from the catalog; never fabricated. */
  connectionId: string;
  /** Canonical model id from the catalog. */
  modelId: string;
  /** Display label; the row may truncate it visually, never by data. */
  label: string;
  /** True only for the Session's exact current pair on the preferred Provider. */
  current: boolean;
  /**
   * Honest non-selectability: the preferred Provider is uncredentialed, a
   * switch is in flight, or the running pair is no longer available for
   * activation. Never used to hide a row.
   */
  disabled: boolean;
  /**
   * The running pair whose model is no longer available for switching.
   * Rendered checked and non-selectable with a concise caption.
   */
  unavailableCurrent: boolean;
};

/**
 * The exact preferred Provider connection id for a Session client: the
 * catalog client default (Settings-selected). Empty when the client has no
 * Provider default (direct official login) — the Composer control must stay
 * hidden for those Sessions.
 */
export function preferredProviderConnectionId(
  snapshot: ProvidersSnapshot | null | undefined,
  client: string,
): string {
  const normalized = normalizeProviderClient(client);
  return normalizeProviderId(snapshot?.defaults[normalized]?.connection_id ?? "");
}

/**
 * Model-required truth for the current Session: the Session route still runs
 * a different Provider than the Settings-selected preferred Provider. In this
 * state the Composer lists only the preferred Provider's models, nothing is
 * checked, sending is blocked, and the user must pick a model to activate the
 * exact preferred connection_id + model_id. The daemon never falls back to
 * another model, so this state is the only honest representation of a pending
 * Provider switch.
 */
export function sessionModelRequired(input: {
  snapshot: ProvidersSnapshot | null | undefined;
  selection: ProviderSessionSelection | null | undefined;
}): boolean {
  const { snapshot, selection } = input;
  if (!snapshot || !selection) return false;
  const preferredId = preferredProviderConnectionId(snapshot, selection.client);
  if (!preferredId) return false;
  return preferredId !== normalizeProviderId(selection.connection_id);
}

/**
 * Whether a model is enabled+available on a connection per the synced
 * allowlist. Returns null when no synced allowlist exists yet (unknown — the
 * daemon admits the activation); false when a synced allowlist exists and
 * does not contain the model, or contains it disabled (never a fallback —
 * the caller must not activate and must not fabricate a model); true when the
 * model is present and available.
 */
export function modelSupportedOnConnection(
  snapshot: ProvidersSnapshot | null | undefined,
  connectionId: string,
  modelId: string,
): boolean | null {
  const normalizedId = normalizeProviderId(connectionId);
  const normalizedModel = normalizeProviderId(modelId);
  const models = snapshot?.models[normalizedId] ?? [];
  if (models.length === 0) return null;
  const entry = models.find(
    (model) => normalizeProviderId(model.id) === normalizedModel,
  );
  if (!entry) return false;
  return entry.available;
}

/**
 * Inventory for the Composer sheet: the enabled+available models of the
 * Settings-selected (preferred) Provider only, in catalog order. No other
 * Provider, group header, name or hostname ever appears. When the Session
 * route is on the preferred Provider, the running pair is checked; in the
 * model-required state nothing is checked (the route runs another Provider)
 * and every available row stays selectable.
 */
export function sessionModelSheetRows(input: {
  snapshot: ProvidersSnapshot | null | undefined;
  selection: ProviderSessionSelection | null | undefined;
  activating?: boolean;
}): ProviderPickerModelRow[] {
  const { snapshot, selection } = input;
  if (!snapshot || !selection) return [];
  const client = normalizeProviderClient(selection.client);
  const preferredId = preferredProviderConnectionId(snapshot, client);
  if (!preferredId) return [];
  const connection = snapshot.connections.find(
    (candidate) => normalizeProviderId(candidate.id) === preferredId,
  );
  const credentialReady = connection?.credential_ready ?? false;
  const modelRequired =
    preferredId !== normalizeProviderId(selection.connection_id);
  const routeOnPreferred =
    !modelRequired &&
    normalizeProviderId(selection.connection_id) === preferredId;
  const runningModelId = normalizeProviderId(selection.model_id);
  const rows: ProviderPickerModelRow[] = [];
  for (const model of snapshot.models[preferredId] ?? []) {
    if (!model.available) continue;
    const current =
      routeOnPreferred &&
      runningModelId !== "" &&
      normalizeProviderId(model.id) === runningModelId;
    rows.push({
      key: `${preferredId}:${model.id}`,
      connectionId: preferredId,
      modelId: model.id,
      label: model.id,
      current,
      disabled: !credentialReady || Boolean(input.activating),
      unavailableCurrent: false,
    });
  }
  // The running pair is always visible on the preferred Provider: when the
  // model is missing from or disabled in discovery, show it checked and
  // non-selectable instead of silently substituting another model.
  const currentShown = rows.some((row) => row.current);
  if (routeOnPreferred && runningModelId !== "" && !currentShown) {
    rows.push({
      key: `${preferredId}:${runningModelId}:current`,
      connectionId: preferredId,
      modelId: runningModelId,
      label: runningModelId,
      current: true,
      disabled: true,
      unavailableCurrent: true,
    });
  }
  return rows;
}

/**
 * Refetch admission for an open sheet: a Session whose binding no longer
 * admits live switching must close the sheet and hide the control instead of
 * presenting an empty fabricated inventory.
 */
export function refetchFoundBindingNotSwitchable(input: {
  activationCapable: boolean;
  hotSwitchable: boolean;
}): boolean {
  return input.activationCapable && !input.hotSwitchable;
}

/**
 * Resolves the exact activation target from the catalog before any request is
 * sent. Returns null (activation refused, old route retained) unless the
 * catalog admits this exact (connection_id, model_id) pair as available —
 * the same admission the daemon enforces against the target connection's
 * support allowlist. A model is never substituted with another id.
 */
export function activationTargetModel(
  catalog: ProvidersSnapshot | null | undefined,
  choice: { connectionId: string; modelId: string },
): ProviderModel | null {
  const connectionId = normalizeProviderId(choice.connectionId);
  const modelId = normalizeProviderId(choice.modelId);
  if (!catalog || !connectionId || !modelId) return null;
  const model = (catalog.models[connectionId] ?? []).find(
    (item) => normalizeProviderId(item.id) === modelId,
  );
  if (!model || !model.available) return null;
  return model;
}

export function resolveComposerModelControl(input: {
  capabilities?: AgentSessionCapabilities | null;
  connectionConnected: boolean;
  selection: ProviderSessionSelection | null;
  refreshRequired: boolean;
  /** Exact preferred Provider connection id (catalog client default). */
  preferredConnectionId: string;
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
  const modelLabel = selection.model_id.trim();
  const preferredId = normalizeProviderId(input.preferredConnectionId);
  if (!preferredId) {
    return null;
  }
  const modelRequired =
    preferredId !== normalizeProviderId(selection.connection_id);
  const label = modelRequired ? "Choose model" : modelLabel || "Choose model";
  if (!label) {
    return null;
  }
  return {
    label,
    accessibilityLabel: modelRequired
      ? "Choose a model. Sending is paused until a model is selected."
      : `Open model selection, ${modelLabel}`,
    modelRequired,
    preferredConnectionId: preferredId,
  };
}

/**
 * The current compatible routed Session for a Settings Provider switch: the
 * focused Session on the current server whose client matches the Provider
 * card and whose daemon-acknowledged capabilities admit live activation.
 * Returns null when no such Session is current — the switch then only
 * persists the preferred Provider (model-required until a model is chosen).
 */
export function currentSessionForClient(input: {
  agents: ReadonlyArray<{
    id: string;
    serverId: string;
    command?: string;
    capabilities?: AgentSessionCapabilities | null;
  }>;
  currentSession: { serverId: string; agentId: string } | null;
  client: string;
}): { agentId: string } | null {
  const current = input.currentSession;
  if (!current || !current.agentId || !current.serverId) return null;
  const agent = input.agents.find(
    (candidate) =>
      candidate.id === current.agentId && candidate.serverId === current.serverId,
  );
  if (!agent) return null;
  if (!sessionAllowsModelProfileActivation(agent.capabilities)) return null;
  const normalizedClient = normalizeProviderClient(input.client);
  if (providerClientForCommand(agent.command ?? "") !== normalizedClient) {
    return null;
  }
  return { agentId: agent.id };
}

export type ActivateSessionProviderRequest = {
  agentId: string;
  connectionId: string;
  modelId: string;
};

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
