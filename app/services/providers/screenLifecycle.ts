import type { ProvidersSnapshot } from "./types";

/**
 * Presentation-only in-flight flags for the Providers Settings screen.
 * Request ownership remains in ProviderRequestOwner; these flags only drive UI.
 */
export type ProvidersInFlightFlags = {
  loading: boolean;
  refreshing: boolean;
  mutating: boolean;
};

export function idleProvidersInFlightFlags(): ProvidersInFlightFlags {
  return {
    loading: false,
    refreshing: false,
    mutating: false,
  };
}

/**
 * Blur/unfocus must clear presentation-only busy state so a stale in-flight
 * operation cannot leave the screen disabled or spinning after return.
 * The catalog projection is preserved until the next focused refresh; callers
 * must also invalidate request ownership so stale replies stay ignored.
 */
export function providersScreenAfterBlur(input: {
  flags: ProvidersInFlightFlags;
  catalog: ProvidersSnapshot | null;
}): {
  flags: ProvidersInFlightFlags;
  catalog: ProvidersSnapshot | null;
} {
  return {
    flags: idleProvidersInFlightFlags(),
    catalog: input.catalog,
  };
}
