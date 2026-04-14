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
import { Colors, Typography, statusColor } from '../../constants/tokens';

export type DelegateSessionOption = {
  id: string;
  title: string;
  subtitle?: string;
  status: string;
};

interface Props {
  visible: boolean;
  busy?: boolean;
  sessions: DelegateSessionOption[];
  onClose: () => void;
  onStartNew: () => void;
  onAttach: (sessionId: string) => void;
}

export function DelegateRunSheet({
  visible,
  busy = false,
  sessions,
  onClose,
  onStartNew,
  onAttach,
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
          <Text style={styles.title}>Delegate Task</Text>
          <Text style={styles.subtitle}>
            Start a new agent run, or attach this task to an existing live session.
          </Text>

          <TouchableOpacity
            style={[styles.primaryAction, busy && styles.disabled]}
            onPress={onStartNew}
            disabled={busy}
            activeOpacity={0.82}
          >
            <Ionicons name="play" size={15} color={Colors.bgPrimary} />
            <Text style={styles.primaryActionText}>
              {busy ? 'Starting...' : 'Start New Agent Run'}
            </Text>
          </TouchableOpacity>

          <Text style={styles.sectionLabel}>Attach Live Session</Text>

          {sessions.length === 0 ? (
            <View style={styles.emptyState}>
              <Text style={styles.emptyTitle}>No available live sessions</Text>
              <Text style={styles.emptySubtitle}>
                Start a new run instead, or create a session from the terminal tab first.
              </Text>
            </View>
          ) : (
            <ScrollView style={styles.list} contentContainerStyle={styles.listContent}>
              {sessions.map(session => (
                <TouchableOpacity
                  key={session.id}
                  style={[styles.row, busy && styles.disabled]}
                  onPress={() => onAttach(session.id)}
                  disabled={busy}
                  activeOpacity={0.82}
                >
                  <View style={[styles.statusDot, { backgroundColor: statusColor(session.status as any) }]} />
                  <View style={styles.copy}>
                    <Text style={styles.rowTitle} numberOfLines={1}>{session.title}</Text>
                    {session.subtitle ? (
                      <Text style={styles.rowSubtitle} numberOfLines={2}>{session.subtitle}</Text>
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
  primaryAction: {
    minHeight: 46,
    borderRadius: 14,
    backgroundColor: Colors.accent,
    alignItems: 'center',
    justifyContent: 'center',
    flexDirection: 'row',
    gap: 8,
    marginBottom: 18,
  },
  primaryActionText: {
    color: Colors.bgPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  sectionLabel: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase',
    letterSpacing: 0.6,
    marginBottom: 10,
    opacity: 0.7,
  },
  list: {
    maxHeight: 320,
  },
  listContent: {
    gap: 8,
    paddingBottom: 4,
  },
  row: {
    minHeight: 56,
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
  statusDot: {
    width: 9,
    height: 9,
    borderRadius: 4.5,
  },
  copy: {
    flex: 1,
    gap: 3,
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
