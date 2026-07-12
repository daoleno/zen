import { describe, expect, test } from 'bun:test';
import {
  injectReleaseSigningGradle,
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
