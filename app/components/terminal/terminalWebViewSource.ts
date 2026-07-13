const DEFAULT_TERMINAL_BASE_URL = 'https://zen.local/';

export function terminalWebViewBaseUrl(fontUri: string, platform: string): string {
  if (platform !== 'ios' || !fontUri.startsWith('file://')) {
    return DEFAULT_TERMINAL_BASE_URL;
  }

  const separator = fontUri.lastIndexOf('/');
  return separator >= 'file://'.length
    ? fontUri.slice(0, separator + 1)
    : DEFAULT_TERMINAL_BASE_URL;
}
