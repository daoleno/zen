import React from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { formatChatBubbleTime } from '../../constants/telegramPresentation';
import { TouchTarget, Typography, useAppTheme } from '../../constants/tokens';
import { PendingMessageLifecycleLabel } from './PendingMessageLifecycleLabel';
import { PENDING_MESSAGE_RETRY_ACCESSIBILITY_LABEL } from './pendingUserMessageLifecycle';
import { chromeTint } from './composerMaterial';

/** 26 pt capsule + vertical hitSlop = platform touch target (44 pt / 48 dp). */
const RETRY_CAPSULE_HEIGHT = 26;
const RETRY_HIT_SLOP = {
  top: (TouchTarget - RETRY_CAPSULE_HEIGHT) / 2,
  bottom: (TouchTarget - RETRY_CAPSULE_HEIGHT) / 2,
  left: 6,
  right: 6,
};

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
  const retryInk = failureColor || timeColor;

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
            hitSlop={RETRY_HIT_SLOP}
            onPress={onRetry}
            style={({ pressed }) => [
              styles.retryCapsule,
              {
                backgroundColor: chromeTint(retryInk, 0.16, 'transparent'),
                opacity: pressed ? 0.64 : 1,
              },
            ]}
          >
            <Ionicons name="refresh" size={12} color={retryInk} />
            <Text style={[styles.retry, { color: retryInk }]}>Retry</Text>
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
    gap: 6,
    paddingTop: 2,
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
  retryCapsule: {
    height: RETRY_CAPSULE_HEIGHT,
    borderRadius: RETRY_CAPSULE_HEIGHT / 2,
    paddingHorizontal: 10,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 4,
  },
  retry: {
    fontFamily: Typography.uiFontMedium,
    fontSize: 12,
    lineHeight: 15,
    includeFontPadding: false,
  },
});
