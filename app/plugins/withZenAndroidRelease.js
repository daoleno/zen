/**
 * Expo config plugin: package Ghostty MIT notice into Android assets and
 * wire optional release signing from ZEN_ANDROID_* environment variables.
 *
 * Secrets are read only from process environment inside Gradle (never printed).
 * Idempotent: re-running prebuild replaces the @generated blocks.
 */
const fs = require('fs');
const path = require('path');
const {
  createRunOncePlugin,
  withAppBuildGradle,
  withDangerousMod,
} = require('@expo/config-plugins');

const NOTICE_SRC_REL = 'assets/notices/GHOSTTY-MIT.txt';
const NOTICE_APK_REL = 'assets/notices/GHOSTTY-MIT.txt'; // path inside APK zip
const BEGIN_SIGNING = '// @generated begin zen-android-release-signing';
const END_SIGNING = '// @generated end zen-android-release-signing';
const BEGIN_RELEASE_BT = '// @generated begin zen-android-release-buildtype-signing';
const END_RELEASE_BT = '// @generated end zen-android-release-buildtype-signing';

function stripGenerated(contents, begin, end) {
  const re = new RegExp(
    `${escapeRegExp(begin)}[\\s\\S]*?${escapeRegExp(end)}\\n?`,
    'g'
  );
  return contents.replace(re, '');
}

function escapeRegExp(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/** Pure helper for tests: inject signing blocks into app/build.gradle text. */
function injectReleaseSigningGradle(contents) {
  let next = stripGenerated(contents, BEGIN_SIGNING, END_SIGNING);
  next = stripGenerated(next, BEGIN_RELEASE_BT, END_RELEASE_BT);

  const releaseSigningConfig = `
        ${BEGIN_SIGNING}
        release {
            // Optional release keystore via env (never commit secrets).
            def zenKs = System.getenv("ZEN_ANDROID_KEYSTORE")
            if (zenKs != null && !zenKs.toString().trim().isEmpty()) {
                storeFile file(zenKs.toString())
                storePassword System.getenv("ZEN_ANDROID_KEYSTORE_PASSWORD")
                keyAlias System.getenv("ZEN_ANDROID_KEY_ALIAS")
                keyPassword System.getenv("ZEN_ANDROID_KEY_PASSWORD")
            }
        }
        ${END_SIGNING}
`;

  if (!/signingConfigs\s*\{/.test(next)) {
    throw new Error('withZenAndroidRelease: signingConfigs block not found');
  }

  // Insert release config before the closing brace of signingConfigs.
  // Match signingConfigs { ... } non-greedily at first level by locating debug block end.
  if (!next.includes(BEGIN_SIGNING)) {
    const debugBlock =
      /signingConfigs\s*\{\s*debug\s*\{[\s\S]*?\n\s*\}/;
    if (!debugBlock.test(next)) {
      throw new Error(
        'withZenAndroidRelease: could not locate signingConfigs.debug block'
      );
    }
    next = next.replace(debugBlock, (m) => `${m}\n${releaseSigningConfig}`);
  }

  const releaseSigningUse = `
            ${BEGIN_RELEASE_BT}
            // Prefer ZEN_ANDROID_KEYSTORE env when set; otherwise debug keystore (dev sideload).
            def zenKs = System.getenv("ZEN_ANDROID_KEYSTORE")
            if (zenKs != null && !zenKs.toString().trim().isEmpty()) {
                signingConfig signingConfigs.release
            } else {
                signingConfig signingConfigs.debug
            }
            ${END_RELEASE_BT}
`;

  // Replace unconditional release signingConfig debug assignment if present.
  const releaseSigningLine =
    /(buildTypes\s*\{[\s\S]*?release\s*\{[\s\S]*?)signingConfig\s+signingConfigs\.debug/;
  if (releaseSigningLine.test(next) && !next.includes(BEGIN_RELEASE_BT)) {
    next = next.replace(releaseSigningLine, `$1${releaseSigningUse}`);
  } else if (!next.includes(BEGIN_RELEASE_BT)) {
    // Insert after `release {`
    next = next.replace(
      /(buildTypes\s*\{[\s\S]*?release\s*\{)/,
      `$1\n${releaseSigningUse}`
    );
  }

  return next;
}

function withZenNoticeAssets(config) {
  return withDangerousMod(config, [
    'android',
    async (cfg) => {
      const projectRoot = cfg.modRequest.projectRoot;
      const src = path.join(projectRoot, NOTICE_SRC_REL);
      if (!fs.existsSync(src)) {
        throw new Error(
          `withZenAndroidRelease: missing Ghostty MIT notice at ${src}`
        );
      }
      const destDir = path.join(
        cfg.modRequest.platformProjectRoot,
        'app/src/main/assets/notices'
      );
      fs.mkdirSync(destDir, { recursive: true });
      const dest = path.join(destDir, 'GHOSTTY-MIT.txt');
      fs.copyFileSync(src, dest);
      return cfg;
    },
  ]);
}

function withZenReleaseSigning(config) {
  return withAppBuildGradle(config, (cfg) => {
    cfg.modResults.contents = injectReleaseSigningGradle(
      cfg.modResults.contents
    );
    return cfg;
  });
}

function withZenAndroidRelease(config) {
  config = withZenNoticeAssets(config);
  config = withZenReleaseSigning(config);
  return config;
}

const pkg = { name: 'with-zen-android-release', version: '1.0.0' };

module.exports = createRunOncePlugin(
  withZenAndroidRelease,
  pkg.name,
  pkg.version
);
module.exports.injectReleaseSigningGradle = injectReleaseSigningGradle;
module.exports.NOTICE_APK_REL = NOTICE_APK_REL;
module.exports.NOTICE_SRC_REL = NOTICE_SRC_REL;
