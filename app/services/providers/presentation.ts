import { invalidProviderReply } from "./errors";
import type {
  ProviderClient,
  ProviderConnection,
  ProviderConnectionInput,
  ProviderModel,
  ProviderPreset,
  ProviderSessionSelection,
  ProvidersSnapshot,
} from "./types";
import {
  isSupportedProviderClient,
  normalizeProviderClient,
  normalizeProviderId,
} from "./types";

export type ProviderModelChoice = {
  connection: ProviderConnection;
  model: ProviderModel;
  current: boolean;
  disabled: boolean;
};

export function connectionsForSession(
  snapshot: ProvidersSnapshot | null | undefined,
  selection: ProviderSessionSelection | null | undefined,
): ProviderConnection[] {
  if (!snapshot || !selection) return [];
  const client = normalizeProviderClient(selection.client);
  return snapshot.connections.filter((connection) =>
    connection.clients.some(
      (candidate) => normalizeProviderClient(candidate) === client,
    ),
  );
}

export function connectionsForClient(
  snapshot: ProvidersSnapshot,
  client: string,
): ProviderConnection[] {
  const normalized = normalizeProviderClient(client);
  return snapshot.connections.filter((connection) =>
    connection.clients.some(
      (candidate) => normalizeProviderClient(candidate) === normalized,
    ),
  );
}

export function availableModelsForConnection(
  snapshot: ProvidersSnapshot,
  connectionId: string,
): ProviderModel[] {
  return (snapshot.models[connectionId] ?? []).filter(
    (model) => model.available,
  );
}

/**
 * The single client a connection serves. Curated and custom connections are
 * always created client-scoped, so a missing/unsupported client means the
 * projection is malformed.
 */
export function clientForConnection(
  connection: ProviderConnection | null | undefined,
): ProviderClient | null {
  if (!connection) return null;
  const client = connection.clients[0];
  return isSupportedProviderClient(client) ? client : null;
}

/**
 * The client the launch command targets, when it is a managed Provider client.
 * Shells and other executors return null (no Provider selection applies).
 */
export function providerClientForCommand(command: string): ProviderClient | null {
  const fields = command.trim().split(/\s+/).filter(Boolean);
  for (const field of fields) {
    if (field.includes("=")) continue;
    const bin = field.split("/").pop() ?? "";
    if (normalizeProviderClient(bin) === "codex") return "codex";
    if (normalizeProviderClient(bin) === "claude") return "claude";
  }
  return null;
}

/**
 * The authoritative launch selection for a new Session: the client's default
 * connection plus its client-selected model. Null when the client has no
 * Provider connection (direct official login) or the snapshot is missing.
 * The daemon resolves a deterministic supported-model fallback when the
 * selected model is no longer supported.
 */
export function launchSelectionFromSnapshot(
  snapshot: ProvidersSnapshot | null | undefined,
  client: ProviderClient | null,
): { connectionId: string; modelId: string } | null {
  if (!snapshot || !client) return null;
  const entry = snapshot.defaults[client];
  if (!entry?.connection_id) return null;
  const connection = snapshot.connections.find(
    (item) => item.id === entry.connection_id,
  );
  if (!connection) return null;
  return {
    connectionId: connection.id,
    modelId: entry.model_id?.trim() || "",
  };
}

/**
 * The client-selected model bound to a connection via the client default, or
 * null when the connection is not the client default or no model is selected
 * yet. This is the single source the UI shows under a connection row; the
 * client (never the gateway) owns this selection for new Sessions.
 */
export function boundModelForConnection(
  snapshot: ProvidersSnapshot | null | undefined,
  client: string,
  connectionId: string,
): string | null {
  const normalizedClient = normalizeProviderClient(client);
  const defaultEntry = snapshot?.defaults[normalizedClient];
  if (!defaultEntry) return null;
  if (
    normalizeProviderId(defaultEntry.connection_id) !==
    normalizeProviderId(connectionId)
  ) {
    return null;
  }
  const modelId = normalizeProviderId(defaultEntry.model_id);
  return modelId || null;
}

/**
 * True when a connection is the client default but has no upstream model
 * bound yet — the exact state that makes `codex new` fail closed. The row
 * renders a "sync models" hint instead of a model name in this state.
 */
export function connectionRequiresModelSelection(
  snapshot: ProvidersSnapshot | null | undefined,
  client: string,
  connectionId: string,
): boolean {
  const normalizedClient = normalizeProviderClient(client);
  const defaultEntry = snapshot?.defaults[normalizedClient];
  if (!defaultEntry) return false;
  if (
    normalizeProviderId(defaultEntry.connection_id) !==
    normalizeProviderId(connectionId)
  ) {
    return false;
  }
  return !normalizeProviderId(defaultEntry.model_id);
}

/**
 * Picker inventory for one connection after discovery: every discovered model
 * as a compact support chip. "Selected" means the gateway exposes the model
 * (the client enable allowlist); tapping toggles support. There is no
 * default-model concept: the gateway never owns a default model.
 */
