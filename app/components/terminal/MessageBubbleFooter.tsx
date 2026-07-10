import React from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { formatChatBubbleTime } from '../../constants/telegramPresentation';
import { Typography, useAppTheme } from '../../constants/tokens';
import { PendingMessageLifecycleLabel } from './PendingMessageLifecycleLabel';

interface MessageBubbleFooterProps {
  timestamp?: string;
  tone?: 'sent' | 'received';
  pending?: boolean;
  lifecycleLabel?: string;
  lifecycleAccessibilityLabel?: string;
}

export function MessageBubbleFooter({
  timestamp,
  tone = 'received',
  pending = false,
  lifecycleLabel,
  lifecycleAccessibilityLabel,
}: MessageBubbleFooterProps) {
  const { theme } = useAppTheme();
  const label = lifecycleLabel
    ? lifecycleLabel
    : pending
      ? 'Sending'
      : formatChatBubbleTime(timestamp);
  if (!label) {
    return null;
  }

  const timeColor =
    tone === 'sent'
      ? theme.chat.sentTimestamp
      : theme.chat.receivedTimestamp;
  const showLifecycle = Boolean(lifecycleLabel || pending);

  return (
    <View style={styles.row}>
      {showLifecycle ? (
        <PendingMessageLifecycleLabel
          label={label}
          accessibilityLabel={lifecycleAccessibilityLabel || label}
          color={timeColor}
        />
      ) : (
        <Text style={[styles.time, { color: timeColor }]}>
          {label}
        </Text>
      )}
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
