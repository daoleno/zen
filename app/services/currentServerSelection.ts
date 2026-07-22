import type { StoredServer } from "./storage";

export function enqueueCurrentServerPersistence(
  pending: Promise<void>,
  write: () => Promise<void>,
): { result: Promise<void>; tail: Promise<void> } {
  const result = pending.then(write, write);
  return {
    result,
    tail: result.then(
      () => undefined,
      () => undefined,
    ),
  };
}

/**
 * Resolve the one persisted server identity. Array order is used only to
 * migrate installations that predate the current-server key.
 */
export function resolveCurrentServerId(
  servers: readonly Pick<StoredServer, "id">[],
  persistedServerId: string | null | undefined,
): string | null {
  const persisted = persistedServerId?.trim() || "";
  if (persisted && servers.some((server) => server.id === persisted)) {
    return persisted;
  }
  return servers[0]?.id ?? null;
}

export function selectCurrentServer<T extends Pick<StoredServer, "id">>(
  servers: readonly T[],
  currentServerId: string | null | undefined,
): T | null {
  if (!currentServerId) {
    return null;
  }
  return servers.find((server) => server.id === currentServerId) ?? null;
}