export function modelSupportChoices(
  snapshot: ProvidersSnapshot | null | undefined,
  connection: ProviderConnection,
  models: ProviderModel[],
): ProviderModelChoice[] {
  void snapshot;
  return models.map((model) => ({
    connection,
    model,
    current: model.available,
    disabled: false,
  }));
}

/**
 * The client-side model support allowlist of a connection as enabled ids in
 * catalog order (availability is the wire's enabled state).
 */
export function enabledModelIds(
  snapshot: ProvidersSnapshot | null | undefined,
  connectionId: string,
): string[] {
  return (snapshot?.models[connectionId] ?? [])
    .filter((model) => model.available)
    .map((model) => model.id);
}

/**
 * Toggle one model's support: returns the next full enabled set (catalog
 * order) after flipping modelId, preserving every other model's state.
 */
export function toggleModelSupport(
  snapshot: ProvidersSnapshot | null | undefined,
  connectionId: string,
  modelId: string,
): string[] {
  const current = new Set(enabledModelIds(snapshot, connectionId));
  const normalized = normalizeProviderId(modelId);
  if (!normalized) return enabledModelIds(snapshot, connectionId);
  if (current.has(normalized)) {
    current.delete(normalized);
  } else {
    current.add(normalized);
  }
  return (snapshot?.models[connectionId] ?? [])
    .map((model) => model.id)
    .filter((id) => current.has(id));
}

/**
 * Deterministic visible fallback / initial selection: the first supported
 * model of a connection in catalog order, or null when none exists.
 */
export function firstSupportedModel(
  snapshot: ProvidersSnapshot | null | undefined,
  connectionId: string,
): string | null {
  for (const model of snapshot?.models[connectionId] ?? []) {
    if (model.available) return model.id;
  }
  return null;
}

export function connectionIsFutureDefault(
  snapshot: ProvidersSnapshot,
  connectionId: string,
): string[] {
  return Object.entries(snapshot.defaults)
    .filter(([, value]) => value.connection_id === connectionId)
    .map(([client]) => client);
}

export function modelChoicesForSession(
  snapshot: ProvidersSnapshot | null | undefined,
  selection: ProviderSessionSelection | null | undefined,
): ProviderModelChoice[] {
  if (!snapshot || !selection) return [];
  return connectionsForSession(snapshot, selection).flatMap((connection) =>
    (snapshot.models[connection.id] ?? [])
      .filter((model) => model.available)
      .map((model) => ({
        connection,
        model,
        current:
          selection.connection_id === connection.id &&
          selection.model_id === model.id,
        disabled: !connection.credential_ready,
      })),
  );
}

export function defaultClientsForConnection(
  snapshot: ProvidersSnapshot,
  connectionId: string,
): string[] {
  return Object.entries(snapshot.defaults)
    .filter(([, value]) => value.connection_id === connectionId)
    .map(([client]) => client);
}

export type FutureDefaultOption = {
  connectionId: string;
  connectionName: string;
  selected: boolean;
};

export type FutureDefaultClientRow = {
  client: string;
  label: string;
  currentConnectionId: string | null;
  currentConnectionName: string | null;
  options: FutureDefaultOption[];
};

function futureDefaultClientLabel(client: string): string {
  const normalized = normalizeProviderClient(client);
  if (normalized === "codex") return "Codex";
  if (normalized === "claude") return "Claude";
  return client;
}

/**
 * One compact Defaults surface: Codex and Claude each select among ready,
 * client-compatible Provider connections. Cards stay glance-only.
 */
export function futureDefaultRows(
  snapshot: ProvidersSnapshot | null | undefined,
  clients: readonly string[] = ["codex", "claude"],
): FutureDefaultClientRow[] {
  if (!snapshot) return [];
  return clients.map((client) => {
    const normalized = normalizeProviderClient(client) || client.trim().toLowerCase();
    const currentId = snapshot.defaults[normalized]?.connection_id ?? null;
    const currentConnection = currentId
      ? snapshot.connections.find((connection) => connection.id === currentId)
      : undefined;
    const options = connectionsForClient(snapshot, normalized)
      .filter((connection) => connection.credential_ready)
      .map((connection) => ({
        connectionId: connection.id,
        connectionName: connection.name,
        selected: connection.id === currentId,
      }));
    return {
      client: normalized,
      label: futureDefaultClientLabel(normalized),
      currentConnectionId: currentId,
      currentConnectionName: currentConnection?.name ?? null,
      options,
    };
  });
}

export function curatedConnectionInput(
  preset: ProviderPreset,
  client: string,
): ProviderConnectionInput {
  if (preset.advanced) {
    throw invalidProviderReply("Advanced Providers require Gateway details.");
  }
  const normalizedClient = normalizeProviderClient(client);
  if (
    !isSupportedProviderClient(normalizedClient) ||
    !preset.clients.includes(normalizedClient)
  ) {
    throw invalidProviderReply("Choose a supported client.");
  }
  return { preset_id: preset.id, client: normalizedClient };
}

/**
 * Advanced/Custom create input. The App sends the selected product client and
 * gateway identity; Zen derives protocol/auth compatibility internally.
 */
