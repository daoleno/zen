import { describe, expect, test } from 'bun:test';
import {
  injectReleaseSigningGradle,
  injectDebugIdentityGradle,
  enablePrivateNetworkHTTP,
  NOTICE_APK_REL,
  NOTICE_SRC_REL,
} from './withZenAndroidRelease.js';

const sampleGradle = `
android {
    signingConfigs {
        debug {
            storeFile file('debug.keystore')
            storePassword 'android'
            keyAlias 'androiddebugkey'
            keyPassword 'android'
        }
    }
    buildTypes {
        debug {
            signingConfig signingConfigs.debug
        }
        release {
            // Caution! In production, you need to generate your own keystore file.
            signingConfig signingConfigs.debug
            minifyEnabled false
        }
    }
}
`;

describe('withZenAndroidRelease signing injection', () => {
  test('injects release signingConfigs from env and is idempotent', () => {
    const once = injectReleaseSigningGradle(sampleGradle);
    expect(once).toContain('System.getenv("ZEN_ANDROID_KEYSTORE")');
    expect(once).toContain('signingConfigs.release');
    expect(once).toContain('@generated begin zen-android-release-signing');
    expect(once).toContain('@generated begin zen-android-release-buildtype-signing');

    const twice = injectReleaseSigningGradle(once);
    const beginCount = (twice.match(/@generated begin zen-android-release-signing/g) || [])
      .length;
    expect(beginCount).toBe(1);
    expect(twice).toContain('System.getenv("ZEN_ANDROID_KEYSTORE")');
  });

  test('exports APK notice path contract', () => {
    expect(NOTICE_SRC_REL).toBe('assets/notices/GHOSTTY-MIT.txt');
    expect(NOTICE_APK_REL).toBe('assets/notices/GHOSTTY-MIT.txt');
  });
});

describe('withZenAndroidRelease debug identity injection', () => {
  test('names debug launcher Zen Debug and suffixes package .debug', () => {
    const once = injectDebugIdentityGradle(sampleGradle);
    expect(once).toContain('applicationIdSuffix ".debug"');
    expect(once).toContain('resValue "string", "app_name", "Zen Debug"');
    expect(once).toContain('@generated begin zen-android-debug-identity');

    // Only buildTypes.debug — not signingConfigs.debug
    const buildTypesSlice = once.slice(once.indexOf('buildTypes'));
    expect(buildTypesSlice).toContain('applicationIdSuffix ".debug"');

    const twice = injectDebugIdentityGradle(once);
    const beginCount = (
      twice.match(/@generated begin zen-android-debug-identity/g) || []
    ).length;
    expect(beginCount).toBe(1);
  });
});

describe('withZenAndroidRelease private-network HTTP config', () => {
  test('enables cleartext HTTP on the production application manifest', () => {
    const manifest = {
      manifest: {
        application: [{ $: { 'android:name': '.MainApplication' } }],
      },
    };

    const configured = enablePrivateNetworkHTTP(manifest);
    expect(configured.manifest.application[0].$['android:usesCleartextTraffic']).toBe('true');
    expect(configured.manifest.application[0].$['android:name']).toBe('.MainApplication');
  });

  test('fails clearly when the application manifest entry is absent', () => {
    expect(() => enablePrivateNetworkHTTP({ manifest: {} })).toThrow(
      'Android application manifest entry not found',
    );
  });
});
