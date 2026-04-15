import React from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography } from '../../constants/tokens';
import { IssueStatusIcon } from './IssueStatusIcon';
import { PriorityBar } from './PriorityBar';
import type { Task, Run } from '../../store/tasks';

interface Props {
  task: Task;
  run?: Run | null;
  metaText: string;
  secondaryText?: string;
  statusLabel?: string;
  statusTone?: string;
  hasLiveSession?: boolean;
  sessionIsLive?: boolean;
  runCount?: number;
  onPress: () => void;
  onLongPress?: () => void;
}

export function IssueRow({
  task,
  run,
  metaText,
  secondaryText,
  statusLabel,
  statusTone = Colors.textSecondary,
  hasLiveSession = false,
  sessionIsLive = false,
  runCount = 0,
  onPress,
  onLongPress,
}: Props) {
  return (
    <TouchableOpacity
      style={styles.row}
      onPress={onPress}
      onLongPress={onLongPress}
      activeOpacity={0.82}
      delayLongPress={400}
    >
      <PriorityBar priority={task.priority} />

      <View style={styles.iconWrap}>
        <IssueStatusIcon status={task.status} size={16} />
      </View>

      <View style={styles.copy}>
        <View style={styles.titleRow}>
          <Text style={styles.title} numberOfLines={1}>{task.title}</Text>
        </View>
        <Text style={styles.meta} numberOfLines={1}>
          {metaText}
          {secondaryText ? `  ·  ${secondaryText}` : ''}
        </Text>
      </View>

      <View style={styles.trailing}>
        {statusLabel ? (
          <View style={[styles.statusPill, { backgroundColor: `${statusTone}18` }]}>
            <Text style={[styles.statusPillText, { color: statusTone }]}>
              {statusLabel}
            </Text>
          </View>
        ) : (
          <View style={styles.trailingSpacer} />
        )}

        <View style={styles.trailingMeta}>
          {runCount > 1 ? (
            <Text style={styles.runCount} numberOfLines={1}>
              ×{runCount}
            </Text>
          ) : null}
          {hasLiveSession ? (
            <Ionicons
              name="terminal-outline"
              size={14}
              color={sessionIsLive ? Colors.accent : Colors.textSecondary}
            />
          ) : run?.agentSessionId ? (
            <Ionicons name="link-outline" size={13} color={Colors.textSecondary} />
          ) : null}
        </View>
      </View>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  row: {
    minHeight: 58,
    paddingRight: 4,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  iconWrap: {
    width: 18,
    alignItems: 'center',
  },
  copy: {
    flex: 1,
    gap: 4,
    paddingVertical: 10,
  },
  titleRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  meta: {
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
  trailing: {
    alignItems: 'flex-end',
    justifyContent: 'center',
    gap: 6,
    paddingLeft: 8,
  },
  statusPill: {
    minHeight: 24,
    paddingHorizontal: 9,
    borderRadius: 999,
    alignItems: 'center',
    justifyContent: 'center',
  },
  statusPillText: {
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
  },
  trailingSpacer: {
    height: 24,
  },
  trailingMeta: {
    minHeight: 14,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
  },
  runCount: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
  },
});
