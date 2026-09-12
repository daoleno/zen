const { expect, test } = require('bun:test');
const { injectPackaging } = require('./withZenAndroidPackaging');

test('injects the BouncyCastle packaging excludes once', () => {
  const input = 'android {\n}\n';
  const output = injectPackaging(input);
  expect(output).toContain('// @generated begin zen-android-packaging');
  expect(output).toContain('excludes += ["META-INF/LICENSE.md"');
  expect(output).toContain('"META-INF/versions/9/OSGI-INF/MANIFEST.MF"');
  expect(output).toContain('"META-INF/versions/11/OSGI-INF/MANIFEST.MF"');
  expect(injectPackaging(output)).toBe(output);
});
