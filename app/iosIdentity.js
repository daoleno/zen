const IOS_IDENTITIES = Object.freeze({
  production: Object.freeze({
    variant: "production",
    displayName: "Zen",
    bundleIdentifier: "com.daoleno.zen",
    nativeProjectName: "Zen",
    artifactName: "zen-ios",
    // Release identities ship to TestFlight/App Store Connect; remote push needs
    // production APS. Local notifications do not depend on this entitlement.
    notificationMode: "production",
  }),
  preview: Object.freeze({
    variant: "preview",
    displayName: "Zen",
    bundleIdentifier: "com.daoleno.zen.preview",
    nativeProjectName: "Zen",
    artifactName: "zen-preview-ios",
    notificationMode: "production",
  }),
});

function resolveIOSIdentity(value) {
  const variant =
    typeof value === "string" && value.trim() ? value.trim() : "production";
  const identity = IOS_IDENTITIES[variant];
  if (!identity) {
    throw new Error(
      `ZEN_IOS_APP_VARIANT must be one of: ${Object.keys(IOS_IDENTITIES).join(", ")}`,
    );
  }
  return identity;
}

function resolveIOSNotificationMode(identity) {
  const mode = identity && identity.notificationMode;
  if (mode !== "production" && mode !== "development") {
    throw new Error("iOS notification mode must be production or development");
  }
  return mode;
}

module.exports = {
  IOS_IDENTITIES,
  resolveIOSIdentity,
  resolveIOSNotificationMode,
};