export function advancedConnectionInput(input: {
  existingId?: string;
  name: string;
  client: string;
  baseUrl: string;
  presetId?: string;
  /** Optional explicit upstream model id (discovery-driven when omitted). */
  modelId?: string;
  /**
   * Advanced/custom gateways expose an editable Base URL. Curated edits stay
   * false so the daemon keeps the official endpoint; defaults to true for new
   * custom endpoints.
   */
  advanced?: boolean;
}): ProviderConnectionInput {
  const name = normalizeProviderId(input.name);
  const baseUrl = normalizeProviderId(input.baseUrl);
  const client = normalizeProviderClient(input.client);
  const advanced = input.advanced ?? true;
  if (!name) throw invalidProviderReply("Display name is required.");
  if (!isSupportedProviderClient(client)) {
    throw invalidProviderReply("Choose Codex or Claude Code.");
  }
  if (advanced) {
    if (!baseUrl) throw invalidProviderReply("Base URL is required.");
    let parsed: URL;
    try {
      parsed = new URL(baseUrl);
    } catch {
      throw invalidProviderReply("Enter a valid HTTP or HTTPS base URL.");
    }
    if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
      throw invalidProviderReply("Base URL must use HTTP or HTTPS.");
    }
  }
  return {
    id: normalizeProviderId(input.existingId) || undefined,
    name,
    preset_id: normalizeProviderId(input.presetId) || "custom",
    client,
    base_url: baseUrl || undefined,
    model_id: normalizeProviderId(input.modelId) || undefined,
    advanced,
  };
}

/**
 * Hostname-only secondary identity for Provider rows: the Base URL is a
 * routing attribute, never the display identity. Non-URL text falls back to
 * the trimmed input so malformed legacy data still renders safely.
 */
export function providerBaseUrlHostname(baseUrl: string): string {
  const normalized = normalizeProviderId(baseUrl);
  if (!normalized) return "";
  try {
    const parsed = new URL(normalized);
    return parsed.hostname || normalized;
  } catch {
    return normalized;
  }
}

/**
 * Inline display-name validation for the unified Add/Edit Provider form:
 * trimmed, non-empty, length-bounded and case-insensitively unique within the
 * current Provider list. Returns a user-facing message or null when the name
 * is acceptable. The daemon re-validates authoritatively on save.
 */
export function providerNameIssue(input: {
  name: string;
  snapshot: ProvidersSnapshot | null | undefined;
  exceptConnectionId?: string;
}): string | null {
  const name = normalizeProviderId(input.name);
  if (!name) return "Provider name is required.";
  if (name.length > 64) return "Provider name must be 64 characters or fewer.";
  if (input.snapshot) {
    const except = normalizeProviderId(input.exceptConnectionId);
    const clash = input.snapshot.connections.find(
      (connection) =>
        connection.id !== except &&
        connection.name.trim().toLowerCase() === name.toLowerCase(),
    );
    if (clash) {
      return `Another Provider is already named “${clash.name}”. Names must be unique.`;
    }
  }
  return null;
}

/**
 * A create reply must identify exactly one new connection before a credential
 * can be addressed. Never guess between same-preset connections.
 */
export function createdConnectionFromMutation(
  previous: ProvidersSnapshot,
  next: ProvidersSnapshot,
  presetId: string,
): ProviderConnection {
  const previousIds = new Set(previous.connections.map((item) => item.id));
  const added = next.connections.filter(
    (connection) =>
      !previousIds.has(connection.id) && connection.preset_id === presetId,
  );
  if (added.length !== 1) {
    throw invalidProviderReply(
      "Could not uniquely identify the new Provider. Refresh before adding an API key.",
    );
  }
  return added[0];
}

export function withProviderCredentialReadiness(
  snapshot: ProvidersSnapshot,
  connectionId: string,
  credentialReady: boolean,
): ProvidersSnapshot {
  return {
    ...snapshot,
    connections: snapshot.connections.map((connection) =>
      connection.id === connectionId
        ? { ...connection, credential_ready: credentialReady }
        : connection,
    ),
  };
}

export function withDiscoveredProviderModels(
  snapshot: ProvidersSnapshot,
  connectionId: string,
  models: ProviderModel[],
): ProvidersSnapshot {
  return {
    ...snapshot,
    models: { ...snapshot.models, [connectionId]: models },
  };
}

export function sanitizeProviderConnectionInput(
  input: ProviderConnectionInput,
): ProviderConnectionInput {
  const presetId = normalizeProviderId(input.preset_id);
  if (!presetId) {
    throw invalidProviderReply("Choose a Provider preset.");
  }
  const id = normalizeProviderId(input.id);
  const name = normalizeProviderId(input.name);
  const client = normalizeProviderClient(input.client);
  const baseUrl = normalizeProviderId(input.base_url);
  const modelId = normalizeProviderId(input.model_id);
  if (!isSupportedProviderClient(client)) {
    throw invalidProviderReply("Choose Codex or Claude Code.");
  }
  const out: ProviderConnectionInput = { preset_id: presetId, client };
  if (id) out.id = id;
  if (name) out.name = name;
  if (baseUrl) out.base_url = baseUrl;
  if (modelId) out.model_id = modelId;
  if (input.advanced === true) out.advanced = true;
  return out;
}
