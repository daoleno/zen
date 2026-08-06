import React from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { formatChatBubbleTime } from '../../constants/telegramPresentation';
import { Typography, useAppTheme } from '../../constants/tokens';
import { PendingMessageLifecycleLabel } from './PendingMessageLifecycleLabel';
import { PENDING_MESSAGE_RETRY_ACCESSIBILITY_LABEL } from './pendingUserMessageLifecycle';

interface MessageBubbleFooterProps {
  timestamp?: string;
  tone?: 'sent' | 'received';
  lifecycleLabel?: string;
  failureMessage?: string;
  failureColor?: string;
  onRetry?: () => void;
}

export function MessageBubbleFooter({
  timestamp,
  tone = 'received',
  lifecycleLabel,
  failureMessage,
  failureColor,
  onRetry,
}: MessageBubbleFooterProps) {
  const { theme } = useAppTheme();
  const hasLifecycleLabel = Boolean(lifecycleLabel);
  // Pending never injects status text. Timestamps stay available so enabling
  // them does not shift geometry when the optimistic row becomes durable.
  const label = hasLifecycleLabel
    ? lifecycleLabel
    : formatChatBubbleTime(timestamp);
  const timeColor =
    tone === 'sent'
      ? theme.chat.sentTimestamp
      : theme.chat.receivedTimestamp;
  if (!label && !failureMessage && !onRetry) {
    return null;
  }

  return (
    <View style={styles.stack}>
      {failureMessage ? (
        <Text
          accessibilityLiveRegion="polite"
          accessibilityRole="text"
          style={[styles.failure, { color: failureColor || timeColor }]}
        >
          {failureMessage}
        </Text>
      ) : null}
      <View style={styles.row}>
        {hasLifecycleLabel ? (
          <PendingMessageLifecycleLabel
            label={label!}
            accessibilityLabel={label}
            color={failureMessage ? failureColor || timeColor : timeColor}
          />
        ) : label ? (
          <Text style={[styles.time, { color: timeColor }]}>
            {label}
          </Text>
        ) : null}
        {onRetry ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={PENDING_MESSAGE_RETRY_ACCESSIBILITY_LABEL}
            hitSlop={8}
            onPress={onRetry}
          >
            <Text style={[styles.retry, { color: failureColor || timeColor }]}>Retry</Text>
          </Pressable>
        ) : null}
      </View>
    </View>
  );
}

const styles = StyleSheet.create({
  stack: {
    alignSelf: 'stretch',
    alignItems: 'flex-end',
    marginTop: 2,
  },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'flex-end',
    alignSelf: 'flex-end',
    gap: 4,
    paddingTop: 1,
  },
  time: {
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 14,
    includeFontPadding: false,
  },
  failure: {
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 14,
    includeFontPadding: false,
    textAlign: 'right',
  },
  retry: {
    fontFamily: Typography.uiFont,
    fontSize: 11,
    lineHeight: 14,
    includeFontPadding: false,
    textDecorationLine: 'underline',
  },
});
