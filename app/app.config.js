const fs = require('fs');
const path = require('path');
const { expo: baseConfig } = require('./app.base.json');

loadEnvFile(path.join(__dirname, '.env.local'));

const projectId = typeof process.env.ZEN_EXPO_PROJECT_ID === 'string'
  ? process.env.ZEN_EXPO_PROJECT_ID.trim()
  : '';

module.exports = () => {
  const extra = { ...(baseConfig.extra || {}) };
  const iosBuildNumber = resolveIOSBuildNumber(
    process.env.ZEN_IOS_BUILD_NUMBER,
    baseConfig.ios && baseConfig.ios.buildNumber,
  );

  if (projectId) {
    extra.eas = { projectId };
  }

  return {
    ...baseConfig,
    ios: {
      ...(baseConfig.ios || {}),
      buildNumber: iosBuildNumber,
    },
    plugins: [
      ...(baseConfig.plugins || []),
      [
        'expo-audio',
        {
          // Zen only plays meditation audio; it never records from the mic.
          microphonePermission: false,
          recordAudioAndroid: false,
          enableBackgroundPlayback: true,
        },
      ],
      'expo-font',
      'expo-status-bar',
      'expo-video',
      // Keep Expo Swift modules on one source-built ABI so patch upgrades
      // cannot mix precompiled ExpoVideo and ExpoModulesCore binaries.
      './plugins/withZenIOSBuild',
      // Package Ghostty MIT notice into Android assets + env-based release signing.
      './plugins/withZenAndroidRelease',
    ],
    extra,
  };
};

function resolveIOSBuildNumber(value, fallback) {
  const candidate = typeof value === 'string' && value.trim() ? value.trim() : fallback;
  if (typeof candidate !== 'string' || !/^[1-9][0-9]*$/.test(candidate)) {
    throw new Error('ZEN_IOS_BUILD_NUMBER must be a positive integer');
  }
  return candidate;
}

module.exports.resolveIOSBuildNumber = resolveIOSBuildNumber;

function loadEnvFile(filePath) {
  if (!fs.existsSync(filePath)) {
    return;
  }

  const content = fs.readFileSync(filePath, 'utf8');
  for (const rawLine of content.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith('#')) {
      continue;
    }

    const separatorIndex = line.indexOf('=');
    if (separatorIndex <= 0) {
      continue;
    }

    const key = line.slice(0, separatorIndex).trim();
    if (!key || Object.prototype.hasOwnProperty.call(process.env, key)) {
      continue;
    }

    let value = line.slice(separatorIndex + 1).trim();
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1);
    }

    process.env[key] = value;
  }
}
