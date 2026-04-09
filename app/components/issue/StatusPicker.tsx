import React from 'react';
import { Modal, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Colors, Typography, issueStatusColor } from '../../constants/tokens';
import type { IssueStatus } from '../../constants/tokens';
import { IssueStatusIcon } from './IssueStatusIcon';

const STATUSES: { key: IssueStatus; label: string }[] = [
  { key: 'backlog', label: 'Backlog' },
  { key: 'todo', label: 'Todo' },
  { key: 'in_progress', label: 'In Progress' },
  { key: 'done', label: 'Done' },
  { key: 'cancelled', label: 'Cancelled' },
];

interface Props {
  visible: boolean;
  current: IssueStatus;
  onSelect: (status: IssueStatus) => void;
  onClose: () => void;
}

export function StatusPicker({ visible, current, onSelect, onClose }: Props) {
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <View style={styles.root}>
        <TouchableOpacity style={styles.backdrop} activeOpacity={1} onPress={onClose} />
        <View style={styles.card}>
          <Text style={styles.title}>Status</Text>
          {STATUSES.map(s => (
            <TouchableOpacity
              key={s.key}
              style={[styles.item, s.key === current && styles.itemActive]}
              onPress={() => { onSelect(s.key); onClose(); }}
              activeOpacity={0.82}
            >
              <IssueStatusIcon status={s.key} size={16} />
              <Text style={styles.itemText}>{s.label}</Text>
              {s.key === current && (
                <Text style={styles.check}>✓</Text>
              )}
            </TouchableOpacity>
          ))}
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  root: { flex: 1, justifyContent: 'flex-end' },
  backdrop: { ...StyleSheet.absoluteFillObject, backgroundColor: 'rgba(0,0,0,0.6)' },
  card: {
    marginHorizontal: 12,
    marginBottom: 32,
    borderRadius: 16,
    backgroundColor: '#1A1A22',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    overflow: 'hidden',
  },
  title: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    paddingHorizontal: 18,
    paddingTop: 16,
    paddingBottom: 10,
    opacity: 0.6,
  },
  item: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingHorizontal: 18,
    paddingVertical: 14,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: 'rgba(255,255,255,0.05)',
  },
  itemActive: {
    backgroundColor: 'rgba(255,255,255,0.04)',
  },
  itemText: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 15,
    fontFamily: Typography.uiFont,
  },
  check: {
    color: Colors.accent,
    fontSize: 14,
  },
});
