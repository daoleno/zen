/**
 * Expo config plugin: keep Expo iOS modules on one Swift ABI.
 *
 * Expo can resolve precompiled Swift modules independently from the local
 * ExpoModulesCore source selected by the lockfile. We observed that mix cause
 * a dyld crash before React Native started. Building all Expo iOS modules from
 * the locked sources keeps those binary interfaces on one ABI.
 */
const {
  createRunOncePlugin,
  withPodfileProperties,
} = require('@expo/config-plugins');

function applyZenIOSPodfileProperties(properties) {
  return {
    ...properties,
    EXPO_USE_PRECOMPILED_MODULES: 'false',
  };
}

function withZenIOSBuild(config) {
  return withPodfileProperties(config, (cfg) => {
    cfg.modResults = applyZenIOSPodfileProperties(cfg.modResults);
    return cfg;
  });
}

const pkg = { name: 'with-zen-ios-build', version: '1.0.0' };

module.exports = createRunOncePlugin(withZenIOSBuild, pkg.name, pkg.version);
module.exports.applyZenIOSPodfileProperties = applyZenIOSPodfileProperties;
