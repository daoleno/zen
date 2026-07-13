// @ts-nocheck
import { describe, expect, it } from 'bun:test';
import { terminalWebViewBaseUrl } from './terminalWebViewSource';

describe('terminalWebViewBaseUrl', () => {
  it('lets WKWebView read a bundled font from its containing directory', () => {
    expect(
      terminalWebViewBaseUrl('file:///data/app/MapleMono-CN-Regular.ttf', 'ios'),
    ).toBe('file:///data/app/');
  });

  it('keeps the isolated synthetic origin on Android and remote dev assets', () => {
    expect(
      terminalWebViewBaseUrl('file:///data/app/MapleMono-CN-Regular.ttf', 'android'),
    ).toBe('https://zen.local/');
    expect(
      terminalWebViewBaseUrl('http://localhost:8081/assets/font.ttf', 'ios'),
    ).toBe('https://zen.local/');
  });
});
