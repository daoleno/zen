const { describe, expect, it } = require('bun:test');
const fs = require('fs');
const path = require('path');

const workflow = fs.readFileSync(
  path.join(__dirname, '..', '.github', 'workflows', 'release-artifacts.yml'),
  'utf8',
);
const identityVerifier = fs.readFileSync(
  path.join(__dirname, '..', 'scripts', 'verify-release-identity.sh'),
  'utf8',
);
const appPackage = fs.readFileSync(path.join(__dirname, 'package.json'), 'utf8');

describe('release asset workflow contract', () => {
  it('uses an immutable beta tag push as the only automatic publication path', () => {
    expect(workflow).toMatch(/push:\s*\n\s*tags:\s*\n\s*- "v\*\.\*\.\*-beta\.\*"/);
    expect(workflow).not.toMatch(/release:\s*\n\s*types:/);
    expect(workflow).toContain('type: boolean');
    expect(workflow).toContain("needs.validate.outputs.publish == 'true'");
    expect(workflow).toContain('./scripts/verify-release-identity.sh --tag');
    expect(identityVerifier).toContain('release tag $RELEASE_TAG does not match tracked version');
    expect(identityVerifier).toContain('checked-out release tag does not resolve to HEAD');
    expect(identityVerifier).toContain('release tag commit is not on origin/main');
    expect(workflow).toContain('gh release create "$TAG" --verify-tag --draft --prerelease');
    expect(workflow).toContain('gh release upload "$TAG" "${assets[@]}" --clobber');
    expect(workflow).toContain('gh release edit "$TAG" --draft=false --prerelease');
  });

  it('builds daemon and Android in parallel before deterministic signed aggregation', () => {
    expect(workflow).toContain('daemon:');
    expect(workflow).toContain('android:');
    expect(workflow).toContain('needs: [validate, daemon, android]');
    expect(workflow).toContain("grep -Eq 'ELF 64-bit.*(x86-64|x86_64)'");
    expect(workflow).toContain("grep -Eq 'Mach-O 64-bit.*arm64'");
    expect(workflow).toContain('./scripts/stage-release.sh --skip-build --apk "$APK"');
    expect(workflow).toContain('SOURCE_DATE_EPOCH');
  });

  it('keeps recovery reviewed and caches no signing material or signed output', () => {
    expect(workflow).toContain('workflow_dispatch:');
    expect(workflow).toContain('publish:');
    expect(workflow).toContain('cache: gradle');
    expect(appPackage).toContain('--build-cache');
    expect(workflow).toContain('zen-android-native-inputs-');
    expect(workflow).toContain('zen-android-ghostty-output-arm64-');
    expect(workflow).toContain("steps.ghostty-output-cache.outputs.cache-hit != 'true'");
    expect(workflow).toContain('run: ./scripts/verify-libghostty.sh --release');
    const cacheBlocks = [...workflow.matchAll(/uses: actions\/cache@v4[\s\S]*?(?=\n\s{6}- name:|$)/g)]
      .map((match) => match[0])
      .join('\n');
    for (const forbidden of ['keystore', '.p12', '.jks', '.apk', 'dist-download']) {
      expect(cacheBlocks.toLowerCase()).not.toContain(forbidden);
    }
  });
});
