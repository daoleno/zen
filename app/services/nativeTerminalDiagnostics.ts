import { Platform } from 'react-native';

export interface NativeTerminalCrashBreadcrumb {
  stage: 'before' | 'after';
  operation: string;
  detail: string;
  timestampMs: number;
  abi: string;
  model: string;
  brand: string;
  sdkInt: number;
}

type NativeTerminalModule = {
  getCrashBreadcrumb: () => NativeTerminalCrashBreadcrumb | null;
  clearCrashBreadcrumb: () => void;
};

function getNativeModule(): NativeTerminalModule | null {
  if (Platform.OS !== 'android') {
    return null;
  }

  try {
    return require('../modules/zen-terminal-vt/src') as NativeTerminalModule;
  } catch {
    return null;
  }
}

export function getNativeTerminalCrashBreadcrumb(): NativeTerminalCrashBreadcrumb | null {
  const mod = getNativeModule();
  if (!mod) {
    return null;
  }

  try {
    return mod.getCrashBreadcrumb();
  } catch {
    return null;
  }
}

export function clearNativeTerminalCrashBreadcrumb(): void {
  const mod = getNativeModule();
  if (!mod) {
    return;
  }

  try {
    mod.clearCrashBreadcrumb();
  } catch {
    // Ignore best-effort cleanup failures.
  }
}
