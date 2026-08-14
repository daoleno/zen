/**
 * Pure Composer model-picker helpers. The picker exists only for Sessions
 * whose current model switch is a daemon-acknowledged live route activation
 * (model_profile_managed + model_profile_active_switch + hot_switchable
 * selection). Every other Session — official-direct Codex/Claude logins,
 * OpenCode, Pi, managed read-only, capability mismatch — hides the control:
 * a dead picker is never rendered.
 *
 * Inventory is truthful per-Session: every saved Provider connection
 * compatible with the Session client is offered, grouped by Provider Name,
 * with that Provider's enabled+available models beneath it. Each selectable
 * row carries the exact stable (connection_id, model_id) pair from the
 * catalog; activation never invents a generation, never substitutes a model
 * (no `gpt-5` fallback), and never touches Provider defaults or other
 * Sessions. Uncredentialed, failed-discovery, unsupported and unavailable
 * states stay honest and non-selectable.
 */

import {
  sessionAllowsModelProfileActivation,
  type AgentSessionCapabilities,
} from "./sessionCapabilities";
import {
  connectionsForSession,
  providerBaseUrlHostname,
} from "./presentation";
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
 * entirely.
 */
export type ComposerModelControlPresentation = {
  label: string;
  accessibilityLabel: string;
};

/**
 * One selectable model under a Provider group. `modelId` is always the
 * exact stable catalog id — never truncated, aliased, or replaced by a
 * display label. `current` is true only for the exact (connection_id,
 * model_id) pair the Session is running.
 */
export type ProviderPickerModelRow = {
  /** Stable React key: connection id + canonical model id. */
  key: string;
  /** Stable connection id from the catalog; never fabricated. */
  connectionId: string;
  /** Canonical model id from the catalog. */
  modelId: string;
  /** Display label; the row may truncate it visually, never by data. */
  label: string;
  /** True only for the Session's exact current (connection_id, model_id). */
  current: boolean;
  /**
   * Honest non-selectability: the Provider is uncredentialed or the model
   * is no longer available for activation. Never used to hide a row.
   */
  disabled: boolean;
  /**
   * The running pair whose model is no longer available for switching.
   * Rendered checked and non-selectable with a concise caption.
   */
  unavailableCurrent: boolean;
};

/**
 * One Provider group in the Session picker: a saved Provider connection
 * compatible with the Session client, with its enabled+available models
 * beneath it. The group exists even when nothing is selectable, so
 * uncredentialed and failed-discovery states stay visible and honest.
 */
export type ProviderPickerGroup = {
  /** Stable React key: connection id (or selection-scoped when unbound). */
  key: string;
  connectionId: string;
  /** Provider display name (editable, unique); falls back to selection. */
  connectionName: string;
  /** Base-URL hostname as secondary identity for advanced/custom rows. */
  hostname: string | null;
  credentialReady: boolean;
  models: ProviderPickerModelRow[];
};

/**
 * Inventory for the Session picker: every saved Provider connection
 * compatible with the Session client, grouped by Provider Name (hostname
 * secondary when the connection carries a Base URL), each with its
 * enabled+available models. Groups are sorted by Provider Name so the
 * surface is deterministic. When the Session's live route binding references
 * a connection no longer in the catalog (binding survives connection
 * deletion), that Provider still appears from the selection itself with the
 * running pair checked — never silently replaced.
 */
export function sessionProviderPickerGroups(
  snapshot: ProvidersSnapshot | null | undefined,
  selection: ProviderSessionSelection | null | undefined,
): ProviderPickerGroup[] {
  if (!snapshot || !selection) return [];
  const groups = connectionsForSession(snapshot, selection).map((connection) =>
    buildPickerGroup(connection, snapshot, selection),
  );
  const boundId = normalizeProviderId(selection.connection_id);
  const boundStillRouted =
    boundId !== "" &&
    !snapshot.connections.some(
      (connection) => normalizeProviderId(connection.id) === boundId,
    );
  if (boundStillRouted) {
    groups.push(buildUnboundCurrentGroup(selection));
  }
  return groups.sort(compareGroups);
}

function buildPickerGroup(
  connection: ProvidersSnapshot["connections"][number],
  snapshot: ProvidersSnapshot,
  selection: ProviderSessionSelection,
): ProviderPickerGroup {
  const credentialReady = connection.credential_ready;
  const models: ProviderPickerModelRow[] = (snapshot.models[connection.id] ?? [])
    .filter((model) => model.available)
    .map((model) => ({
      key: `${connection.id}:${model.id}`,
      connectionId: connection.id,
      modelId: model.id,
      label: model.id,
      current:
        selection.connection_id === connection.id &&
        selection.model_id === model.id,
      disabled: !credentialReady,
      unavailableCurrent: false,
    }));
  // The running pair is always visible: when the model is missing from or
  // disabled in discovery, show it checked and non-selectable instead of
  // silently substituting another model.
  const runningModelId = normalizeProviderId(selection.model_id);
  const runningHere =
    selection.connection_id === connection.id && runningModelId !== "";
  const currentShown = models.some((model) => model.current);
  if (runningHere && !currentShown) {
    models.push({
      key: `${connection.id}:${runningModelId}:current`,
      connectionId: connection.id,
      modelId: runningModelId,
      label: runningModelId,
      current: true,
      disabled: true,
      unavailableCurrent: true,
    });
  }
  return {
    key: connection.id,
    connectionId: connection.id,
    connectionName:
      connection.name.trim() ||
      selection.connection_name.trim() ||
      connection.id,
    hostname:
      connection.advanced && connection.base_url
        ? providerBaseUrlHostname(connection.base_url)
        : null,
    credentialReady,
    models,
  };
}

function buildUnboundCurrentGroup(
  selection: ProviderSessionSelection,
): ProviderPickerGroup {
  const runningModelId = normalizeProviderId(selection.model_id);
  const connectionName =
    selection.connection_name.trim() || selection.connection_id;
  return {
    key: `current:${selection.connection_id}`,
    connectionId: selection.connection_id,
    connectionName,
    hostname: null,
    credentialReady: selection.credential_ready,
    models:
      runningModelId === ""
        ? []
        : [
            {
              key: `${selection.connection_id}:${runningModelId}:current`,
              connectionId: selection.connection_id,
              modelId: runningModelId,
              label: runningModelId,
              current: true,
              disabled: true,
              // The binding outlives the catalog row, so this model can never
              // be re-admitted for switching; it is shown checked and honest.
              unavailableCurrent: true,
            },
          ],
  };
}

function compareGroups(a: ProviderPickerGroup, b: ProviderPickerGroup): number {
  if (a.connectionName === b.connectionName) {
    return a.connectionId < b.connectionId ? -1 : a.connectionId > b.connectionId ? 1 : 0;
  }
  return a.connectionName < b.connectionName ? -1 : 1;
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
  const providerLabel = selection.connection_name.trim();
  const label =
    modelLabel && providerLabel
      ? `${providerLabel} · ${modelLabel}`
      : modelLabel || providerLabel;
  if (!label) {
    return null;
  }
  return {
    label,
    // Provider identity stays explicit so same-host/different-key
    // connections are distinguishable to assistive tech too.
    accessibilityLabel: modelLabel
      ? `Open model selection, ${modelLabel}, ${providerLabel}`
      : `Open model selection, ${providerLabel}`,
  };
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
