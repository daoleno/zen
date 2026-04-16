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
  const isDimmed = task.status === 'done' || task.status === 'cancelled';
  const hasTrailing = hasLiveSession || !!run?.agentSessionId || runCount > 1 || !!statusLabel;

  // Derive terminal icon color from execution state
  const terminalColor = statusLabel
    ? statusTone
    : sessionIsLive
      ? Colors.accent
      : Colors.textSecondary;

  return (
    <TouchableOpacity
      style={[styles.row, isDimmed && styles.rowDimmed]}
      onPress={onPress}
      onLongPress={onLongPress}
      activeOpacity={0.82}
      delayLongPress={400}
    >
      <PriorityBar priority={task.priority} />

      <View style={styles.iconWrap}>
        <IssueStatusIcon status={task.status} size={15} />
      </View>

      <View style={styles.copy}>
        <Text style={styles.title} numberOfLines={1}>{task.title}</Text>
        <Text style={styles.meta} numberOfLines={1}>
          {metaText}
          {secondaryText ? `  ·  ${secondaryText}` : ''}
        </Text>
      </View>

      {hasTrailing ? (
        <View style={styles.trailing}>
          {runCount > 1 ? (
            <Text style={styles.runCount}>×{runCount}</Text>
          ) : null}
          {statusLabel ? (
            <View style={[styles.statusDot, { backgroundColor: statusTone }]} />
          ) : null}
          {hasLiveSession ? (
            <Ionicons
              name="terminal-outline"
              size={13}
              color={terminalColor}
            />
          ) : run?.agentSessionId ? (
            <Ionicons name="link-outline" size={12} color={Colors.textSecondary} />
          ) : null}
        </View>
      ) : null}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  row: {
    minHeight: 50,
    paddingRight: 4,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  rowDimmed: {
    opacity: 0.42,
  },
  iconWrap: {
    width: 16,
    alignItems: 'center',
  },
  copy: {
    flex: 1,
    gap: 3,
    paddingVertical: 10,
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
    lineHeight: 15,
    fontFamily: Typography.uiFont,
  },
  trailing: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    paddingLeft: 6,
  },
  statusDot: {
    width: 5,
    height: 5,
    borderRadius: 2.5,
  },
  runCount: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
  },
});
