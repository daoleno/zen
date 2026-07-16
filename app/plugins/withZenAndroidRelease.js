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
  withAndroidManifest,
  withAppBuildGradle,
  withDangerousMod,
} = require('@expo/config-plugins');

const NOTICE_SRC_REL = 'assets/notices/GHOSTTY-MIT.txt';
const NOTICE_APK_REL = 'assets/notices/GHOSTTY-MIT.txt'; // path inside APK zip
const BEGIN_SIGNING = '// @generated begin zen-android-release-signing';
const END_SIGNING = '// @generated end zen-android-release-signing';
const BEGIN_RELEASE_BT = '// @generated begin zen-android-release-buildtype-signing';
const END_RELEASE_BT = '// @generated end zen-android-release-buildtype-signing';
const BEGIN_DEBUG_BT = '// @generated begin zen-android-debug-identity';
const END_DEBUG_BT = '// @generated end zen-android-debug-identity';

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

/**
 * Pure helper: debug buildType shows as "Zen Debug" with a separate package id
 * so it can sit alongside the release "Zen" install.
 */
function injectDebugIdentityGradle(contents) {
  let next = stripGenerated(contents, BEGIN_DEBUG_BT, END_DEBUG_BT);

  const debugIdentity = `
            ${BEGIN_DEBUG_BT}
            // Side-by-side with release: launcher "Zen Debug", package *.debug
            applicationIdSuffix ".debug"
            resValue "string", "app_name", "Zen Debug"
            ${END_DEBUG_BT}
`;

  if (next.includes(BEGIN_DEBUG_BT)) {
    return next;
  }

  // Insert after `debug {` inside buildTypes (not signingConfigs.debug).
  const buildTypesDebug = /(buildTypes\s*\{[\s\S]*?\bdebug\s*\{)/;
  if (!buildTypesDebug.test(next)) {
    throw new Error(
      'withZenAndroidRelease: could not locate buildTypes.debug block'
    );
  }
  next = next.replace(buildTypesDebug, `$1\n${debugIdentity}`);
  return next;
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
    let contents = injectReleaseSigningGradle(cfg.modResults.contents);
    contents = injectDebugIdentityGradle(contents);
    cfg.modResults.contents = contents;
    return cfg;
  });
}

function enablePrivateNetworkHTTP(androidManifest) {
  const application = androidManifest?.manifest?.application?.[0];
  if (!application) {
    throw new Error('withZenAndroidRelease: Android application manifest entry not found');
  }
  application.$ = application.$ || {};
  application.$['android:usesCleartextTraffic'] = 'true';
  return androidManifest;
}

function withZenPrivateNetworkHTTP(config) {
  return withAndroidManifest(config, (cfg) => {
    cfg.modResults = enablePrivateNetworkHTTP(cfg.modResults);
    return cfg;
  });
}

function withZenAndroidRelease(config) {
  config = withZenNoticeAssets(config);
  config = withZenReleaseSigning(config);
  config = withZenPrivateNetworkHTTP(config);
  return config;
}

const pkg = { name: 'with-zen-android-release', version: '1.0.0' };

module.exports = createRunOncePlugin(
  withZenAndroidRelease,
  pkg.name,
  pkg.version
);
module.exports.injectReleaseSigningGradle = injectReleaseSigningGradle;
module.exports.injectDebugIdentityGradle = injectDebugIdentityGradle;
module.exports.enablePrivateNetworkHTTP = enablePrivateNetworkHTTP;
module.exports.NOTICE_APK_REL = NOTICE_APK_REL;
module.exports.NOTICE_SRC_REL = NOTICE_SRC_REL;
