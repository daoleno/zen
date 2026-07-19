export type SystemRootBackgroundDeps = {
  setBackgroundColorAsync: (color: string) => Promise<void>;
};

/**
 * Sync the Expo system root surface to the resolved Zen canvas on Android and
 * iOS. `expo-system-ui` is the shared mobile owner (no per-platform theme
 * state). SoftInput / route detach can expose that surface; it must follow
 * ThemeProvider's bgPrimary. Best-effort: setter failures must not reject.
 */
export function syncSystemRootBackground(
  color: string,
  deps: SystemRootBackgroundDeps,
): Promise<void> {
  return deps.setBackgroundColorAsync(color).catch(() => undefined);
}
