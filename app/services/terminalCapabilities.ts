export type TerminalPlatform = 'android' | 'ios' | 'web' | string;

export interface TerminalCapabilityPresentation {
  supported: boolean;
  title: string;
  detail: string;
  hint: string;
}

export function getTerminalCapabilityPresentation(
  platform: TerminalPlatform,
): TerminalCapabilityPresentation {
  if (platform === 'android') {
    return {
      supported: true,
      title: 'Terminal available',
      detail: 'This build uses the native libghostty VT core.',
      hint: '',
    };
  }

  if (platform === 'ios') {
    return {
      supported: false,
      title: 'Terminal unavailable on iOS',
      detail: 'This iOS build does not yet link the Ghostty VT core and renderer.',
      hint: 'Chat and other Zen features remain available while Terminal support is completed.',
    };
  }

  return {
    supported: false,
    title: 'Terminal unavailable on this platform',
    detail: 'This build only ships the libghostty-backed terminal on Android.',
    hint: 'Use the Android app for Terminal access.',
  };
}
