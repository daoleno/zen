const { describe, expect, it } = require('bun:test');
const fs = require('fs');
const path = require('path');

const workflow = fs.readFileSync(
  path.join(__dirname, '..', '.github', 'workflows', 'ios-release.yml'),
  'utf8',
);
const ciWorkflow = fs.readFileSync(
  path.join(__dirname, '..', '.github', 'workflows', 'ci.yml'),
  'utf8',
);
const nativeLibsWorkflow = fs.readFileSync(
  path.join(__dirname, '..', '.github', 'workflows', 'native-libs.yml'),
  'utf8',
);

describe('iOS signed release identity contract', () => {
  it('automatically publishes only immutable reviewed beta prereleases as Preview', () => {
    expect(workflow).toMatch(/release:\s*\n\s*types: \[published\]/);
    expect(workflow).toContain("github.event.release.prerelease == true");
    expect(workflow).toContain("contains(github.event.release.tag_name, '-beta.')");
    expect(workflow).toContain("github.event_name == 'release' && 'preview'");
    expect(workflow).toContain("github.event_name == 'release' && 'testflight'");
    expect(workflow).toContain("ref: ${{ env.ZEN_IOS_REF }}");
    expect(workflow).toContain("ref: ${{ github.workflow_sha }}");
    expect(workflow).toContain('release tag version');
    expect(workflow).toContain('ios_release = json.load(open("app/ios-build.json"');
    expect(workflow).toContain('tracked iOS buildNumber must be a positive integer');
    expect(workflow).not.toContain('tracked iOS buildNumber must equal Android versionCode');
    expect(workflow).not.toContain('version_code = base.get("android", {}).get("versionCode")');
    expect(workflow).toContain('ZEN_IOS_BUILD_NUMBER="$(python3 - "$ZEN_RELEASE_TAG"');
    expect(workflow).not.toContain('github.run_number');
  });

  it('offers only production and Preview identities and defaults to production', () => {
    expect(workflow).toContain('app_identity:');
    expect(workflow).toMatch(/app_identity:[\s\S]*?default: production[\s\S]*?options:\s*\n\s*- production\s*\n\s*- preview/);
  });

  it('isolates Preview signing material in a separate protected environment', () => {
    expect(workflow).toContain("github.event_name == 'release' || inputs.app_identity == 'preview'");
    expect(workflow).toContain("'app-store-connect-preview'");
    expect(workflow).toContain("'app-store-connect'");
    expect(workflow).toContain("ZEN_IOS_APP_VARIANT: ${{ github.event_name == 'release' && 'preview' || inputs.app_identity }}");
  });

  it('derives signing, native project, artifact, and verification values from the closed identity', () => {
    expect(workflow).toContain("require('./app/iosIdentity')");
    expect(workflow).toContain('ZEN_IOS_DISPLAY_NAME: identity.displayName');
    expect(workflow).toContain('expected_bundle = os.environ["ZEN_IOS_BUNDLE_ID"]');
    expect(workflow).toContain('provisioningProfiles": {os.environ["ZEN_IOS_BUNDLE_ID"]');
    expect(workflow).toContain('${ZEN_IOS_NATIVE_PROJECT_NAME}.xcworkspace');
    expect(workflow).toContain('${ZEN_IOS_ARTIFACT_NAME}-v${ZEN_IOS_VERSION}');
    expect(workflow).toContain('ZEN_IOS_VERSION: config.ios.infoPlist.CFBundleShortVersionString');
    expect(workflow).toContain('./scripts/verify-ios-artifact.sh ipa "${IPAS[0]}"');
    expect(workflow).toContain('"display_name": os.environ["ZEN_IOS_DISPLAY_NAME"]');
    expect(workflow).toContain('"marketing_version": os.environ["ZEN_IOS_VERSION"]');
    expect(workflow).not.toContain('"marketing_version": base["version"]');
    expect(workflow).not.toMatch(/expected_bundle\s*=\s*"com\.daoleno\.zen"/);
  });

  it('uploads Preview with an Individual API key and no altool issuer dependency', () => {
    expect(workflow).toContain("github.event_name == 'release' || inputs.app_identity == 'preview'");
    expect(workflow).toContain('HD84J3DJ2B');
    expect(workflow).toContain('./.zen-release-automation/scripts/app-store-connect-upload.py');
    expect(workflow).toContain('--app-id "$ZEN_ASC_APP_ID"');
    expect(workflow).toContain('EXTRA_ARGS=(--reuse-existing-build --beta-group-name "Zen Preview" --submit-beta-review)');
    expect(workflow).toContain('"${EXTRA_ARGS[@]}"');
    expect(workflow).toContain('zen-asc-individual-key.p8');
    expect(workflow).not.toContain('ZEN_ASC_ISSUER_ID');
    expect(workflow).not.toContain('altool');
    expect(workflow).not.toContain('--apiIssuer');
    expect(workflow).not.toContain('iTMSTransporter');
    expect(workflow).not.toContain('ipa.read_bytes()');
  });

  it('does not expose Apple secrets to the whole archive job', () => {
    const jobEnv = workflow.match(/jobs:\s*\n\s*archive:[\s\S]*?\n\s{4}env:\s*\n([\s\S]*?)\n\s{4}steps:/)?.[1];
    expect(jobEnv).toBeDefined();
    expect(jobEnv).not.toContain('secrets.');
    expect(workflow).toContain('ZEN_ASC_API_KEY_BASE64: ${{ secrets.ZEN_ASC_API_KEY_BASE64 }}');
    expect(workflow).toContain('if: always()');
    expect(workflow).toContain('${{ runner.temp }}/zen-asc-individual-key.p8');
  });

  it('caches only unsigned native inputs, verified native output, and CocoaPods downloads', () => {
    expect(workflow).toContain('zen-ios-native-inputs-');
    expect(workflow).toContain('zen-ios-ghostty-output-');
    expect(workflow).toContain('zen-cocoapods-');
    expect(workflow).toContain("steps.ghostty-output-cache.outputs.cache-hit != 'true'");
    expect(workflow).toContain('run: ./scripts/verify-libghostty-ios.sh');
    for (const forbidden of ['.p12', '.mobileprovision', '.keychain', '.xcarchive', '.ipa']) {
      const cacheBlocks = [...workflow.matchAll(/uses: actions\/cache@v4[\s\S]*?(?=\n\s{6}- name:|$)/g)]
        .map((match) => match[0])
        .join('\n');
      expect(cacheBlocks).not.toContain(forbidden);
    }
  });
});

describe('iOS unsigned Simulator contract', () => {
  it('builds only the Apple Silicon architecture shipped by the native XCFramework', () => {
    expect(ciWorkflow).toContain('runs-on: macos-26');
    expect(ciWorkflow).toContain("-destination 'generic/platform=iOS Simulator'");
    expect(ciWorkflow).toContain('ARCHS=arm64');
    expect(nativeLibsWorkflow).toContain('runs-on: macos-26');
    expect(nativeLibsWorkflow).toContain("-destination 'generic/platform=iOS Simulator'");
    expect(nativeLibsWorkflow).toContain('ARCHS=arm64');
  });
});
