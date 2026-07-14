const { describe, expect, it } = require('bun:test');
const fs = require('fs');
const path = require('path');

const workflow = fs.readFileSync(
  path.join(__dirname, '..', '.github', 'workflows', 'release-artifacts.yml'),
  'utf8',
);

describe('release asset workflow contract', () => {
  it('uses published prerelease as the only publication path', () => {
    expect(workflow).toMatch(/release:\s*\n\s*types: \[published\]/);
    expect(workflow).not.toMatch(/push:\s*\n\s*tags:/);
    expect(workflow).not.toContain('inputs.publish');
    expect(workflow).toContain("if: ${{ github.event_name == 'release' }}");
    expect(workflow).toContain('release tag $RELEASE_TAG does not match tracked version');
    expect(workflow).toContain('checked-out release tag does not resolve to HEAD');
    expect(workflow).toContain('gh release upload "$TAG" "${assets[@]}" --clobber');
  });

  it('keeps manual dispatch artifact-only and caches no signing material or signed output', () => {
    expect(workflow).toContain('workflow_dispatch:');
    expect(workflow).toContain('cache: gradle');
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
