import React from 'react';
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography, issueStatusColor, priorityColor } from '../../constants/tokens';
import { formatIssueId } from '../../services/taskIdentity';

export type TaskPickerOption = {
  id: string;
  identifierPrefix: string;
  number: number;
  title: string;
  subtitle?: string;
  status: string;
  priority: number;
};

interface Props {
  visible: boolean;
  busy?: boolean;
  title?: string;
  subtitle?: string;
  emptyTitle?: string;
  emptySubtitle?: string;
  tasks: TaskPickerOption[];
  onClose: () => void;
  onPick: (taskId: string) => void;
}

export function TaskPickerSheet({
  visible,
  busy = false,
  title = 'Link to Task',
  subtitle = 'Attach this live session to an existing task.',
  emptyTitle = 'No tasks available',
  emptySubtitle = 'Create a new task first, then come back to link this session.',
  tasks,
  onClose,
  onPick,
}: Props) {
  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose}>
      <KeyboardAvoidingView
        style={styles.root}
        behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
      >
        <TouchableOpacity style={styles.backdrop} activeOpacity={1} onPress={onClose} />
        <View style={styles.sheet}>
          <View style={styles.handle} />
          <Text style={styles.title}>{title}</Text>
          <Text style={styles.subtitle}>{subtitle}</Text>

          {tasks.length === 0 ? (
            <View style={styles.emptyState}>
              <Text style={styles.emptyTitle}>{emptyTitle}</Text>
              <Text style={styles.emptySubtitle}>{emptySubtitle}</Text>
            </View>
          ) : (
            <ScrollView style={styles.list} contentContainerStyle={styles.listContent}>
              {tasks.map(task => (
                <TouchableOpacity
                  key={task.id}
                  style={[styles.row, busy && styles.disabled]}
                  onPress={() => onPick(task.id)}
                  disabled={busy}
                  activeOpacity={0.82}
                >
                  <View style={[styles.priorityBar, { backgroundColor: priorityColor(task.priority as any) || 'transparent' }]} />
                    <View style={styles.copy}>
                      <View style={styles.rowHeader}>
                      <Text style={styles.issueId}>{formatIssueId(task.identifierPrefix, task.number)}</Text>
                      <View style={[styles.statusDot, { backgroundColor: issueStatusColor(task.status as any) }]} />
                    </View>
                    <Text style={styles.rowTitle} numberOfLines={1}>{task.title}</Text>
                    {task.subtitle ? (
                      <Text style={styles.rowSubtitle} numberOfLines={2}>{task.subtitle}</Text>
                    ) : null}
                  </View>
                  <Ionicons name="chevron-forward" size={16} color={Colors.textSecondary} />
                </TouchableOpacity>
              ))}
            </ScrollView>
          )}
        </View>
      </KeyboardAvoidingView>
    </Modal>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, justifyContent: 'flex-end' },
  backdrop: { ...StyleSheet.absoluteFillObject, backgroundColor: 'rgba(0,0,0,0.6)' },
  sheet: {
    backgroundColor: Colors.bgSurface,
    borderTopLeftRadius: 20,
    borderTopRightRadius: 20,
    paddingHorizontal: 20,
    paddingTop: 10,
    paddingBottom: 32,
    maxHeight: '82%',
  },
  handle: {
    width: 36,
    height: 4,
    borderRadius: 2,
    backgroundColor: 'rgba(255,255,255,0.15)',
    alignSelf: 'center',
    marginBottom: 12,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 18,
    fontFamily: Typography.uiFontMedium,
  },
  subtitle: {
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
    marginTop: 6,
    marginBottom: 18,
  },
  list: {
    maxHeight: 380,
  },
  listContent: {
    gap: 8,
    paddingBottom: 4,
  },
  row: {
    minHeight: 62,
    borderRadius: 14,
    backgroundColor: 'rgba(255,255,255,0.04)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    paddingHorizontal: 14,
    paddingVertical: 12,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
  },
  priorityBar: {
    width: 3,
    alignSelf: 'stretch',
    borderRadius: 3,
  },
  copy: {
    flex: 1,
    gap: 3,
  },
  rowHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  issueId: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
    opacity: 0.75,
  },
  statusDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  rowTitle: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  rowSubtitle: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
  emptyState: {
    borderRadius: 14,
    backgroundColor: 'rgba(255,255,255,0.04)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    paddingHorizontal: 16,
    paddingVertical: 16,
    gap: 6,
  },
  emptyTitle: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  emptySubtitle: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
  },
  disabled: {
    opacity: 0.45,
  },
});
