/**
 * TerminalSurface selects the Android libghostty-backed terminal surface
 * lazily so iOS/web never evaluate the Android-only native module.
 */
import React from 'react';
import { Platform } from 'react-native';

export type { TerminalSurfaceHandle } from './TerminalSurfaceGhosttyWebView';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
let _resolved: React.ComponentType<any> | null = null;

function getImpl() {
  if (!_resolved) {
    if (Platform.OS === 'android') {
      _resolved = require('./TerminalSurfaceGhosttyWebView').TerminalSurfaceGhosttyWebView;
    } else {
      _resolved = require('./TerminalSurfaceWebView').TerminalSurfaceWebView;
    }
  }
  return _resolved!;
}

const TerminalSurfaceImpl = React.forwardRef((props: any, ref: any) => {
  const Impl = getImpl();
  return <Impl {...props} ref={ref} />;
});

export const TerminalSurface = React.memo(TerminalSurfaceImpl, (prev: any, next: any) => (
  prev.serverId === next.serverId &&
  prev.targetId === next.targetId &&
  prev.backend === next.backend &&
  prev.themeName === next.themeName &&
  prev.themeOverrides === next.themeOverrides &&
  prev.ctrlArmed === next.ctrlArmed
));
