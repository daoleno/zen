/**
 * Secret-free App DTOs for the daemon's public Provider wire.
 *
 * Credential values intentionally have no representation in any reply,
 * snapshot, connection, or session-selection type.
 */

export const PROVIDER_CLIENTS = ["codex", "claude"] as const;
export type ProviderClient = (typeof PROVIDER_CLIENTS)[number];

export type ProviderConnection = {
  id: string;
  name: string;
  preset_id?: string;
  clients: string[];
  credential_ready: boolean;
  advanced: boolean;
  /** Advanced/Custom only. Curated presentation must never render this. */
  base_url?: string;
  /** Advanced/Custom only. Curated presentation must never render this. */
  manual_model_id?: string;
  /**
   * Conservative masked preview of the active stored secret (bounded
   * prefix/suffix, fixed bullet center), daemon-generated from the private
   * credential store. Presentation only: the editable API-key input stays
   * logically empty and this value is never submitted as a credential.
   */
  credential_hint?: string;
};

export type ProviderPreset = {
  id: string;
  label: string;
  clients: string[];
  advanced: boolean;
};

export type ProviderDefault = {
  connection_id: string;
  model_id?: string;
};

export type ProviderModel = {
  id: string;
  available: boolean;
  source: string;
  /**
   * Daemon-owned metadata known for this model identity on managed Codex.
   * Unknown gateway-only models are clearly unsupported (fail closed), never
   * masqueraded under a compatibility identity.
   */
  known?: boolean;
};

export type ProviderModelEntry = ProviderModel;

export type ProvidersSnapshot = {
  revision: number;
  connections: ProviderConnection[];
  defaults: Record<string, ProviderDefault>;
  presets: ProviderPreset[];
  models: Record<string, ProviderModel[]>;
};

export type ProviderCatalogProjection = ProvidersSnapshot;

export type ProviderConnectionInput = {
  id?: string;
  name?: string;
  preset_id: string;
  client: ProviderClient | string;
  base_url?: string;
  model_id?: string;
  advanced?: boolean;
};

export type ProviderDefaultInput = {
  client: ProviderClient | string;
  connectionId: string;
  modelId?: string | null;
  revision: number;
};

export type ProviderSessionSelection = {
  session_id: string;
  client: string;
  connection_id: string;
  connection_name: string;
  provider_label?: string;
  model_id: string;
  /**
   * Daemon-owned Reasoning Effort override for this Session ("" = none; the
   * model/CLI default applies). Terminal and Interface project the same value.
   */
  reasoning_effort?: string;
  /** Documented model default effort (present only when the model has an effort contract). */
  reasoning_effort_default?: string;
  /** Supported effort values of the Session's model (present only when configurable). */
  reasoning_efforts?: string[];
  credential_ready: boolean;
  hot_switchable: boolean;
};

export type ProviderModelsResult = {
  connectionId: string;
  models: ProviderModel[];
  discoveryWarning?: string;
  persistenceWarning?: string;
  persistenceDurable?: boolean;
};

export type ProviderCredentialResult = {
  connection_id: string;
  credential_ready: boolean;
  persistence: import("./persistence").MutationPersistence;
};

export type ProviderConnectionTestResult = {
  client: ProviderClient;
  modelCount: number;
  latencyMs: number;
};

export type TestProviderConnectionInput = {
  client: ProviderClient;
  baseUrl: string;
  apiKey: string;
};

export type ProvidersMutationResult = {
  snapshot: ProvidersSnapshot;
  catalog: ProvidersSnapshot;
  persistence: import("./persistence").MutationPersistence;
};

export type CodexHandoffState = {
  /** applied | failed | skipped */
  state: string;
  message?: string;
};

export type ProviderActivationResult = {
  selection: ProviderSessionSelection;
  persistence: import("./persistence").MutationPersistence;
  /**
   * Managed Codex process-handoff outcome after a Zen-initiated model/effort
   * switch on a live TUI Session. Missing = no handoff attempted. The route
   * activation is authoritative regardless; the handoff only proves the
   * running Codex process shows the new identity too.
   */
  handoff?: CodexHandoffState;
};

export type SessionProviderActivationResult = ProviderActivationResult;
export type ProviderModelsDiscoveryResult = ProviderModelsResult;

export type ActivateSessionProviderInput = {
  agentId: string;
  connectionId: string;
  modelId: string;
};

export type UpsertProviderConnectionInput = {
  connection: ProviderConnectionInput;
  revision: number;
  operation?: "create" | "update";
};

export type SetProviderDefaultInput = ProviderDefaultInput;

export type CreateSessionResult = {
  agentId: string;
  /** Present only when the daemon included persistence_* fields. */
  persistence?: import("./persistence").MutationPersistence;
};

export function normalizeProviderId(
  value: string | null | undefined,
): string {
  return (value ?? "").trim();
}

export function normalizeProviderClient(
  value: string | null | undefined,
): string {
  return normalizeProviderId(value).toLowerCase();
}

export function isSupportedProviderClient(
  value: string | null | undefined,
): value is ProviderClient {
  const client = normalizeProviderClient(value);
  return client === "codex" || client === "claude";
}

export function providerClientLabel(client: string): string {
  switch (normalizeProviderClient(client)) {
    case "codex":
      return "Codex";
    case "claude":
      return "Claude Code";
    default:
      return normalizeProviderId(client) || "Unknown";
  }
}

export function newProviderRequestId(): string {
  return `provider_${Date.now().toString(36)}_${Math.random()
    .toString(36)
    .slice(2, 10)}`;
}
