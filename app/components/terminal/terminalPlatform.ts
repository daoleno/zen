export function supportsNativeTerminalPlatform(platform: string): boolean {
  return platform === 'android' || platform === 'ios';
}
