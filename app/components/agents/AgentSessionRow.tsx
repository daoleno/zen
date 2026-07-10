import React, { useMemo } from 'react';
import { ActivityIndicator, StyleSheet, Text, View } from 'react-native';
import {
  Colors,
  TypeScale,
  UiTextMetrics,
  type AgentStatus,
  useAppColors,
} from '../../constants/tokens';
import type { AgentKind } from '../../services/agentPresentation';
import type { TerminalFlavor } from '../../services/terminalFlavor';
import type { SessionPreviewTone } from '../../services/sessionPreview';
import { AnimatedPressable } from '../ui/AnimatedPressable';
import { AgentKindIcon } from '../terminal/AgentKindIcon';

interface AgentSessionRowProps {
  title: string;
  kind: AgentKind;
  terminalFlavor?: TerminalFlavor;
  preview: string;
  previewTone: SessionPreviewTone;
  previewPrefix?: string;
  timeLabel: string;
  status: AgentStatus;
  showBrainBadge?: boolean;
  onPress: () => void;
  onLongPress: () => void;
}

export function AgentSessionRow({
  title,
  kind,
  terminalFlavor,
  preview,
  previewTone,
  previewPrefix,
  timeLabel,
  status,
  showBrainBadge = false,
  onPress,
  onLongPress,
}: AgentSessionRowProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const previewColor = previewToneColor(previewTone, colors);
  const statusColor = agentStatusColor(status, colors);
  const statusLabel = agentStatusLabel(status);

  return (
    <AnimatedPressable
      style={[styles.row, status === 'running' && styles.rowActive]}
      preset="card"
      onPress={onPress}
      onLongPress={onLongPress}
      delayLongPress={400}
      accessibilityRole="button"
      accessibilityLabel={`${title}, ${statusLabel}, ${preview}${status === 'running' ? '' : `, ${timeLabel}`}`}
      accessibilityHint="Opens the terminal session"
      accessibilityState={{ busy: status === 'running' }}
    >
      <View style={styles.iconSlot}>
        <AgentKindIcon kind={kind} flavor={terminalFlavor} size={36} />
      </View>
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
        <Text style={styles.preview} numberOfLines={1} ellipsizeMode="middle">
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
        {status !== 'running' ? (
          <Text style={styles.time} numberOfLines={1}>
            {timeLabel}
          </Text>
        ) : null}
        <View style={styles.statusMeta}>
          {status === 'running' ? (
            <ActivityIndicator size="small" color={statusColor} style={styles.spinner} />
          ) : (
            <View style={[styles.statusDot, { backgroundColor: statusColor }]} />
          )}
          <Text style={[styles.statusText, { color: statusColor }]} numberOfLines={1}>
            {statusLabel}
          </Text>
        </View>
      </View>
      <View pointerEvents="none" style={styles.separator} />
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
      return colors.success;
    case 'muted':
      return colors.textTertiary;
    default:
      return colors.textSecondary;
  }
}

function agentStatusColor(status: AgentStatus, colors: typeof Colors): string {
  switch (status) {
    case 'failed':
      return colors.statusFailed;
    case 'blocked':
      return colors.statusBlocked;
    case 'running':
      return colors.statusRunning;
    case 'done':
      return colors.statusDone;
    default:
      return colors.statusUnknown;
  }
}

function agentStatusLabel(status: AgentStatus): string {
  switch (status) {
    case 'failed':
      return 'Failed';
    case 'blocked':
      return 'Blocked';
    case 'running':
      return 'Running';
    case 'done':
      return 'Done';
    default:
      return 'Unknown';
  }
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    iconSlot: {
      width: 44,
      height: 44,
      alignItems: 'center',
      justifyContent: 'center',
    },
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      minHeight: 72,
      gap: 10,
      paddingVertical: 9,
      paddingHorizontal: 16,
      backgroundColor: colors.bgPrimary,
    },
    rowActive: {
      backgroundColor: colors.accentSoft,
    },
    separator: {
      position: 'absolute',
      left: 70,
      right: 16,
      bottom: 0,
      height: StyleSheet.hairlineWidth,
      backgroundColor: colors.borderSubtle,
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
      ...UiTextMetrics,
      ...TypeScale.body,
      flexShrink: 1,
      minWidth: 0,
      color: colors.textPrimary,
    },
    preview: {
      ...UiTextMetrics,
      ...TypeScale.compact,
      marginTop: 1,
    },
    previewPrefix: {
      fontFamily: TypeScale.compact.fontFamily,
    },
    previewText: {
      fontFamily: TypeScale.compact.fontFamily,
    },
    meta: {
      minWidth: 60,
      maxWidth: 84,
      alignItems: 'flex-end',
      gap: 4,
    },
    time: {
      ...UiTextMetrics,
      ...TypeScale.caption,
      color: colors.textTertiary,
    },
    statusMeta: {
      flexDirection: 'row',
      alignItems: 'center',
      justifyContent: 'flex-end',
      gap: 4,
      minHeight: 16,
    },
    statusDot: {
      width: 6,
      height: 6,
      borderRadius: 3,
    },
    statusText: {
      ...UiTextMetrics,
      ...TypeScale.micro,
    },
    spinner: {
      transform: [{ scale: 0.45 }],
      width: 12,
      height: 12,
    },
    brainBadge: {
      minHeight: 22,
      paddingHorizontal: 6,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderStrong,
      borderRadius: 5,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: colors.surfaceSubtle,
    },
    brainBadgeText: {
      ...UiTextMetrics,
      ...TypeScale.micro,
      color: colors.textSecondary,
    },
  });
}
