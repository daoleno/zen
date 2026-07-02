import React from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { formatChatBubbleTime } from '../../constants/telegramPresentation';
import { Typography, useAppTheme } from '../../constants/tokens';

interface MessageBubbleFooterProps {
  timestamp?: string;
  tone?: 'sent' | 'received';
  pending?: boolean;
}

export function MessageBubbleFooter({
  timestamp,
  tone = 'received',
  pending = false,
}: MessageBubbleFooterProps) {
  const { theme } = useAppTheme();
  const label = pending
    ? 'sending'
    : formatChatBubbleTime(timestamp);
  if (!label) {
    return null;
  }

  const timeColor =
    tone === 'sent'
      ? theme.chat.sentTimestamp
      : theme.chat.receivedTimestamp;

  return (
    <View style={styles.row}>
      <Text style={[styles.time, { color: timeColor }]}>
        {label}
      </Text>
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
});