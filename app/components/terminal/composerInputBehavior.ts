export const COMPOSER_SUBMIT_BEHAVIOR = "newline" as const;

export function composerReturnKeyType(platform: string) {
  return platform === "android" ? ("none" as const) : ("default" as const);
}
