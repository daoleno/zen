import React, { forwardRef, useImperativeHandle, useMemo } from 'react';
import { StyleSheet, View } from 'react-native';
import { Colors, useAppColors } from '../../constants/tokens';
import {
  DefaultTerminalThemeName,
  resolveTerminalTheme,
} from '../../constants/terminalThemes';
import type { TerminalSurfaceHandle, TerminalSurfaceProps } from './TerminalSurface.types';
import { AppText } from '../ui';

export const TerminalSurfaceUnsupported = forwardRef<TerminalSurfaceHandle, TerminalSurfaceProps>(({
  themeName = DefaultTerminalThemeName,
  themeOverrides,
}, ref) => {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const theme = useMemo(
    () => resolveTerminalTheme(themeName, themeOverrides),
    [themeName, themeOverrides],
  );

  useImperativeHandle(ref, () => ({
    sendInput() {},
    focus() {},
    blur() {},
    resumeInput() {},
    scrollToBottom() {},
  }), []);

  return (
    <View style={[styles.container, { backgroundColor: theme.background }]}>
      <View style={[styles.card, { borderColor: colors.borderStrong }]}>
        <AppText variant="title" style={{ color: theme.foreground }}>
          Terminal unavailable on this platform
        </AppText>
        <AppText variant="caption" style={[styles.body, { color: theme.foreground }]}>
          This build only ships the libghostty-backed terminal on Android.
        </AppText>
        <AppText variant="caption" tone="secondary" style={styles.caption}>
          Web and iOS stay disabled until a libghostty-backed surface exists there.
        </AppText>
      </View>
    </View>
  );
});

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  container: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
    padding: 24,
  },
  card: {
    width: '100%',
    maxWidth: 420,
    paddingHorizontal: 18,
    paddingVertical: 16,
    borderRadius: 16,
    borderWidth: 1,
    backgroundColor: colors.bgSurface,
  },
  body: {
    marginTop: 8,
    opacity: 0.86,
  },
  caption: {
    marginTop: 8,
  },
  });
}
