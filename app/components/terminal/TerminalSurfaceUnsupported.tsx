import React, { forwardRef, useImperativeHandle, useMemo } from 'react';
import { Platform, StyleSheet, View } from 'react-native';
import { Colors, useAppColors } from '../../constants/tokens';
import type { TerminalSurfaceHandle, TerminalSurfaceProps } from './TerminalSurface.types';
import { AppText } from '../ui';
import { getTerminalCapabilityPresentation } from '../../services/terminalCapabilities';

export const TerminalSurfaceUnsupported = forwardRef<TerminalSurfaceHandle, TerminalSurfaceProps>(({
  theme,
}, ref) => {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const presentation = getTerminalCapabilityPresentation(Platform.OS);

  useImperativeHandle(ref, () => ({
    sendInput() {},
    focus() {},
    blur() {},
    wakeRenderer() {},
    resumeInput() {},
    scrollToBottom() {},
  }), []);

  return (
    <View style={[styles.container, { backgroundColor: theme.background }]}>
      <View style={[styles.card, { borderColor: colors.borderStrong }]}>
        <AppText variant="title" style={{ color: theme.foreground }}>
          {presentation.title}
        </AppText>
        <AppText variant="caption" style={[styles.body, { color: theme.foreground }]}>
          {presentation.detail}
        </AppText>
        <AppText variant="caption" tone="secondary" style={styles.caption}>
          {presentation.hint}
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
