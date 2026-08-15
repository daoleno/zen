import { invalidProviderReply } from "./errors";
import {
  isSupportedProviderClient,
  normalizeProviderClient,
  normalizeProviderId,
  type ProviderConnection,
  type ProviderConnectionTestResult,
  type ProviderCredentialResult,
  type ProviderDefault,
  type ProviderModel,
  type ProviderModelsResult,
  type ProviderPreset,
  type ThreadRuntimeSelection,
  type ProvidersSnapshot,
} from "./types";
import { requireAppliedPersistence } from "./persistence";

const FORBIDDEN_REPLY_KEYS = new Set([
  "credential",
  "credential_value",
  "api_key",
  "apikey",
  "access_token",
  "secret",
]);

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

function asString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function asStringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const out: string[] = [];
  for (const item of value) {
    const text = typeof item === "string" ? item.trim() : "";
    if (text && !out.includes(text)) out.push(text);
  }
  return out.length > 0 ? out : undefined;
}

function asRevision(value: unknown): number | null {
  return typeof value === "number" &&
    Number.isInteger(value) &&
    Number.isFinite(value) &&
    value >= 0
    ? value
    : null;
}

function assertSecretFree(value: unknown, seen = new Set<unknown>()): void {
  if (!value || typeof value !== "object" || seen.has(value)) return;
  seen.add(value);
  if (Array.isArray(value)) {
    for (const item of value) assertSecretFree(item, seen);
    return;
  }
  for (const [key, nested] of Object.entries(
    value as Record<string, unknown>,
  )) {
    if (FORBIDDEN_REPLY_KEYS.has(key.trim().toLowerCase())) {
      throw invalidProviderReply(
        "Daemon returned a credential-bearing Provider reply.",
      );
    }
    assertSecretFree(nested, seen);
  }
}

function parseClients(value: unknown): string[] | null {
  if (!Array.isArray(value)) return [];
  const clients: string[] = [];
  for (const raw of value) {
    if (typeof raw !== "string") return null;
    const client = normalizeProviderClient(raw);
    if (!isSupportedProviderClient(client)) return null;
    if (!clients.includes(client)) clients.push(client);
  }
  return clients;
}

export function parseProviderConnection(
  raw: unknown,
): ProviderConnection | null {
  const record = asRecord(raw);
  if (!record) return null;
  assertSecretFree(record);
  const id = asString(record.id);
  const name = asString(record.name);
  const clients = parseClients(record.clients);
  if (
    !id ||
    !name ||
    !clients ||
    clients.length !== 1 ||
    typeof record.credential_ready !== "boolean"
  ) {
    return null;
  }
  const advanced = record.advanced === true;
  return {
    id,
    name,
    preset_id: asString(record.preset_id) || undefined,
    clients,
    credential_ready: record.credential_ready,
    advanced,
    // Do not retain advanced fields from a malformed curated projection.
    base_url: advanced ? asString(record.base_url) || undefined : undefined,
    manual_model_id: advanced
      ? asString(record.manual_model_id) || undefined
      : undefined,
    // Masked hint only: the daemon never projects the full stored secret, and
    // the hint is presentation-only (never submitted as a credential).
    credential_hint: asString(record.credential_hint) || undefined,
  };
}

export function parseProviderPreset(raw: unknown): ProviderPreset | null {
  const record = asRecord(raw);
  if (!record) return null;
  assertSecretFree(record);
  const id = asString(record.id);
  const label = asString(record.label);
  const clients = parseClients(record.clients);
  if (!id || !label || !clients || clients.length === 0) return null;
  return {
    id,
    label,
    clients,
    advanced: record.advanced === true,
  };
}

export function parseProviderModel(raw: unknown): ProviderModel | null {
  const record = asRecord(raw);
  if (!record) return null;
  assertSecretFree(record);
  const id = asString(record.id);
  const source = asString(record.source);
  if (!id || !source || typeof record.available !== "boolean") return null;
  return {
    id,
    source,
    available: record.available,
    known: record.known === true ? true : undefined,
    reasoning_effort_default:
      asString(record.reasoning_effort_default) || undefined,
    reasoning_efforts: asStringArray(record.reasoning_efforts),
  };
}

export function parseProvidersSnapshot(raw: unknown): ProvidersSnapshot | null {
  const record = asRecord(raw);
  if (!record) return null;
  assertSecretFree(record);
  const revision = asRevision(record.revision);
  if (revision == null || !Array.isArray(record.connections)) return null;

  const connections: ProviderConnection[] = [];
  const connectionIds = new Set<string>();
  for (const item of record.connections) {
    const connection = parseProviderConnection(item);
    if (!connection || connectionIds.has(connection.id)) return null;
    connectionIds.add(connection.id);
    connections.push(connection);
  }

  if (!Array.isArray(record.presets)) return null;
  const presets: ProviderPreset[] = [];
  const presetIds = new Set<string>();
  for (const item of record.presets) {
    const preset = parseProviderPreset(item);
    if (!preset || presetIds.has(preset.id)) return null;
    presetIds.add(preset.id);
    presets.push(preset);
  }

  const defaultsRecord = asRecord(record.defaults);
  if (!defaultsRecord) return null;
  const defaults: Record<string, ProviderDefault> = {};
  for (const [rawClient, rawDefault] of Object.entries(defaultsRecord)) {
    const client = normalizeProviderClient(rawClient);
    const entry = asRecord(rawDefault);
    const connectionId = asString(entry?.connection_id);
    const modelId = asString(entry?.model_id);
    if (
      !isSupportedProviderClient(client) ||
      !connectionId ||
      !modelId ||
      !connectionIds.has(connectionId)
    ) {
      return null;
    }
    const connection = connections.find((item) => item.id === connectionId);
    if (!connection?.clients.includes(client)) return null;
    defaults[client] = {
      connection_id: connectionId,
      model_id: modelId,
    };
  }

  const modelsRecord = asRecord(record.models);
  if (!modelsRecord) return null;
  const models: Record<string, ProviderModel[]> = {};
  for (const [connectionId, rawModels] of Object.entries(modelsRecord)) {
    if (!connectionIds.has(connectionId) || !Array.isArray(rawModels)) {
      return null;
    }
    const entries: ProviderModel[] = [];
    const modelIds = new Set<string>();
    for (const rawModel of rawModels) {
      const model = parseProviderModel(rawModel);
      if (!model || modelIds.has(model.id)) return null;
      modelIds.add(model.id);
      entries.push(model);
    }
    models[connectionId] = entries;
  }
  for (const id of connectionIds) {
    models[id] ??= [];
  }

  return { revision, connections, defaults, presets, models };
}

