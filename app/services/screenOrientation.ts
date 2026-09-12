import type * as ScreenOrientation from "expo-screen-orientation";

export type OrientationPolicy = "portrait" | "free";

/**
 * Remote Desktop is the only route that may rotate: the phone must be able to
 * follow a landscape desktop. Every other Zen route keeps the product's
 * portrait lock. The route list is explicit so a new route cannot silently
 * inherit free rotation.
 */
export function orientationPolicyFor(pathname: string | undefined | null): OrientationPolicy {
  const route = (pathname ?? "").split("?")[0].split("#")[0];
  return route === "/remote-desktop" ? "free" : "portrait";
}

let loaded: typeof ScreenOrientation | null | undefined;

// The native module is absent on a shell built before the rotation feature, so
// the policy degrades to the launch orientation instead of failing the route.
function orientationModule(): typeof ScreenOrientation | null {
  if (loaded !== undefined) return loaded;
  try {
    loaded = require("expo-screen-orientation") as typeof ScreenOrientation;
  } catch {
    loaded = null;
  }
  return loaded;
}

export function hasNativeOrientation(): boolean {
  return orientationModule() !== null;
}

/** Applies the route policy; reports whether a native lock actually ran. */
export async function applyOrientationPolicy(policy: OrientationPolicy): Promise<boolean> {
  const screens = orientationModule();
  if (!screens) return false;
  try {
    if (policy === "free") {
      // unlockAsync respects the OS auto-rotate lock and any sensor policy.
      await screens.unlockAsync();
    } else {
      await screens.lockAsync(screens.OrientationLock.PORTRAIT_UP);
    }
    return true;
  } catch {
    return false;
  }
}
