/**
 * Fail-closed helpers for Provider WebSocket persistence/correlation without
 * importing React Native.
 */

import {
  classifyMutationPersistence,
  parseOptionalMutationPersistence,
  parseRequiredMutationPersistence,
  type MutationPersistence,
} from "./persistence";
import { ambiguousProviderMutation } from "./errors";

export type CorrelatedReplyAdmission =
  | { ok: true }
  | { ok: false; reason: "stale_request" | "wrong_server" | "ambiguous" };

export function admitCorrelatedProviderReply(input: {
  expectedServerId: string;
  expectedRequestId: string;
  serverId: string;
  requestId: string;
}): CorrelatedReplyAdmission {
  if (input.serverId !== input.expectedServerId) {
    return { ok: false, reason: "wrong_server" };
  }
  if (input.requestId !== input.expectedRequestId) {
    return { ok: false, reason: "stale_request" };
  }
  return { ok: true };
}

export function reconcileProviderMutationReply(payload: unknown): {
  persistence: MutationPersistence;
  classification: ReturnType<typeof classifyMutationPersistence>;
} {
  const persistence = parseRequiredMutationPersistence(payload);
  const classification = classifyMutationPersistence(persistence);
  if (classification === "ambiguous") {
    throw ambiguousProviderMutation(persistence.warning);
  }
  return { persistence, classification };
}

export function reconcileProviderListReply(payload: unknown): {
  persistence?: MutationPersistence;
  classification?: ReturnType<typeof classifyMutationPersistence>;
} {
  const persistence = parseOptionalMutationPersistence(payload);
  if (!persistence) return {};
  const classification = classifyMutationPersistence(persistence);
  if (classification === "ambiguous") {
    throw ambiguousProviderMutation(persistence.warning);
  }
  return { persistence, classification };
}

/** Ordinary create_session body must never include profile_id. */
export function createSessionBodyOmitsProfileId(
  body: Record<string, unknown>,
): boolean {
  return !Object.prototype.hasOwnProperty.call(body, "profile_id");
}
