import React, { useMemo, useState } from 'react';
import {
  Alert,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Colors, Typography, issueStatusColor, priorityColor } from '../../constants/tokens';
import type { IssueStatus, IssuePriority } from '../../constants/tokens';
import { useTasks } from '../../store/tasks';
import { wsClient } from '../../services/websocket';
import { IssueStatusIcon } from '../../components/issue/IssueStatusIcon';
import { StatusPicker } from '../../components/issue/StatusPicker';
import { PriorityPicker } from '../../components/issue/PriorityPicker';

const PRIORITY_LABEL: Record<number, string> = {
  0: 'None',
  1: 'Urgent',
  2: 'High',
  3: 'Medium',
  4: 'Low',
};

const STATUS_LABEL: Record<string, string> = {
  backlog: 'Backlog',
  todo: 'Todo',
  in_progress: 'In Progress',
  done: 'Done',
  cancelled: 'Cancelled',
};

export default function IssueDetailScreen() {
  const { id, serverId } = useLocalSearchParams<{ id: string; serverId: string }>();
  const router = useRouter();
  const { state: taskState } = useTasks();
  const [statusPickerVisible, setStatusPickerVisible] = useState(false);
  const [delegating, setDelegating] = useState(false);

  const task = useMemo(
    () => taskState.tasks.find(t => t.id === id && t.serverId === serverId),
    [taskState.tasks, id, serverId],
  );

  if (!task) {
    return (
      <SafeAreaView style={styles.container}>
        <View style={styles.header}>
          <TouchableOpacity onPress={() => router.back()} activeOpacity={0.82}>
            <Ionicons name="arrow-back" size={22} color={Colors.textPrimary} />
          </TouchableOpacity>
          <Text style={styles.headerTitle}>Issue not found</Text>
        </View>
      </SafeAreaView>
    );
  }

  const canDelegate = task.status === 'backlog' || task.status === 'todo';
  const canMarkDone = task.status === 'in_progress' || task.status === 'todo';

  const handleStatusChange = (status: IssueStatus) => {
    wsClient.updateTask(task.serverId, task.id, { status });
  };

  const handlePriorityChange = (priority: IssuePriority) => {
    wsClient.updateTask(task.serverId, task.id, { priority });
  };

  const handleDelegate = async () => {
    setDelegating(true);
    try {
      const result = await wsClient.delegateTask(task.serverId, task.id);
      router.push({
        pathname: '/terminal/[id]',
        params: { id: result.agentId, serverId: task.serverId },
      });
    } catch (error: any) {
      Alert.alert('Delegation failed', error?.message || 'Could not delegate.');
    } finally {
      setDelegating(false);
    }
  };

  const handleDelete = () => {
    Alert.alert('Delete issue?', `ZEN-${task.number}: ${task.title}`, [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Delete',
        style: 'destructive',
        onPress: () => {
          wsClient.deleteTask(task.serverId, task.id);
          router.back();
        },
      },
    ]);
  };

  return (
    <SafeAreaView style={styles.container}>
      {/* Header */}
      <View style={styles.header}>
        <TouchableOpacity onPress={() => router.back()} activeOpacity={0.82}>
          <Ionicons name="arrow-back" size={22} color={Colors.textPrimary} />
        </TouchableOpacity>
        <Text style={styles.headerTitle}>ZEN-{task.number}</Text>
        <View style={{ flex: 1 }} />
      </View>

      <ScrollView style={styles.content} contentContainerStyle={styles.contentInner}>
        {/* Status badge */}
        <TouchableOpacity
          style={[styles.statusBadge, { backgroundColor: issueStatusColor(task.status) + '22', borderColor: issueStatusColor(task.status) }]}
          onPress={() => setStatusPickerVisible(true)}
          activeOpacity={0.82}
        >
          <IssueStatusIcon status={task.status} size={14} />
          <Text style={[styles.statusBadgeText, { color: issueStatusColor(task.status) }]}>
            {STATUS_LABEL[task.status] || task.status}
          </Text>
          <Ionicons name="chevron-down" size={12} color={issueStatusColor(task.status)} />
        </TouchableOpacity>

        {/* Title */}
        <Text style={styles.title}>{task.title}</Text>

        {/* Description */}
        {task.description ? (
          <Text style={styles.description}>{task.description}</Text>
        ) : (
          <Text style={styles.descriptionPlaceholder}>No description</Text>
        )}

        {/* Properties */}
        <View style={styles.properties}>
          <View style={styles.propRow}>
            <Text style={styles.propLabel}>Priority</Text>
            <PriorityPicker value={task.priority} onChange={handlePriorityChange} />
          </View>

          {task.labels && task.labels.length > 0 && (
            <View style={styles.propRow}>
              <Text style={styles.propLabel}>Labels</Text>
              <View style={styles.labelsRow}>
                {task.labels.map((label, i) => (
                  <View key={i} style={styles.labelChip}>
                    <Text style={styles.labelText}>{label}</Text>
                  </View>
                ))}
              </View>
            </View>
          )}

          {task.agentId && (
            <View style={styles.propRow}>
              <Text style={styles.propLabel}>Agent</Text>
              <TouchableOpacity
                style={styles.agentLink}
                onPress={() => router.push({
                  pathname: '/terminal/[id]',
                  params: { id: task.agentId!, serverId: task.serverId },
                })}
                activeOpacity={0.82}
              >
                <Ionicons name="terminal-outline" size={14} color={Colors.accent} />
                <Text style={styles.agentLinkText}>
                  {task.agentId}{task.agentStatus ? ` (${task.agentStatus})` : ''}
                </Text>
              </TouchableOpacity>
            </View>
          )}

          <View style={styles.propRow}>
            <Text style={styles.propLabel}>Server</Text>
            <Text style={styles.propValue}>{task.serverName}</Text>
          </View>
        </View>

        {/* Actions */}
        <View style={styles.actions}>
          {canDelegate && (
            <TouchableOpacity
              style={[styles.actionBtn, styles.actionBtnPrimary]}
              onPress={handleDelegate}
              disabled={delegating}
              activeOpacity={0.82}
            >
              <Ionicons name="play" size={16} color={Colors.bgPrimary} />
              <Text style={[styles.actionText, styles.actionTextPrimary]}>
                {delegating ? 'Delegating...' : 'Delegate to Agent'}
              </Text>
            </TouchableOpacity>
          )}
          {canMarkDone && (
            <TouchableOpacity
              style={styles.actionBtn}
              onPress={() => handleStatusChange('done')}
              activeOpacity={0.82}
            >
              <Ionicons name="checkmark" size={16} color={Colors.textPrimary} />
              <Text style={styles.actionText}>Mark Done</Text>
            </TouchableOpacity>
          )}
          {task.status !== 'cancelled' && task.status !== 'done' && (
            <TouchableOpacity
              style={styles.actionBtn}
              onPress={() => handleStatusChange('cancelled')}
              activeOpacity={0.82}
            >
              <Ionicons name="close" size={16} color={Colors.textSecondary} />
              <Text style={[styles.actionText, { color: Colors.textSecondary }]}>Cancel</Text>
            </TouchableOpacity>
          )}
          <TouchableOpacity style={styles.actionBtn} onPress={handleDelete} activeOpacity={0.82}>
            <Ionicons name="trash-outline" size={16} color="#F09999" />
            <Text style={[styles.actionText, { color: '#F09999' }]}>Delete</Text>
          </TouchableOpacity>
        </View>
      </ScrollView>

      <StatusPicker
        visible={statusPickerVisible}
        current={task.status}
        onSelect={handleStatusChange}
        onClose={() => setStatusPickerVisible(false)}
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.bgPrimary },
  header: {
    flexDirection: 'row', alignItems: 'center', gap: 12,
    paddingHorizontal: 16, paddingVertical: 12,
  },
  headerTitle: {
    color: Colors.textSecondary, fontSize: 14,
    fontFamily: Typography.terminalFont,
  },
  content: { flex: 1 },
  contentInner: { paddingHorizontal: 20, paddingBottom: 40 },

  statusBadge: {
    flexDirection: 'row', alignItems: 'center', gap: 6,
    alignSelf: 'flex-start',
    paddingHorizontal: 10, paddingVertical: 5, borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    marginBottom: 12,
  },
  statusBadgeText: {
    fontSize: 12, fontFamily: Typography.uiFontMedium,
  },

  title: {
    color: Colors.textPrimary, fontSize: 20,
    fontFamily: Typography.uiFontMedium, lineHeight: 26,
    marginBottom: 12,
  },
  description: {
    color: Colors.textSecondary, fontSize: 14,
    fontFamily: Typography.uiFont, lineHeight: 20,
    marginBottom: 24,
  },
  descriptionPlaceholder: {
    color: Colors.textSecondary, fontSize: 14,
    fontFamily: Typography.uiFont, fontStyle: 'italic',
    opacity: 0.4, marginBottom: 24,
  },

  properties: { gap: 16, marginBottom: 28 },
  propRow: { gap: 6 },
  propLabel: {
    color: Colors.textSecondary, fontSize: 11,
    fontFamily: Typography.uiFontMedium, textTransform: 'uppercase',
    letterSpacing: 0.5, opacity: 0.6,
  },
  propValue: {
    color: Colors.textPrimary, fontSize: 14, fontFamily: Typography.uiFont,
  },
  labelsRow: { flexDirection: 'row', gap: 6, flexWrap: 'wrap' },
  labelChip: {
    paddingHorizontal: 8, paddingVertical: 3, borderRadius: 6,
    backgroundColor: 'rgba(255,255,255,0.06)',
  },
  labelText: { color: Colors.textPrimary, fontSize: 12, fontFamily: Typography.uiFont },
  agentLink: { flexDirection: 'row', alignItems: 'center', gap: 6 },
  agentLinkText: { color: Colors.accent, fontSize: 14, fontFamily: Typography.terminalFont },

  actions: { gap: 10 },
  actionBtn: {
    flexDirection: 'row', alignItems: 'center', justifyContent: 'center',
    gap: 8, height: 44, borderRadius: 12,
    backgroundColor: 'rgba(255,255,255,0.06)',
    borderWidth: StyleSheet.hairlineWidth, borderColor: 'rgba(255,255,255,0.1)',
  },
  actionBtnPrimary: { backgroundColor: Colors.accent, borderColor: Colors.accent },
  actionText: { color: Colors.textPrimary, fontSize: 14, fontFamily: Typography.uiFontMedium },
  actionTextPrimary: { color: Colors.bgPrimary },
});
