import { describe, expect, test } from 'bun:test';
import {
  applyZenIOSPodfileProperties,
  applyZenIOSVersionBuildSettings,
} from './withZenIOSBuild.js';

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

describe('withZenIOSBuild native version contract', () => {
  test('sets every generated Xcode configuration to the packaged iOS values', () => {
    const configurations = {
      DEBUG: { buildSettings: { MARKETING_VERSION: '1.0', CURRENT_PROJECT_VERSION: '1' } },
      DEBUG_comment: 'Debug',
      RELEASE: { buildSettings: {} },
    };
    const project = {
      pbxXCBuildConfigurationSection: () => configurations,
    };

    applyZenIOSVersionBuildSettings(project, {
      marketingVersion: '0.1.0',
      buildNumber: '42',
    });

    expect(configurations.DEBUG.buildSettings.MARKETING_VERSION).toBe('0.1.0');
    expect(configurations.DEBUG.buildSettings.CURRENT_PROJECT_VERSION).toBe('42');
    expect(configurations.RELEASE.buildSettings.MARKETING_VERSION).toBe('0.1.0');
    expect(configurations.RELEASE.buildSettings.CURRENT_PROJECT_VERSION).toBe('42');
  });

  test('rejects App Store-invalid marketing and build values', () => {
    const project = { pbxXCBuildConfigurationSection: () => ({}) };
    expect(() => applyZenIOSVersionBuildSettings(project, {
      marketingVersion: '0.1.0-beta.2',
      buildNumber: '42',
    })).toThrow('invalid iOS marketing version');
    expect(() => applyZenIOSVersionBuildSettings(project, {
      marketingVersion: '0.1.0',
      buildNumber: '0',
    })).toThrow('invalid iOS build number');
  });
});
