import React, { useMemo } from 'react';
import { Ionicons } from '@expo/vector-icons';
import { StyleSheet, View } from 'react-native';
import { Claude, OpenAI } from '@lobehub/icons-rn';
import { Colors, useAppColors } from '../../constants/tokens';
import type { AgentKind } from '../../services/agentPresentation';

interface AgentKindIconProps {
  kind: AgentKind;
  size?: number;
}

export function AgentKindIcon({ kind, size = 16 }: AgentKindIconProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
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

  return (
    <View style={[styles.frame, styles.terminalFrame, { width: frameSize, height: frameSize }]}>
      <Ionicons name="terminal-outline" size={size} color={colors.textSecondary} />
    </View>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  frame: {
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  terminalFrame: {
    backgroundColor: colors.surfaceSubtle,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
  },
  });
}
