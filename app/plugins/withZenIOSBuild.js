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
  withXcodeProject,
} = require('@expo/config-plugins');

function applyZenIOSPodfileProperties(properties) {
  return {
    ...properties,
    EXPO_USE_PRECOMPILED_MODULES: 'false',
  };
}

function applyZenIOSVersionBuildSettings(project, { marketingVersion, buildNumber }) {
  if (!/^(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]?)\.(0|[1-9][0-9]?)$/.test(marketingVersion)) {
    throw new Error(`invalid iOS marketing version: ${marketingVersion}`);
  }
  if (!/^[1-9][0-9]*$/.test(buildNumber)) {
    throw new Error(`invalid iOS build number: ${buildNumber}`);
  }

  const configurations = project.pbxXCBuildConfigurationSection();
  for (const [key, configuration] of Object.entries(configurations)) {
    if (key.endsWith('_comment') || !configuration || !configuration.buildSettings) {
      continue;
    }
    configuration.buildSettings.MARKETING_VERSION = marketingVersion;
    configuration.buildSettings.CURRENT_PROJECT_VERSION = buildNumber;
  }
  return project;
}

function withZenIOSBuild(config, options) {
  config = withPodfileProperties(config, (cfg) => {
    cfg.modResults = applyZenIOSPodfileProperties(cfg.modResults);
    return cfg;
  });
  return withXcodeProject(config, (cfg) => {
    cfg.modResults = applyZenIOSVersionBuildSettings(cfg.modResults, options);
    return cfg;
  });
}

const pkg = { name: 'with-zen-ios-build', version: '1.0.0' };

module.exports = createRunOncePlugin(withZenIOSBuild, pkg.name, pkg.version);
module.exports.applyZenIOSPodfileProperties = applyZenIOSPodfileProperties;
module.exports.applyZenIOSVersionBuildSettings = applyZenIOSVersionBuildSettings;
