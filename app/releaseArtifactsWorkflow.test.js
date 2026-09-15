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
const nativeVerifier = fs.readFileSync(
  path.join(__dirname, '..', 'scripts', 'verify-libghostty.sh'),
  'utf8',
);
const appPackage = fs.readFileSync(path.join(__dirname, 'package.json'), 'utf8');

describe('release asset workflow contract', () => {
  it('prepares native release inputs before compilation in release and ordinary CI', () => {
    const ci = fs.readFileSync(path.join(__dirname, '..', '.github', 'workflows', 'ci.yml'), 'utf8');
    const apk = fs.readFileSync(path.join(__dirname, '..', 'scripts', 'android-release-apk.sh'), 'utf8');
    const daemon = workflow.slice(workflow.indexOf('  daemon:'), workflow.indexOf('  android:'));
    for (const pkg of ['libgtk-3-dev', 'libglib2.0-dev', 'libgstreamer1.0-dev', 'libgstreamer-plugins-base1.0-dev', 'libx11-dev', 'libxtst-dev']) {
      expect(daemon).toContain(pkg);
    }
    expect(daemon.indexOf('pkg-config --exists')).toBeLessThan(daemon.indexOf('run: ./scripts/build-daemon-linux.sh'));
    for (const [source, compile] of [[apk, 'if [[ $SKIP_PREBUILD -eq 0 ]]'], [ci, '- name: Generate Android project']]) {
      const fetch = source.indexOf('/scripts/fetch-moonlight-common-c.sh');
      const crypto = source.indexOf('/scripts/build-openssl-android.sh');
      expect(fetch).toBeGreaterThan(0);
      expect(crypto).toBeGreaterThan(fetch);
      expect(crypto).toBeLessThan(source.indexOf(compile));
      expect(source).toContain('--abi arm64-v8a --api 24');
      expect(source).toContain('/ndk/27.1.12297006');
    }
    expect(ci).toContain(':app:assembleRelease');
    expect(ci).toContain('./scripts/build-daemon-linux.sh --out-dir');
    for (const source of [workflow, ci]) {
      expect(source).toMatch(/uses: android-actions\/setup-android@v3\s+with:\s+packages: platform-tools/);
    }
  });
  it('rejects RN-normalized asset collisions before native and signed builds', () => {
    const gate = workflow.indexOf('run: bun test androidAssetNames.test.js');
    expect(gate).toBeGreaterThan(0);
    expect(gate).toBeLessThan(workflow.indexOf('- name: Restore pinned Zig and Ghostty source caches'));
    const preparation = fs.readFileSync(
      path.join(__dirname, '..', '.github', 'workflows', 'release-next-beta.yml'), 'utf8',
    );
    expect(preparation).toContain('androidAssetNames.test.js');
    expect(preparation.indexOf('androidAssetNames.test.js')).toBeLessThan(
      preparation.indexOf('- name: Commit and annotate exact prepared release'),
    );
  });
  it('uses immutable stable or beta tag pushes as the only automatic publication path', () => {
    expect(workflow).toMatch(
      /push:\s*\n\s*tags:\s*\n\s*- "v\*\.\*\.\*"\s*\n\s*- "v\*\.\*\.\*-beta\.\*"/,
    );
    expect(workflow).not.toMatch(/release:\s*\n\s*types:/);
    expect(workflow).toContain('type: boolean');
    expect(workflow).toContain("needs.validate.outputs.publish == 'true'");
    expect(workflow).toContain('./scripts/verify-release-identity.sh --tag');
    expect(identityVerifier).toContain('release tag $RELEASE_TAG does not match tracked version');
    expect(identityVerifier).toContain('checked-out release tag does not resolve to HEAD');
    expect(identityVerifier).toContain('release tag commit is not on origin/main');
    expect(workflow).toContain('RELEASE_IS_PRERELEASE=true');
    expect(workflow).toContain('RELEASE_IS_PRERELEASE=false');
    expect(workflow).toContain('--prerelease="$RELEASE_IS_PRERELEASE"');
    expect(workflow).toContain('--latest="$RELEASE_IS_STABLE"');
    expect(workflow).toContain('gh release create "$TAG" --verify-tag --draft');
    expect(workflow).toContain('gh release upload "$TAG" "${assets[@]}" --clobber');
    expect(workflow).toContain('gh release edit "$TAG" --draft=false');
  });

  it('builds daemon and Android in parallel before deterministic signed aggregation', () => {
    expect(workflow).toContain('daemon:');
    expect(workflow).toContain('android:');
    expect(workflow).toContain('needs: [validate, daemon, android]');
    expect(workflow).toContain("grep -Eq 'ELF 64-bit.*(x86-64|x86_64)'");
    expect(workflow).toContain("grep -Eq 'Mach-O 64-bit.*arm64'");
    expect(workflow).toContain('--out-dir "$GITHUB_WORKSPACE/dist-download/staging/bin"');
    expect(workflow).toContain('./scripts/stage-release.sh --skip-build --apk "$APK"');
    expect(workflow).toContain('SOURCE_DATE_EPOCH');
  });

  it('keeps recovery reviewed and caches no signing material or signed output', () => {
    expect(workflow).toContain('workflow_dispatch:');
    expect(workflow).toContain('publish:');
    expect(workflow).toContain('cache: gradle');
    expect(appPackage).toContain('--build-cache');
    expect(workflow).toContain('-Dorg.gradle.jvmargs=-Xmx6g');
    expect(workflow).toContain('zen-android-native-inputs-');
    expect(workflow).toContain('zen-android-ghostty-output-v2-arm64-');
    expect(workflow).toContain('app/modules/zen-terminal-vt/android/src/main/cpp/ghostty');
    expect(workflow).toContain('app/modules/zen-terminal-vt/patches/android/**');
    expect(workflow).toContain('scripts/verify-android-native-symbols.py');
    expect(workflow).toContain("steps.ghostty-output-cache.outputs.cache-hit != 'true'");
    expect(workflow).toContain('run: ./scripts/verify-libghostty.sh --release');
    expect(nativeVerifier).toContain('bad "missing pinned header $HEADERS_DIR/vt.h"');
    const cacheBlocks = [...workflow.matchAll(/uses: actions\/cache@v4[\s\S]*?(?=\n\s{6}- name:|$)/g)]
      .map((match) => match[0])
      .join('\n');
    for (const forbidden of ['keystore', '.p12', '.jks', '.apk', 'dist-download']) {
      expect(cacheBlocks.toLowerCase()).not.toContain(forbidden);
    }
  });
});
