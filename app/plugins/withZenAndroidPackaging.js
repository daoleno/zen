/**
 * Expo config plugin: resolve duplicate BouncyCastle metadata in the Android
 * merge step (bcprov/bcpkix/bcutil all ship META-INF/LICENSE.md and per-version
 * OSGI manifests). Idempotent via a @generated block; no runtime behavior.
 */
const { createRunOncePlugin, withAppBuildGradle } = require('@expo/config-plugins');

const BEGIN = '// @generated begin zen-android-packaging';
const END = '// @generated end zen-android-packaging';
const EXCLUDES = [
  'META-INF/LICENSE.md',
  'META-INF/versions/9/OSGI-INF/MANIFEST.MF',
  'META-INF/versions/11/OSGI-INF/MANIFEST.MF',
];

function injectPackaging(contents) {
  if (contents.includes(BEGIN)) return contents;
  const block = [
    '',
    BEGIN,
    'android {',
    '  packaging {',
    '    resources {',
    `      excludes += [${EXCLUDES.map((value) => `"${value}"`).join(', ')}]`,
    '    }',
    '  }',
    '}',
    END,
    '',
  ].join('\n');
  return contents + block;
}

module.exports = createRunOncePlugin(
  (config) => withAppBuildGradle(config, (gradle) => {
    gradle.modResults.contents = injectPackaging(gradle.modResults.contents);
    return gradle;
  }),
  'withZenAndroidPackaging',
);
module.exports.injectPackaging = injectPackaging;
