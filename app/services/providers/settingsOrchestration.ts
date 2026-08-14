/**
 * Pure Settings orchestration helpers for Provider create → credential →
 * discovery. Never retains credential values.
 */

import { invalidProviderReply } from "./errors";
import {
  classifyMutationPersistence,
  type MutationPersistence,
} from "./persistence";
import { modelSupportedOnConnection } from "./sessionModelHelpers";
import type {
  ProviderConnection,
  ProviderCredentialResult,
  ProviderModelsResult,
  ProviderPreset,
  ProviderSessionSelection,
  ProvidersSnapshot,
} from "./types";
import {
  advancedConnectionInput,
  createdConnectionFromMutation,
  curatedConnectionInput,
} from "./presentation";

export type SettingsSwitchCarryover = {
  /** Exact current compatible routed Session (never another Session). */
  agentId: string;
  /** Exact preferred connection id from the Settings selection. */
  connectionId: string;
  /** The current Session's model — carried, never substituted. */
  modelId: string;
};

/**
 * The Settings-only Provider switch plan for one client card selection.
 * Settings owns Provider selection: the preferred Provider is persisted with
 * NO fabricated model (a new default connection starts model-required until
 * the client chooses a model). Carryover activates the exact new Provider +
 * current Model on the current compatible routed Session, and ONLY when the
 * current model is not contradicted by the new Provider's synced allowlist —
 * never a fallback, never a different Session.
 */
export function planSettingsProviderSwitch(input: {
  snapshot: ProvidersSnapshot;
  connection: ProviderConnection;
  /** Session the Settings action may activate (null = no current Session). */
  currentSession: { agentId: string } | null;
  /** Current live selection of that Session (null = not loaded). */
  currentSelection: ProviderSessionSelection | null;
}): {
  preferredConnectionId: string;
  carryover: SettingsSwitchCarryover | null;
  /**
   * True when the current model is explicitly unsupported on the selected
   * Provider: the switch stays model-required and never falls back.
   */
  unsupportedCurrentModel: boolean;
} {
  const preferredConnectionId = input.connection.id;
  if (!input.currentSession || !input.currentSelection) {
    return { preferredConnectionId, carryover: null, unsupportedCurrentModel: false };
  }
  const selection = input.currentSelection;
  if (selection.hot_switchable !== true) {
    return { preferredConnectionId, carryover: null, unsupportedCurrentModel: false };
  }
  // The Session already runs the selected Provider: nothing to activate.
  if (selection.connection_id === input.connection.id) {
    return { preferredConnectionId, carryover: null, unsupportedCurrentModel: false };
  }
  const currentModel = selection.model_id.trim();
  if (!currentModel) {
    return { preferredConnectionId, carryover: null, unsupportedCurrentModel: false };
  }
  // An explicit synced allowlist without the current model means unsupported:
  // keep the old route, stay model-required, never fall back. An unknown
  // (empty/unsynced) allowlist defers to the daemon's authoritative admission.
  if (
    modelSupportedOnConnection(input.snapshot, input.connection.id, currentModel) ===
    false
  ) {
    return {
      preferredConnectionId,
      carryover: null,
      unsupportedCurrentModel: true,
    };
  }
  return {
    preferredConnectionId,
    carryover: {
      agentId: input.currentSession.agentId,
      connectionId: input.connection.id,
      modelId: currentModel,
    },
    unsupportedCurrentModel: false,
  };
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
