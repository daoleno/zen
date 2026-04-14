import React from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography, runStatusColor } from '../../constants/tokens';
import { IssueStatusIcon } from './IssueStatusIcon';
import { PriorityBar } from './PriorityBar';
import type { Task, Run } from '../../store/tasks';

interface Props {
  task: Task;
  run?: Run | null;
  onPress: () => void;
  onLongPress?: () => void;
}

export function IssueRow({ task, run, onPress, onLongPress }: Props) {
  return (
    <TouchableOpacity
      style={styles.row}
      onPress={onPress}
      onLongPress={onLongPress}
      activeOpacity={0.82}
      delayLongPress={400}
    >
      <PriorityBar priority={task.priority} />
      <IssueStatusIcon status={task.status} size={16} />
      <Text style={styles.issueId}>ZEN-{task.number}</Text>
      <Text style={styles.title} numberOfLines={1}>{task.title}</Text>
      {run?.status ? (
        <View style={[styles.runDot, { backgroundColor: runStatusColor(run.status) }]} />
      ) : null}
      {run?.agentSessionId ? (
        <Ionicons name="terminal-outline" size={14} color={Colors.accent} />
      ) : null}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    height: 44,
    paddingRight: 16,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: 'rgba(255,255,255,0.04)',
  },
  issueId: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
    opacity: 0.6,
    minWidth: 52,
  },
  title: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  runDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
});
