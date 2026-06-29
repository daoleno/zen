import React from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Typography, useAppTheme } from '../../constants/tokens';

interface MessageBubbleFooterProps {
  timestamp?: string;
  tone?: 'sent' | 'received';
  showChecks?: boolean;
  pending?: boolean;
}

export function MessageBubbleFooter({
  timestamp,
  tone = 'received',
  showChecks = false,
  pending = false,
}: MessageBubbleFooterProps) {
  const { theme } = useAppTheme();
  const label = timestamp?.trim() || (pending ? 'sending' : '');
  if (!label && !showChecks) {
    return null;
  }

  const timeColor =
    tone === 'sent'
      ? theme.chat.sentTimestamp
      : theme.chat.receivedTimestamp;

  return (
    <View style={styles.row}>
      {label ? (
        <Text style={[styles.time, { color: timeColor }]}>
          {label}
        </Text>
      ) : null}
      {showChecks ? (
        <Text style={[styles.checks, { color: timeColor }]}>
          ✓✓
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'flex-end',
    alignSelf: 'flex-end',
    gap: 4,
    marginTop: 2,
    paddingTop: 1,
  },
  time: {
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 14,
    includeFontPadding: false,
  },
  checks: {
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 14,
    includeFontPadding: false,
  },
});