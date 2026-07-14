const { describe, expect, it } = require('bun:test');
const fs = require('fs');
const path = require('path');

const root = path.join(__dirname, '..');
const workflow = fs.readFileSync(path.join(root, '.github', 'workflows', 'ci.yml'), 'utf8');
const rootPackage = require(path.join(root, 'package.json'));

describe('ordinary CI contract', () => {
  it('keeps the offline installer suite in a bounded Linux job', () => {
    expect(rootPackage.scripts['installer:test']).toBe('./scripts/tests/install_test.sh');
    expect(workflow).toMatch(/installer:\s*[\s\S]*?runs-on: ubuntu-latest[\s\S]*?timeout-minutes: 5[\s\S]*?run: bun run installer:test/);
  });
});
