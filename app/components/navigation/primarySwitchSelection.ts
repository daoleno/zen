import type { PrimaryRouteName } from "../../services/interactionTrace";

/** Pager index for the two primary TopTabs routes (index → list). */
export function primaryRoutePagerIndex(route: PrimaryRouteName): 0 | 1 {
  return route === "list" ? 1 : 0;
}

export interface PrimarySwitchTapResult {
  cancelTrace: boolean;
  pendingRoute: PrimaryRouteName | null;
  shouldNavigate: boolean;
}

/**
 * Direct Header tap navigation policy.
 * `pendingRoute` tracks an in-flight navigate target only — never Header visuals.
 */
export function applyPrimarySwitchTap({
  canonicalRoute,
  pendingRoute,
  target,
}: {
  canonicalRoute: PrimaryRouteName;
  pendingRoute: PrimaryRouteName | null;
  target: PrimaryRouteName;
}): PrimarySwitchTapResult {
  if (target === pendingRoute) {
    return {
      cancelTrace: false,
      pendingRoute,
      shouldNavigate: false,
    };
  }
  if (target === canonicalRoute) {
    if (pendingRoute == null) {
      return {
        cancelTrace: false,
        pendingRoute: null,
        shouldNavigate: false,
      };
    }
    // Canonical not yet updated, but reverse must override the pending transition.
    return {
      cancelTrace: true,
      pendingRoute: null,
      shouldNavigate: true,
    };
  }
  return {
    cancelTrace: false,
    pendingRoute: target,
    shouldNavigate: true,
  };
}

/**
 * pressIn trace policy. Repeat against the same pending target preserves the
 * open trace; reverse to canonical cancels without opening a replacement.
 */
export function applyPrimarySwitchPressIn({
  canonicalRoute,
  pendingRoute,
  target,
}: {
  canonicalRoute: PrimaryRouteName;
  pendingRoute: PrimaryRouteName | null;
  target: PrimaryRouteName;
}): { cancelTrace: boolean; openTrace: boolean } {
  if (target === pendingRoute) {
    return { cancelTrace: false, openTrace: false };
  }
  if (target === canonicalRoute) {
    return { cancelTrace: pendingRoute != null, openTrace: false };
  }
  return { cancelTrace: false, openTrace: true };
}

/** Clear pending once React Navigation canonical catches up. */
export function reconcilePrimarySwitchPending(
  canonicalRoute: PrimaryRouteName,
  pendingRoute: PrimaryRouteName | null,
): PrimaryRouteName | null {
  if (pendingRoute == null || canonicalRoute === pendingRoute) {
    return null;
  }
  return pendingRoute;
}
