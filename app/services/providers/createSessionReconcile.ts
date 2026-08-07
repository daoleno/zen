import {
  ProviderError,
  providerMutationRequiresRefresh,
} from "./errors";
import {
  classifyMutationPersistence,
  durabilityWarningMessage,
  type MutationPersistence,
} from "./persistence";
import type { CreateSessionResult } from "./types";

export type CreateSessionReconciliation =
  | {
      kind: "navigable";
      agentId: string;
      persistence: MutationPersistence;
      durabilityWarning: string | null;
      writeLocked: boolean;
    }
  | {
      kind: "ambiguous";
      message: string;
      requiresReconcileBeforeCreate: true;
    }
  | {
      kind: "failed";
      message: string;
      requiresReconcileBeforeCreate: boolean;
    };

export function reconcileCreateSessionSuccess(
  created: CreateSessionResult,
): CreateSessionReconciliation {
  const persistence: MutationPersistence = created.persistence ?? {
    applied: true,
    durable: true,
    outcome: "applied",
  };
  const classification = classifyMutationPersistence(persistence);
  if (classification === "ambiguous") {
    return {
      kind: "ambiguous",
      message:
        persistence.warning ||
        "The create result was ambiguous. Refresh Sessions before creating another.",
      requiresReconcileBeforeCreate: true,
    };
  }
  if (classification === "not_applied") {
    return {
      kind: "failed",
      message:
        persistence.warning ||
        "The Session was not created. Refresh Sessions before trying again.",
      requiresReconcileBeforeCreate: false,
    };
  }
  return {
    kind: "navigable",
    agentId: created.agentId,
    persistence,
    durabilityWarning:
      classification === "applied_uncertain"
        ? durabilityWarningMessage(persistence)
        : null,
    writeLocked: classification === "applied_uncertain",
  };
}

export function reconcileCreateSessionFailure(
  error: unknown,
  dispatched: boolean,
): CreateSessionReconciliation {
  const message =
    error instanceof Error ? error.message : "Could not create terminal.";
  if (
    dispatched &&
    (!(error instanceof ProviderError) ||
      providerMutationRequiresRefresh(error))
  ) {
    return {
      kind: "ambiguous",
      message:
        message ||
        "The create result is ambiguous. Refresh Sessions before creating another.",
      requiresReconcileBeforeCreate: true,
    };
  }
  return {
    kind: "failed",
    message,
    requiresReconcileBeforeCreate: false,
  };
}
