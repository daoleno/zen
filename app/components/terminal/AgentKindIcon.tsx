import React, { useMemo } from 'react';
import { Ionicons } from '@expo/vector-icons';
import { StyleSheet, View } from 'react-native';
import { Claude, OpenAI } from '@lobehub/icons-rn';
import { Colors, useAppColors, useAppTheme } from '../../constants/tokens';
import type { ResolvedZenTheme } from '../../theme';
import { createThemedSurfaces } from '../../constants/themedSurfaces';
import type { AgentKind } from '../../services/agentPresentation';

interface AgentKindIconProps {
  kind: AgentKind;
  size?: number;
}

export function AgentKindIcon({ kind, size = 16 }: AgentKindIconProps) {
  const colors = useAppColors();
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const frameSize = size + 8;
  if (kind === 'claude') {
    return (
      <View style={[styles.frame, { width: frameSize, height: frameSize }]}>
        <Claude.Color size={size} />
      </View>
    );
  }

  if (kind === 'codex') {
    return (
      <View style={[styles.frame, { width: frameSize, height: frameSize }]}>
        <OpenAI.Avatar size={size} />
      </View>
    );
  }

  if (kind === 'grok') {
    return (
      <View style={[styles.frame, styles.grokFrame, { width: frameSize, height: frameSize }]}>
        <Ionicons name="sparkles" size={size} color={colors.textPrimary} />
      </View>
    );
  }

  return (
    <View style={[styles.frame, styles.terminalFrame, { width: frameSize, height: frameSize }]}>
      <Ionicons name="terminal-outline" size={size} color={colors.textSecondary} />
    </View>
  );
}

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  const { subtle: themedSubtle, border: themedBorder } = createThemedSurfaces(theme);

  return StyleSheet.create({
  frame: {
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
  },
  terminalFrame: {
    backgroundColor: themedSubtle,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: themedBorder,
  },
  grokFrame: {
    backgroundColor: themedSubtle,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: themedBorder,
  },
  });
}
