import React, { useMemo } from 'react';
import { ActivityIndicator, StyleSheet, Text, View } from 'react-native';
import { Colors, Typography, useAppColors } from '../../constants/tokens';
import type { SessionPreviewTone } from '../../services/sessionPreview';
import { AnimatedPressable } from '../ui/AnimatedPressable';
import { SessionAvatar } from '../ui/SessionAvatar';

interface AgentSessionRowProps {
  title: string;
  avatarSeed: string;
  preview: string;
  previewTone: SessionPreviewTone;
  previewPrefix?: string;
  timeLabel: string;
  timeActive?: boolean;
  running?: boolean;
  showBrainBadge?: boolean;
  active?: boolean;
  onPress: () => void;
  onLongPress: () => void;
}

export function AgentSessionRow({
  title,
  avatarSeed,
  preview,
  previewTone,
  previewPrefix,
  timeLabel,
  timeActive = false,
  running = false,
  showBrainBadge = false,
  active = false,
  onPress,
  onLongPress,
}: AgentSessionRowProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const previewColor = previewToneColor(previewTone, colors);

  return (
    <AnimatedPressable
      style={[styles.row, active && styles.rowActive]}
      preset="card"
      onPress={onPress}
      onLongPress={onLongPress}
      delayLongPress={400}
    >
      <SessionAvatar label={title} seed={avatarSeed} size={48} />
      <View style={styles.body}>
        <View style={styles.titleRow}>
          <Text style={styles.title} numberOfLines={1}>
            {title}
          </Text>
          {showBrainBadge ? (
            <View style={styles.brainBadge}>
              <Text style={styles.brainBadgeText}>Brain</Text>
            </View>
          ) : null}
        </View>
        <Text style={styles.preview} numberOfLines={1}>
          {previewPrefix ? (
            <Text style={[styles.previewPrefix, { color: previewColor }]}>
              {previewPrefix}
            </Text>
          ) : null}
          <Text style={[styles.previewText, { color: previewColor }]}>
            {preview}
          </Text>
        </Text>
      </View>
      <View style={styles.meta}>
        <Text style={[styles.time, timeActive && styles.timeActive]}>
          {timeLabel}
        </Text>
        {running ? (
          <View style={styles.runningBadge}>
            <ActivityIndicator size="small" color={colors.accent} style={styles.spinner} />
          </View>
        ) : null}
      </View>
    </AnimatedPressable>
  );
}

function previewToneColor(tone: SessionPreviewTone, colors: typeof Colors): string {
  switch (tone) {
    case 'accent':
      return colors.accent;
    case 'danger':
      return colors.dangerText;
    case 'success':
      return colors.statusRunning;
    case 'muted':
      return colors.textTertiary;
    default:
      return colors.textSecondary;
  }
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      minHeight: 68,
      gap: 12,
      paddingVertical: 9,
      paddingHorizontal: 16,
      backgroundColor: colors.bgPrimary,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    rowActive: {
      backgroundColor: 'rgba(42,171,238,0.08)',
    },
    body: {
      flex: 1,
      minWidth: 0,
      justifyContent: 'center',
    },
    titleRow: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 7,
      minWidth: 0,
    },
    title: {
      flexShrink: 1,
      minWidth: 0,
      color: colors.textPrimary,
      fontSize: 15.5,
      lineHeight: 22,
      fontFamily: Typography.uiFontMedium,
    },
    preview: {
      marginTop: 1,
      fontSize: 13,
      lineHeight: 18,
      fontFamily: Typography.uiFont,
    },
    previewPrefix: {
      fontFamily: Typography.uiFont,
    },
    previewText: {
      fontFamily: Typography.uiFont,
    },
    meta: {
      minWidth: 44,
      alignItems: 'flex-end',
      gap: 6,
    },
    time: {
      color: colors.textTertiary,
      fontFamily: Typography.uiFont,
      fontSize: 11,
      lineHeight: 14,
    },
    timeActive: {
      color: colors.accent,
      fontFamily: Typography.uiFontMedium,
    },
    runningBadge: {
      width: 18,
      height: 18,
      borderRadius: 9,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: colors.accentSoft,
    },
    spinner: {
      transform: [{ scale: 0.45 }],
    },
    brainBadge: {
      height: 18,
      paddingHorizontal: 6,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderStrong,
      borderRadius: 6,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: colors.surfaceSubtle,
    },
    brainBadgeText: {
      fontSize: 10,
      lineHeight: 12,
      fontFamily: Typography.uiFontMedium,
      color: colors.textSecondary,
      includeFontPadding: false,
    },
  });
}