/**
 * TerminalSurface selects the Android libghostty-backed terminal surface
 * lazily so iOS/web never evaluate the Android-only native module.
 */
import React from 'react';
import { Platform } from 'react-native';
import type { TerminalSurfaceHandle, TerminalSurfaceProps } from './TerminalSurface.types';

export type { TerminalSurfaceHandle, TerminalSurfaceProps } from './TerminalSurface.types';

type TerminalSurfaceComponent = React.ForwardRefExoticComponent<
  TerminalSurfaceProps & React.RefAttributes<TerminalSurfaceHandle>
>;

let _resolved: TerminalSurfaceComponent | null = null;

function getImpl() {
  if (!_resolved) {
    if (Platform.OS === 'android') {
      _resolved = require('./TerminalSurfaceGhosttyWebView').TerminalSurfaceGhosttyWebView as TerminalSurfaceComponent;
    } else {
      _resolved = require('./TerminalSurfaceUnsupported').TerminalSurfaceUnsupported as TerminalSurfaceComponent;
    }
  }
  return _resolved!;
}

const TerminalSurfaceImpl = React.forwardRef<TerminalSurfaceHandle, TerminalSurfaceProps>((props, ref) => {
  const Impl = getImpl();
  return <Impl {...props} ref={ref} />;
});

export const TerminalSurface = TerminalSurfaceImpl;
