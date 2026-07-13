// @ts-nocheck
import { describe, expect, test } from 'bun:test';
import { getTerminalCapabilityPresentation } from './terminalCapabilities';

describe('getTerminalCapabilityPresentation', () => {
  test('advertises the existing Android implementation', () => {
    expect(getTerminalCapabilityPresentation('android').supported).toBe(true);
  });

  test('describes iOS as capability-gated without claiming app-wide failure', () => {
    const result = getTerminalCapabilityPresentation('ios');
    expect(result.supported).toBe(false);
    expect(result.title).toContain('iOS');
    expect(result.hint).toContain('Chat');
  });

  test('keeps web unsupported', () => {
    expect(getTerminalCapabilityPresentation('web').supported).toBe(false);
  });
});
