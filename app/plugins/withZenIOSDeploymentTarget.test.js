import { describe, expect, test } from 'bun:test';
import plugin from './withZenIOSDeploymentTarget.js';

const { MINIMUM_IOS, setIOSDeploymentTarget } = plugin;

describe('withZenIOSDeploymentTarget', () => {
  test('sets every generated Xcode build configuration to the pinned floor', () => {
    const section = {
      Debug: { buildSettings: { IPHONEOS_DEPLOYMENT_TARGET: '15.1' } },
      Release: { buildSettings: {} },
      DebugComment: 'Debug',
    };
    const project = { pbxXCBuildConfigurationSection: () => section };

    expect(setIOSDeploymentTarget(project)).toBe(project);
    expect(section.Debug.buildSettings.IPHONEOS_DEPLOYMENT_TARGET).toBe('17.0');
    expect(section.Release.buildSettings.IPHONEOS_DEPLOYMENT_TARGET).toBe('17.0');
    expect(MINIMUM_IOS).toBe('17.0');
  });
});
