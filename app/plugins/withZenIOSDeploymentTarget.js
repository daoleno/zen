const { withXcodeProject } = require('expo/config-plugins');

const MINIMUM_IOS = '17.0';

function setIOSDeploymentTarget(project) {
  const configurations = project.pbxXCBuildConfigurationSection();
  for (const value of Object.values(configurations)) {
    if (!value || typeof value !== 'object' || !value.buildSettings) continue;
    value.buildSettings.IPHONEOS_DEPLOYMENT_TARGET = MINIMUM_IOS;
  }
  return project;
}

/** Keep the app target compatible with the pinned Ghostty XCFramework slices. */
module.exports = function withZenIOSDeploymentTarget(config) {
  return withXcodeProject(config, (mod) => {
    setIOSDeploymentTarget(mod.modResults);
    return mod;
  });
};

module.exports.MINIMUM_IOS = MINIMUM_IOS;
module.exports.setIOSDeploymentTarget = setIOSDeploymentTarget;
