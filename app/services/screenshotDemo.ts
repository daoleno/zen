export const SCREENSHOT_DEMO_STATES = [
  "chat",
  "sessions",
  "brain",
  "stats",
  "calendar",
] as const;

export type ScreenshotDemoState = (typeof SCREENSHOT_DEMO_STATES)[number];

type ScreenshotDemoEnvironment = {
  dev: boolean;
  enabled: string | undefined;
};

export function screenshotDemoEnabled(
  environment: ScreenshotDemoEnvironment = {
    dev: typeof __DEV__ !== "undefined" && __DEV__,
    enabled: process.env.EXPO_PUBLIC_ZEN_SCREENSHOT_DEMO,
  },
): boolean {
  return environment.dev && environment.enabled === "1";
}

export function resolveScreenshotDemoState(
  value: string | string[] | undefined,
): ScreenshotDemoState {
  const candidate = Array.isArray(value) ? value[0] : value;
  return SCREENSHOT_DEMO_STATES.includes(candidate as ScreenshotDemoState)
    ? (candidate as ScreenshotDemoState)
    : "chat";
}
