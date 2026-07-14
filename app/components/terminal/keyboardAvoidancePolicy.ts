export function shouldAvoidKeyboard(
  surfaceEnabled: boolean,
  keyboardVisible: boolean,
) {
  return surfaceEnabled && keyboardVisible;
}

export function keyboardAvoidanceResetStyle(platform: string) {
  return platform === "android"
    ? { height: "auto" as const, flex: 1 }
    : { paddingBottom: 0 };
}
