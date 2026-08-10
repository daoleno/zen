const fs = require("fs");
const path = require("path");
const { expo: baseConfig } = require("./app.base.json");
const { buildNumber: trackedIOSBuildNumber } = require("./ios-build.json");
const {
  resolveIOSIdentity,
  resolveIOSNotificationMode,
} = require("./iosIdentity");

loadEnvFile(path.join(__dirname, ".env.local"));

const projectId =
  typeof process.env.ZEN_EXPO_PROJECT_ID === "string"
    ? process.env.ZEN_EXPO_PROJECT_ID.trim()
    : "";

module.exports = () => {
  const extra = { ...(baseConfig.extra || {}) };
  const iosIdentity = resolveIOSIdentity(process.env.ZEN_IOS_APP_VARIANT);
  const iosNotificationMode = resolveIOSNotificationMode(iosIdentity);
  const iosMarketingVersion = resolveIOSMarketingVersion(baseConfig.version);
  const iosBuildNumber = resolveIOSBuildNumber(
    process.env.ZEN_IOS_BUILD_NUMBER,
    trackedIOSBuildNumber,
  );

  if (projectId) {
    extra.eas = { projectId };
  }

  return {
    ...baseConfig,
    ios: {
      ...(baseConfig.ios || {}),
      bundleIdentifier: iosIdentity.bundleIdentifier,
      buildNumber: iosBuildNumber,
      infoPlist: {
        ...((baseConfig.ios && baseConfig.ios.infoPlist) || {}),
        CFBundleDisplayName: iosIdentity.displayName,
        CFBundleShortVersionString: iosMarketingVersion,
        CFBundleVersion: iosBuildNumber,
      },
    },
    plugins: [
      ...(baseConfig.plugins || []),
      "expo-font",
      "expo-status-bar",
      // Own APS environment from the release identity so TestFlight/App Store
      // remote push is production-entitled; local notifications are unaffected.
      [
        "expo-notifications",
        {
          mode: iosNotificationMode,
        },
      ],
      // Keep Expo Swift modules on one source-built ABI so patch upgrades
      // cannot mix precompiled modules and ExpoModulesCore binaries.
      // Also packages Ghostty MIT notice into the iOS app bundle.
      [
        "./plugins/withZenIOSBuild",
        {
          marketingVersion: iosMarketingVersion,
          buildNumber: iosBuildNumber,
        },
      ],
      // Package Ghostty MIT notice into Android assets + env-based release signing.
      "./plugins/withZenAndroidRelease",
    ],
    extra,
  };
};

function resolveIOSBuildNumber(value, fallback) {
  const candidate =
    typeof value === "string" && value.trim() ? value.trim() : String(fallback);
  if (!/^[1-9][0-9]*$/.test(candidate)) {
    throw new Error("ZEN_IOS_BUILD_NUMBER must be a positive integer");
  }
  return candidate;
}

function resolveIOSMarketingVersion(value) {
  const candidate = typeof value === "string" ? value.split("-", 1)[0] : "";
  if (
    !/^(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]?)\.(0|[1-9][0-9]?)$/.test(candidate)
  ) {
    throw new Error(
      "iOS marketing version must be three numeric components (for example 0.1.0)",
    );
  }
  return candidate;
}

module.exports.resolveIOSBuildNumber = resolveIOSBuildNumber;
module.exports.resolveIOSMarketingVersion = resolveIOSMarketingVersion;
module.exports.resolveIOSIdentity = resolveIOSIdentity;
module.exports.resolveIOSNotificationMode = resolveIOSNotificationMode;

function loadEnvFile(filePath) {
  if (!fs.existsSync(filePath)) {
    return;
  }

  const content = fs.readFileSync(filePath, "utf8");
  for (const rawLine of content.split(/\r?\n/)) {
    const line = rawLine.trim();
    if (!line || line.startsWith("#")) {
      continue;
    }

    const separatorIndex = line.indexOf("=");
    if (separatorIndex <= 0) {
      continue;
    }

    const key = line.slice(0, separatorIndex).trim();
    if (!key || Object.prototype.hasOwnProperty.call(process.env, key)) {
      continue;
    }

    let value = line.slice(separatorIndex + 1).trim();
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }

    process.env[key] = value;
  }
}
