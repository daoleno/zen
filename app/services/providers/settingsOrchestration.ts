/**
 * Pure Settings orchestration helpers for Provider create → credential →
 * discovery. Never retains credential values.
 */

import { invalidProviderReply } from "./errors";
import {
  classifyMutationPersistence,
  type MutationPersistence,
} from "./persistence";
import type {
  ProviderConnection,
  ProviderCredentialResult,
  ProviderModelsResult,
  ProviderPreset,
  ProvidersSnapshot,
} from "./types";
import {
  advancedConnectionInput,
  createdConnectionFromMutation,
  curatedConnectionInput,
} from "./presentation";

export type DefaultRuntimeSeedAction =
  | { kind: "preserve"; modelId: string }
  | { kind: "choose"; models: ProviderModelsResult["models"] }
  | { kind: "unavailable" };

export function defaultRuntimeSeedAction(input: {
  snapshot: ProvidersSnapshot;
  client: string;
  connectionId: string;
}): DefaultRuntimeSeedAction {
  const current = input.snapshot.defaults[input.client];
  const available = (input.snapshot.models[input.connectionId] ?? []).filter(
    (model) => model.available && model.known !== false,
  );
  if (
    current?.connection_id === input.connectionId &&
    current.model_id &&
    available.some((model) => model.id === current.model_id)
  ) {
    return { kind: "preserve", modelId: current.model_id };
  }
  if (available.length === 0) return { kind: "unavailable" };
  return { kind: "choose", models: available };
}

export function modelSupportChangeKeepsDefaultValid(input: {
  snapshot: ProvidersSnapshot;
  client: string;
  connectionId: string;
  enabledModelIds: string[];
}): boolean {
  const current = input.snapshot.defaults[input.client];
  return !(
    current?.connection_id === input.connectionId &&
    !input.enabledModelIds.includes(current.model_id)
  );
}

export type CredentialFollowUp =
  | { kind: "discover"; connectionId: string }
  | { kind: "refresh_lock"; connectionId: string; reason: string }
  | { kind: "retry_key"; connectionId: string; reason: string };

/**
 * Decide the next Settings step after a credential write settles.
 * Uncertain/ambiguous results must refresh before another mutation and must
 * not launch discovery through a refresh-required lock.
 */
export function planAfterCredentialWrite(input: {
  connectionId: string;
  result: ProviderCredentialResult;
}): CredentialFollowUp {
  const classification = classifyMutationPersistence(input.result.persistence);
  if (classification === "ambiguous" || classification === "applied_uncertain") {
    return {
      kind: "refresh_lock",
      connectionId: input.connectionId,
      reason:
        classification === "ambiguous"
          ? "Credential write settled ambiguously. Refresh before mutating again."
          : "Credential write applied with uncertain durability. Refresh before mutating again.",
    };
  }
  if (!input.result.credential_ready) {
    return {
      kind: "retry_key",
      connectionId: input.connectionId,
      reason: "API key is not ready. Add or replace it, then retry.",
    };
  }
  return { kind: "discover", connectionId: input.connectionId };
}

/** Discovery is refused while the catalog write lock is held. */
export function mayDiscoverAfterCredential(input: {
  catalogRefreshRequired: boolean;
  followUp: CredentialFollowUp;
}): boolean {
  return (
    input.followUp.kind === "discover" && input.catalogRefreshRequired !== true
  );
}

export function resolveCreatedConnection(input: {
  previous: ProvidersSnapshot;
  next: ProvidersSnapshot;
  presetId: string;
}): ProviderConnection {
  return createdConnectionFromMutation(
    input.previous,
    input.next,
    input.presetId,
  );
}

export function curatedCreateInput(preset: ProviderPreset, client: string) {
  const input = curatedConnectionInput(preset, client);
  if (input.model_id || (input as { protocol?: string }).protocol) {
    throw invalidProviderReply(
      "Curated Provider create must not include model or protocol fields.",
    );
  }
  return input;
}

export function customGatewayCreateInput(input: {
  client: string;
  name: string;
  baseUrl: string;
  /** Optional explicit upstream model. Omit to stay discovery-driven; the
   *  daemon fails closed at binding time instead of fabricating a model. */
  modelId?: string;
}) {
  return advancedConnectionInput(input);
}

/** Scrub any accidental credential fields from durable projection objects. */
export function assertNoCredentialRetention(value: unknown): void {
  if (!value || typeof value !== "object") return;
  const seen = new Set<unknown>();
  const walk = (node: unknown) => {
    if (!node || typeof node !== "object" || seen.has(node)) return;
    seen.add(node);
    if (Array.isArray(node)) {
      for (const item of node) walk(item);
      return;
    }
    for (const [key, nested] of Object.entries(
      node as Record<string, unknown>,
    )) {
      const normalized = key.trim().toLowerCase();
      if (
        normalized === "credential" ||
        normalized === "api_key" ||
        normalized === "apikey" ||
        normalized === "secret" ||
        normalized === "access_token"
      ) {
        throw invalidProviderReply(
          "Credential values must never enter App projection state.",
        );
      }
      walk(nested);
    }
  };
  walk(value);
}

export function mergeDiscoveredModels(input: {
  snapshot: ProvidersSnapshot;
  discovery: ProviderModelsResult;
}): ProvidersSnapshot {
  assertNoCredentialRetention(input.discovery);
  return {
    ...input.snapshot,
    models: {
      ...input.snapshot.models,
      [input.discovery.connectionId]: input.discovery.models,
    },
  };
}

export function settleCredentialPersistence(
  persistence: MutationPersistence,
): {
  applied: boolean;
  durable: boolean;
  ambiguous: boolean;
  refreshRequired: boolean;
} {
  const classification = classifyMutationPersistence(persistence);
  return {
    applied:
      classification === "applied_durable" ||
      classification === "applied_uncertain",
    durable: classification === "applied_durable",
    ambiguous: classification === "ambiguous",
    refreshRequired:
      classification === "ambiguous" || classification === "applied_uncertain",
  };
}
