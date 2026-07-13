// @ts-nocheck
import { describe, expect, it } from 'bun:test';
import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';

const moduleRoot = import.meta.dir;
const moduleConfig = JSON.parse(readFileSync(join(moduleRoot, 'expo-module.config.json'), 'utf8'));

describe('zen-terminal-vt Apple contract', () => {
  it('registers the Swift module through Expo autolinking metadata', () => {
    expect(moduleConfig.platforms).toContain('apple');
    expect(moduleConfig.apple.modules).toContain('ZenTerminalVtModule');
    expect(moduleConfig.apple.podspecPath).toContain('ZenTerminalVt.podspec');
  });

  it('ships the bridge sources and build contract', () => {
    for (const path of [
      'ZenTerminalVt.podspec',
      'ios/ZenTerminalVtBridge.h',
      'ios/ZenTerminalVtBridge.mm',
      'ios/ZenTerminalVtModule.swift',
    ]) {
      expect(existsSync(join(moduleRoot, path))).toBe(true);
    }
  });
});
