const { describe, expect, it } = require('bun:test');
const createConfig = require('./app.config.js');
const {
  resolveIOSBuildNumber,
  resolveIOSIdentity,
  resolveIOSMarketingVersion,
} = createConfig;

describe('native platform config', () => {
  const config = createConfig();

  it('defines a stable iOS application identity', () => {
    expect(config.name).toBe('Zen');
    expect(config.ios.bundleIdentifier).toBe('com.daoleno.zen');
    expect(config.ios.infoPlist.CFBundleDisplayName).toBe('Zen');
    expect(config.ios.infoPlist.CFBundleShortVersionString).toBe('0.1.0');
    expect(config.ios.infoPlist.CFBundleVersion).toBe('5');
    expect(config.ios.buildNumber).toBe('5');
  });

  it('selects the Preview bundle identity while keeping the installed name Zen', () => {
    expect(resolveIOSIdentity('preview')).toEqual({
      variant: 'preview',
      displayName: 'Zen',
      bundleIdentifier: 'com.daoleno.zen.preview',
      nativeProjectName: 'Zen',
      artifactName: 'zen-preview-ios',
    });
  });

  it('applies the complete Preview identity to Expo without changing Android package identity', () => {
    const previous = process.env.ZEN_IOS_APP_VARIANT;
    process.env.ZEN_IOS_APP_VARIANT = 'preview';
    try {
      const preview = createConfig();
      expect(preview.name).toBe('Zen');
      expect(preview.ios.bundleIdentifier).toBe('com.daoleno.zen.preview');
      expect(preview.ios.infoPlist.CFBundleDisplayName).toBe('Zen');
      expect(preview.android.package).toBe('com.daoleno.zen');
      expect(preview.android).toEqual(config.android);
    } finally {
      if (previous === undefined) {
        delete process.env.ZEN_IOS_APP_VARIANT;
      } else {
        process.env.ZEN_IOS_APP_VARIANT = previous;
      }
    }
  });

  it('keeps production as the default and rejects free-form identities', () => {
    expect(resolveIOSIdentity()).toEqual(resolveIOSIdentity('production'));
    expect(() => resolveIOSIdentity('Preview')).toThrow('production, preview');
    expect(() => resolveIOSIdentity('com.example.other')).toThrow('production, preview');
  });

  it('explains local-network access used by self-hosted daemons on iOS', () => {
    expect(config.ios.infoPlist.ITSAppUsesNonExemptEncryption).toBe(false);
    expect(config.ios.infoPlist.NSLocalNetworkUsageDescription).toContain('self-hosted daemon');
  });

  it('does not request microphone access for playback and QR scanning', () => {
    const camera = config.plugins.find((plugin) => Array.isArray(plugin) && plugin[0] === 'expo-camera');
    const audio = config.plugins.find((plugin) => Array.isArray(plugin) && plugin[0] === 'expo-audio');

    expect(camera?.[1]?.microphonePermission).toBe(false);
    expect(camera?.[1]?.recordAudioAndroid).toBe(false);
    expect(audio?.[1]?.microphonePermission).toBe(false);
    expect(audio?.[1]?.recordAudioAndroid).toBe(false);
  });

  it('includes the tracked Android plugin that enables private-network HTTP', () => {
    expect(config.plugins).toContain('./plugins/withZenAndroidRelease');
  });

  it('accepts an explicit monotonically increasing CI build number', () => {
    expect(resolveIOSBuildNumber('42', '2')).toBe('42');
    expect(resolveIOSBuildNumber('', '2')).toBe('2');
    expect(() => resolveIOSBuildNumber('0', '2')).toThrow();
    expect(() => resolveIOSBuildNumber('beta', '2')).toThrow();
  });

  it('derives an App Store-valid iOS marketing version and rejects invalid values', () => {
    expect(resolveIOSMarketingVersion('0.1.0-beta.2')).toBe('0.1.0');
    expect(resolveIOSMarketingVersion('12.34.56')).toBe('12.34.56');
    expect(() => resolveIOSMarketingVersion('0.1-beta')).toThrow();
    expect(() => resolveIOSMarketingVersion('01.2.3')).toThrow();
    expect(() => resolveIOSMarketingVersion('1.234.5')).toThrow();
  });
});
