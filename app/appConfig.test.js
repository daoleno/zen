const { describe, expect, it } = require('bun:test');
const createConfig = require('./app.config.js');

describe('native platform config', () => {
  const config = createConfig();

  it('defines a stable iOS application identity', () => {
    expect(config.ios.bundleIdentifier).toBe('com.daoleno.zen');
    expect(config.ios.buildNumber).toBe('2');
  });

  it('explains local-network access used by self-hosted daemons on iOS', () => {
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
});
