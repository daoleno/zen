// @ts-nocheck
import { describe, expect, it } from 'bun:test';
import { supportsNativeTerminalPlatform } from './terminalPlatform';

describe('supportsNativeTerminalPlatform', () => {
  it('enables the shared Ghostty terminal on Android and iOS', () => {
    expect(supportsNativeTerminalPlatform('android')).toBe(true);
    expect(supportsNativeTerminalPlatform('ios')).toBe(true);
  });

  it('keeps platforms without a native Ghostty module disabled', () => {
    expect(supportsNativeTerminalPlatform('web')).toBe(false);
    expect(supportsNativeTerminalPlatform('windows')).toBe(false);
  });
});
