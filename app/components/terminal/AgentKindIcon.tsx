import React from 'react';
import { Ionicons } from '@expo/vector-icons';
import { StyleSheet, View } from 'react-native';
import { Claude, OpenAI } from '@lobehub/icons-rn';
import { Colors } from '../../constants/tokens';
import type { AgentKind } from '../../services/agentPresentation';

interface AgentKindIconProps {
  kind: AgentKind;
  size?: number;
}

export function AgentKindIcon({ kind, size = 16 }: AgentKindIconProps) {
  if (kind === 'claude') {
    return (
      <View style={[styles.frame, { width: size + 8, height: size + 8 }]}>
        <Claude.Color size={size} />
      </View>
    );
  }

  if (kind === 'codex') {
    return (
      <OpenAI.Avatar size={size + 8} />
    );
  }

  return (
    <View style={[styles.frame, styles.terminalFrame, { width: size + 8, height: size + 8 }]}>
      <Ionicons name="terminal-outline" size={size} color={Colors.textSecondary} />
    </View>
  );
}

const styles = StyleSheet.create({
  frame: {
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  terminalFrame: {
    backgroundColor: 'rgba(255,255,255,0.04)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.06)',
  },
});
