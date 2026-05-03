import React, { forwardRef, useImperativeHandle, useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Colors, Typography, useAppColors } from '../../constants/tokens';
import {
  DefaultTerminalThemeName,
  resolveTerminalTheme,
} from '../../constants/terminalThemes';
import type { TerminalSurfaceHandle, TerminalSurfaceProps } from './TerminalSurface.types';

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
        <Text style={[styles.title, { color: theme.foreground }]}>
          Terminal unavailable on this platform
        </Text>
        <Text style={[styles.body, { color: theme.foreground }]}>
          This build only ships the libghostty-backed terminal on Android.
        </Text>
        <Text style={[styles.caption, { color: colors.textSecondary }]}>
          Web and iOS stay disabled until a libghostty-backed surface exists there.
        </Text>
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
  title: {
    fontSize: 16,
    lineHeight: 22,
    fontFamily: Typography.uiFontMedium,
  },
  body: {
    marginTop: 8,
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
    opacity: 0.86,
  },
  caption: {
    marginTop: 8,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  });
}
