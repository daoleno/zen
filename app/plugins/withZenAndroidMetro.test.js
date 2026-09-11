const { test, expect } = require('bun:test');
const fs = require('fs');
const os = require('os');
const path = require('path');
const {
  injectMetroGradle,
  injectStandaloneGradle,
  injectStandaloneMainApplication,
  writeMetroDebugSources,
} = require('./withZenAndroidRelease');

test('Metro launcher is explicitly opposite the standalone launcher', () => {
  const result = injectMetroGradle('android { defaultConfig { } }');
  expect(result).toContain("manifestPlaceholders.zenMetroLauncher = !(findProperty('zenStandalone') ?: 'false').toBoolean()");
  expect(result).toContain("manifestPlaceholders.zenStandaloneLauncher = (findProperty('zenStandalone') ?: 'false').toBoolean()");
  expect(injectMetroGradle(result)).toBe(result);
});

test('Metro and standalone Gradle fields compose in one defaultConfig', () => {
  const generated = injectMetroGradle(injectStandaloneGradle('android { defaultConfig { } }'));
  expect(generated).toContain('"boolean", "ZEN_STANDALONE"');
  expect(generated).toContain('zenMetroLauncher');
  expect(generated).toContain('zenStandaloneLauncher');
  expect(injectMetroGradle(generated)).toBe(generated);
});

test('Metro Gradle injection fails closed without defaultConfig', () => {
  expect(() => injectMetroGradle('android { }')).toThrow('defaultConfig block not found');
});

test('developer host factory stays explicit and off for standalone debug', () => {
  const generated = injectStandaloneMainApplication(
    'ExpoReactHostFactory.getDefaultReactHost(\n context = applicationContext\n)',
  );
  expect(generated).toContain('useDevSupport = BuildConfig.DEBUG && !BuildConfig.ZEN_STANDALONE,');
});

test('connection UI exists only in the debug source set and preserves deep links', () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'zen-metro-'));
  try {
    writeMetroDebugSources(root);
    expect(fs.existsSync(path.join(root, 'app/src/main'))).toBe(false);
    expect(fs.existsSync(path.join(root, 'app/src/release'))).toBe(false);
    const manifest = fs.readFileSync(path.join(root, 'app/src/debug/AndroidManifest.xml'), 'utf8');
    expect(manifest).toContain('${zenMetroLauncher}');
    expect(manifest).toContain('${zenStandaloneLauncher}');
    expect(manifest).toContain('.MetroConnectActivity');
    expect(manifest).not.toContain('android.intent.action.VIEW');
    const source = fs.readFileSync(path.join(root, 'app/src/debug/java/com/daoleno/zen/MetroConnectActivity.kt'), 'utf8');
    expect(source).toContain('!BuildConfig.DEBUG || BuildConfig.ZEN_STANDALONE');
    expect(source).toContain('packager-status:running');
    expect(source).toContain('debug_http_host');
    expect(source).toContain('PackagerConnectionSettings');
    expect(source).toContain('PreferenceManager.getDefaultSharedPreferences(applicationContext)');
    expect(source).toContain('.commit()');
    expect(source).toContain('Metro is unreachable');
    expect(source).toContain('Enter a Metro HTTP host and port.');
    expect(source).toContain('Retry');
    expect(source).not.toContain('adb');
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});
