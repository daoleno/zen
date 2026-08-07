import { invalidProviderReply } from "./errors";
import type {
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
): ProviderConnectionInput {
  if (preset.advanced) {
    throw invalidProviderReply("Advanced Providers require Gateway details.");
  }
  return { preset_id: preset.id };
}

export function advancedConnectionInput(input: {
  existingId?: string;
  name: string;
  client: string;
  baseUrl: string;
  manualModelId?: string;
  presetId?: string;
}): ProviderConnectionInput {
  const name = normalizeProviderId(input.name);
  const client = normalizeProviderClient(input.client);
  const baseUrl = normalizeProviderId(input.baseUrl);
  const modelId = normalizeProviderId(input.manualModelId);
  if (!name) throw invalidProviderReply("Display name is required.");
  if (!isSupportedProviderClient(client)) {
    throw invalidProviderReply("Choose Codex or Claude.");
  }
  if (!baseUrl) throw invalidProviderReply("Base URL is required.");
  let parsed: URL;
  try {
    parsed = new URL(baseUrl);
  } catch {
    throw invalidProviderReply("Enter a valid HTTPS base URL.");
  }
  if (parsed.protocol !== "https:" && parsed.protocol !== "http:") {
    throw invalidProviderReply("Base URL must use HTTP or HTTPS.");
  }
  return {
    id: normalizeProviderId(input.existingId) || undefined,
    name,
    preset_id: normalizeProviderId(input.presetId) || "custom",
    client,
    base_url: baseUrl,
    model_id: modelId || undefined,
    advanced: true,
  };
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
  const out: ProviderConnectionInput = { preset_id: presetId };
  const id = normalizeProviderId(input.id);
  const name = normalizeProviderId(input.name);
  const client = normalizeProviderClient(input.client);
  const baseUrl = normalizeProviderId(input.base_url);
  const modelId = normalizeProviderId(input.model_id);
  if (id) out.id = id;
  if (name) out.name = name;
  if (client) {
    if (!isSupportedProviderClient(client)) {
      throw invalidProviderReply("Choose Codex or Claude.");
    }
    out.client = client;
  }
  if (baseUrl) out.base_url = baseUrl;
  if (modelId) out.model_id = modelId;
  if (input.advanced === true) out.advanced = true;
  return out;
}
