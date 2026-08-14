import React, { useMemo } from 'react';
import { ActivityIndicator, StyleSheet, Text, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
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
import {
  agentStatusLabel,
  isAgentActivelyRunning,
} from '../../services/agentStatusPresentation';
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
  /** Selection mode: taps toggle selection instead of opening the Session. */
  selectionMode?: boolean;
  selected?: boolean;
  /** Row cannot be terminated (e.g. daemon offline): disabled inside selection. */
  selectionDisabled?: boolean;
  onToggleSelection?: () => void;
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
  selectionMode = false,
  selected = false,
  selectionDisabled = false,
  onToggleSelection,
}: AgentSessionRowProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const previewColor = previewToneColor(previewTone, colors);
  const statusColor = agentStatusColor(status, colors);
  const statusLabel = agentStatusLabel(status);
  const activelyRunning = isAgentActivelyRunning(status);
  const inSelectionMode = selectionMode;
  const rowDisabled = inSelectionMode && selectionDisabled;

  return (
    <AnimatedPressable
      style={[
        styles.row,
        (activelyRunning || (inSelectionMode && selected)) && styles.rowActive,
      ]}
      preset="card"
      onPress={inSelectionMode ? onToggleSelection : onPress}
      onLongPress={inSelectionMode ? undefined : onLongPress}
      delayLongPress={400}
      disabled={rowDisabled}
      accessibilityRole={inSelectionMode ? 'checkbox' : 'button'}
      accessibilityLabel={`${title}, ${statusLabel}, ${preview}${activelyRunning ? '' : `, ${timeLabel}`}`}
      accessibilityHint={
        inSelectionMode
          ? 'Double tap to toggle selection'
          : 'Opens the terminal session'
      }
      accessibilityState={{
        busy: activelyRunning,
        checked: inSelectionMode ? selected : undefined,
        disabled: rowDisabled,
      }}
    >
      <View style={styles.iconSlot}>
        {inSelectionMode ? (
          <View
            style={styles.selectionBadge}
            accessibilityElementsHidden
            importantForAccessibility="no-hide-descendants"
          >
            <Ionicons
              name={selected ? 'checkmark-circle' : 'ellipse-outline'}
              size={22}
              color={selected ? colors.accent : colors.textTertiary}
            />
          </View>
        ) : null}
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
        {!activelyRunning ? (
          <Text style={styles.time} numberOfLines={1}>
            {timeLabel}
          </Text>
        ) : null}
        <View style={styles.statusMeta}>
          {activelyRunning ? (
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

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    iconSlot: {
      width: 44,
      height: 44,
      alignItems: 'center',
      justifyContent: 'center',
    },
    selectionBadge: {
      position: 'absolute',
      left: 0,
      top: 0,
      width: 24,
      height: 24,
      borderRadius: 12,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: colors.bgPrimary,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
      zIndex: 2,
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
