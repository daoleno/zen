import React, { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Typography } from '../../constants/tokens';
import {
  DefaultTerminalThemeName,
  resolveTerminalTheme,
  TerminalThemeName,
} from '../../constants/terminalThemes';

interface TerminalPreviewProps {
  lines: string[];
  themeName?: TerminalThemeName;
}

const PREVIEW_LINE_COUNT = 12;

function formatPreviewLines(lines: string[]): string {
  const visibleLines = lines
    .map((line) => line.replace(/\t/g, '  '))
    .slice(-PREVIEW_LINE_COUNT);

  if (visibleLines.length === 0) {
    return '$ zen';
  }

  return visibleLines.join('\n');
}

export function TerminalPreview({
  lines,
  themeName = DefaultTerminalThemeName,
}: TerminalPreviewProps) {
  const theme = useMemo(() => resolveTerminalTheme(themeName), [themeName]);
  const previewText = useMemo(() => formatPreviewLines(lines), [lines]);
  const borderColor = useMemo(() => withAlpha(theme.foreground, 0.12), [theme.foreground]);

  return (
    <View
      style={[
        styles.container,
        {
          backgroundColor: theme.background,
          borderColor,
        },
      ]}
    >
      <Text
        numberOfLines={PREVIEW_LINE_COUNT}
        style={[
          styles.previewText,
          {
            color: theme.foreground,
            backgroundColor: theme.background,
          },
        ]}
      >
        {previewText}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    borderRadius: 8,
    overflow: 'hidden',
    paddingHorizontal: 10,
    paddingVertical: 12,
    borderWidth: 1,
  },
  previewText: {
    flex: 1,
    fontFamily: Typography.terminalFont,
    fontSize: 9,
    lineHeight: 13,
    includeFontPadding: false,
  },
});

function withAlpha(hex: string, alpha: number): string {
  const normalized = hex.trim().replace('#', '');
  if (!/^[0-9a-fA-F]{6}$/.test(normalized)) {
    return hex;
  }

  const red = Number.parseInt(normalized.slice(0, 2), 16);
  const green = Number.parseInt(normalized.slice(2, 4), 16);
  const blue = Number.parseInt(normalized.slice(4, 6), 16);
  return `rgba(${red}, ${green}, ${blue}, ${Math.min(Math.max(alpha, 0), 1)})`;
}
