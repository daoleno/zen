const IOS_IDENTITIES = Object.freeze({
  production: Object.freeze({
    variant: 'production',
    displayName: 'Zen',
    bundleIdentifier: 'com.daoleno.zen',
    nativeProjectName: 'Zen',
    artifactName: 'zen-ios',
  }),
  preview: Object.freeze({
    variant: 'preview',
    displayName: 'Zen',
    bundleIdentifier: 'com.daoleno.zen.preview',
    nativeProjectName: 'Zen',
    artifactName: 'zen-preview-ios',
  }),
});

function resolveIOSIdentity(value) {
  const variant = typeof value === 'string' && value.trim() ? value.trim() : 'production';
  const identity = IOS_IDENTITIES[variant];
  if (!identity) {
    throw new Error(
      `ZEN_IOS_APP_VARIANT must be one of: ${Object.keys(IOS_IDENTITIES).join(', ')}`,
    );
  }
  return identity;
}

module.exports = {
  IOS_IDENTITIES,
  resolveIOSIdentity,
};