export function parseThreadRuntimeSelection(
  raw: unknown,
  expectedAgentId?: string,
): ThreadRuntimeSelection | null {
  const record = asRecord(raw);
  if (!record) return null;
  assertSecretFree(record);
  const selection: ThreadRuntimeSelection = {
    session_id: asString(record.session_id),
    client: normalizeProviderClient(asString(record.client)),
    connection_id: asString(record.connection_id),
    connection_name: asString(record.connection_name),
    provider_label: asString(record.provider_label) || undefined,
    model_id: asString(record.model_id),
    reasoning_effort: asString(record.reasoning_effort) || undefined,
    reasoning_effort_default: asString(record.reasoning_effort_default) || undefined,
    reasoning_efforts: asStringArray(record.reasoning_efforts),
    credential_ready: record.credential_ready === true,
    hot_switchable: record.hot_switchable === true,
  };
  if (
    !selection.session_id ||
    !isSupportedProviderClient(selection.client) ||
    !selection.connection_id ||
    !selection.connection_name ||
    !selection.model_id ||
    typeof record.credential_ready !== "boolean" ||
    typeof record.hot_switchable !== "boolean" ||
    (expectedAgentId && selection.session_id !== expectedAgentId.trim())
  ) {
    return null;
  }
  return selection;
}

export function parseProviderModelsResult(
  raw: unknown,
  expectedConnectionId: string,
): ProviderModelsResult | null {
  const record = asRecord(raw);
  if (!record || !Array.isArray(record.models)) return null;
  assertSecretFree(record);
  const connectionId = asString(record.connection_id);
  if (!connectionId || connectionId !== expectedConnectionId.trim()) return null;
  const models: ProviderModel[] = [];
  const ids = new Set<string>();
  for (const item of record.models) {
    const model = parseProviderModel(item);
    if (!model || ids.has(model.id)) return null;
    ids.add(model.id);
    models.push(model);
  }
  return {
    connectionId,
    models,
    discoveryWarning: asString(record.discovery_warning) || undefined,
    persistenceWarning: asString(record.persistence_warning) || undefined,
    persistenceDurable:
      typeof record.persistence_durable === "boolean"
        ? record.persistence_durable
        : undefined,
  };
}

export function parseProviderCredentialResult(
  raw: unknown,
  expectedConnectionId?: string,
): ProviderCredentialResult | null {
  const record = asRecord(raw);
  if (!record) return null;
  assertSecretFree(record);
  const connectionId = asString(record.connection_id);
  if (
    !connectionId ||
    (expectedConnectionId &&
      connectionId !== expectedConnectionId.trim()) ||
    typeof record.credential_ready !== "boolean"
  ) {
    return null;
  }
  return {
    connection_id: connectionId,
    credential_ready: record.credential_ready,
    persistence: requireAppliedPersistence(record),
  };
}

export function parseProviderConnectionTestResult(
  raw: unknown,
  expectedClient: string,
): ProviderConnectionTestResult | null {
  const record = asRecord(raw);
  if (!record) return null;
  assertSecretFree(record);
  const client = normalizeProviderClient(asString(record.client));
  const modelCount = record.model_count;
  const latencyMs = record.latency_ms;
  if (
    !isSupportedProviderClient(client) ||
    client !== normalizeProviderClient(expectedClient) ||
    typeof modelCount !== "number" ||
    !Number.isInteger(modelCount) ||
    modelCount < 0 ||
    typeof latencyMs !== "number" ||
    !Number.isInteger(latencyMs) ||
    latencyMs < 0
  ) {
    return null;
  }
  return { client, modelCount, latencyMs };
}

export const parseProviderCatalogProjection = parseProvidersSnapshot;

export function parseProviderModelsDiscovery(
  raw: unknown,
): ProviderModelsResult | null {
  const record = asRecord(raw);
  const connectionId = asString(record?.connection_id);
  if (!connectionId) return null;
  return parseProviderModelsResult(raw, connectionId);
}

export function assertThreadRuntimeMatches(
  selection: ThreadRuntimeSelection,
  input: {
    agentId: string;
    runtime: { connectionId: string; modelId: string; effect?: string; useDefaultEffect?: boolean };
  },
): boolean {
  return (
    selection.session_id === normalizeProviderId(input.agentId) &&
    selection.connection_id === normalizeProviderId(input.runtime.connectionId) &&
    selection.model_id === normalizeProviderId(input.runtime.modelId) &&
    (input.runtime.useDefaultEffect
      ? !selection.reasoning_effort
      : !input.runtime.effect ||
        selection.reasoning_effort === normalizeProviderId(input.runtime.effect))
  );
}
