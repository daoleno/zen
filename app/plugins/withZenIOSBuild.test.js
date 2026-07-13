import { describe, expect, test } from 'bun:test';
import { applyZenIOSPodfileProperties } from './withZenIOSBuild.js';

describe('withZenIOSBuild Podfile properties', () => {
  test('disables ABI-mismatched precompiled Expo modules', () => {
    const properties = applyZenIOSPodfileProperties({
      'expo.jsEngine': 'hermes',
    });

    expect(properties).toEqual({
      'expo.jsEngine': 'hermes',
      EXPO_USE_PRECOMPILED_MODULES: 'false',
    });
  });

  test('overrides stale generated values', () => {
    expect(
      applyZenIOSPodfileProperties({ EXPO_USE_PRECOMPILED_MODULES: 'true' })
        .EXPO_USE_PRECOMPILED_MODULES
    ).toBe('false');
  });
});
