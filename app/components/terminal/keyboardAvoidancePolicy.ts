export function shouldAvoidKeyboard(
  surfaceEnabled: boolean,
  keyboardVisible: boolean,
) {
  return surfaceEnabled && keyboardVisible;
}
