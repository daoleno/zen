import { ambiguousProviderMutation } from "./errors";

export type MutationPersistence = {
  applied: boolean;
  durable: boolean;
  outcome: string;
  warning?: string;
  ambiguous?: boolean;
};

export type PersistenceClassification =
  | "applied_durable"
  | "applied_uncertain"
  | "not_applied"
  | "ambiguous";

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null;
}

export function parseRequiredMutationPersistence(
  payload: unknown,
): MutationPersistence {
  const record = asRecord(payload);
  if (
    !record ||
    record.persistence_outcome !== "applied" ||
    typeof record.persistence_durable !== "boolean" ||
    (Object.prototype.hasOwnProperty.call(record, "persistence_applied") &&
      record.persistence_applied !== true)
  ) {
    return {
      applied: false,
      durable: false,
      outcome: "unknown",
      warning:
        "Provider persistence fields were missing, malformed, or contradictory.",
      ambiguous: true,
    };
  }
  return {
    applied: true,
    durable: record.persistence_durable,
    outcome: "applied",
    warning:
      typeof record.persistence_warning === "string"
        ? record.persistence_warning.trim() || undefined
        : undefined,
  };
}

export function parseOptionalMutationPersistence(
  payload: unknown,
): MutationPersistence | undefined {
  const record = asRecord(payload);
  if (!record) return undefined;
  const hasAny =
    Object.prototype.hasOwnProperty.call(record, "persistence_outcome") ||
    Object.prototype.hasOwnProperty.call(record, "persistence_durable") ||
    Object.prototype.hasOwnProperty.call(record, "persistence_applied");
  return hasAny ? parseRequiredMutationPersistence(record) : undefined;
}

export function parseOptionalListPersistence(
  payload: unknown,
): MutationPersistence {
  return (
    parseOptionalMutationPersistence(payload) ?? {
      applied: true,
      durable: true,
      outcome: "applied",
    }
  );
}

export const ambiguousPersistenceError = ambiguousProviderMutation;

export function classifyMutationPersistence(
  persistence: MutationPersistence,
): PersistenceClassification {
  if (persistence.ambiguous) return "ambiguous";
  if (!persistence.applied || persistence.outcome !== "applied") {
    return "not_applied";
  }
  return persistence.durable ? "applied_durable" : "applied_uncertain";
}

export function requireAppliedPersistence(
  payload: unknown,
): MutationPersistence {
  const persistence = parseRequiredMutationPersistence(payload);
  const classification = classifyMutationPersistence(persistence);
  if (classification === "ambiguous") {
    throw ambiguousProviderMutation(persistence.warning);
  }
  if (classification === "not_applied") {
    throw ambiguousProviderMutation(
      "The Provider change was not applied. Refresh before trying again.",
    );
  }
  return persistence;
}

export function durabilityWarningMessage(
  persistence: MutationPersistence,
): string {
  return (
    persistence.warning?.trim() ||
    "The Provider change applied, but durability could not be confirmed. Refresh before making another change."
  );
}
