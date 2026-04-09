import React, { useState } from 'react';
import {
  Alert,
  KeyboardAvoidingView,
  Modal,
  Platform,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography } from '../../constants/tokens';
import type { IssuePriority } from '../../constants/tokens';
import { PriorityPicker } from './PriorityPicker';
import { wsClient } from '../../services/websocket';

interface Props {
  visible: boolean;
  serverOptions: { id: string; name: string }[];
  selectedServerId: string | null;
  onSelectServer: (id: string) => void;
  onClose: () => void;
  onCreated?: (serverId: string, taskId: string) => void;
}

export function CreateIssueSheet({
  visible,
  serverOptions,
  selectedServerId,
  onSelectServer,
  onClose,
  onCreated,
}: Props) {
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [priority, setPriority] = useState<IssuePriority>(0);
  const [submitting, setSubmitting] = useState(false);

  const reset = () => {
    setTitle('');
    setDescription('');
    setPriority(0);
  };

  const handleCreate = async (delegate: boolean) => {
    const trimmed = title.trim();
    if (!trimmed || !selectedServerId) return;

    setSubmitting(true);
    try {
      const task = await wsClient.createTask(selectedServerId, {
        title: trimmed,
        description: description.trim(),
        priority,
      });
      if (delegate) {
        try {
          await wsClient.delegateTask(selectedServerId, task.id);
        } catch {
          // Task created, delegation failed — user can retry
        }
      }
      reset();
      onCreated?.(selectedServerId, task.id);
      onClose();
    } catch (error: any) {
      Alert.alert('Could not create issue', error?.message || 'Try again.');
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal visible={visible} transparent animationType="slide" onRequestClose={onClose}>
      <KeyboardAvoidingView
        style={styles.root}
        behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
      >
        <TouchableOpacity style={styles.backdrop} activeOpacity={1} onPress={onClose} />
        <View style={styles.sheet}>
          <View style={styles.handle} />

          <Text style={styles.sheetTitle}>New Issue</Text>

          {/* Server selector */}
          {serverOptions.length > 1 && (
            <ScrollView horizontal showsHorizontalScrollIndicator={false} style={styles.serverRow}>
              {serverOptions.map(server => (
                <TouchableOpacity
                  key={server.id}
                  style={[styles.serverChip, server.id === selectedServerId && styles.serverChipActive]}
                  onPress={() => onSelectServer(server.id)}
                  activeOpacity={0.82}
                >
                  <Text style={[styles.serverChipText, server.id === selectedServerId && styles.serverChipTextActive]}>
                    {server.name}
                  </Text>
                </TouchableOpacity>
              ))}
            </ScrollView>
          )}

          {/* Title */}
          <View style={styles.fieldGroup}>
            <TextInput
              style={styles.titleInput}
              value={title}
              onChangeText={setTitle}
              placeholder="Issue title"
              placeholderTextColor="rgba(255,255,255,0.2)"
              autoCapitalize="sentences"
              autoCorrect={false}
              editable={!submitting}
            />
          </View>

          {/* Description */}
          <View style={styles.fieldGroup}>
            <TextInput
              style={styles.descInput}
              value={description}
              onChangeText={setDescription}
              placeholder="Description (optional)"
              placeholderTextColor="rgba(255,255,255,0.15)"
              multiline
              numberOfLines={3}
              autoCapitalize="sentences"
              editable={!submitting}
            />
          </View>

          {/* Priority */}
          <View style={styles.fieldGroup}>
            <Text style={styles.fieldLabel}>Priority</Text>
            <PriorityPicker value={priority} onChange={setPriority} />
          </View>

          {/* Actions */}
          <View style={styles.actions}>
            <TouchableOpacity
              style={[styles.actionBtn, styles.actionBtnPrimary, !title.trim() && styles.actionBtnDisabled]}
              onPress={() => handleCreate(false)}
              disabled={!title.trim() || submitting}
              activeOpacity={0.82}
            >
              <Text style={[styles.actionText, styles.actionTextPrimary]}>
                {submitting ? 'Creating...' : 'Create'}
              </Text>
            </TouchableOpacity>
            <TouchableOpacity
              style={[styles.actionBtn, !title.trim() && styles.actionBtnDisabled]}
              onPress={() => handleCreate(true)}
              disabled={!title.trim() || submitting}
              activeOpacity={0.82}
            >
              <Ionicons name="play" size={14} color={Colors.textPrimary} />
              <Text style={styles.actionText}>Create & Delegate</Text>
            </TouchableOpacity>
          </View>
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
    paddingBottom: 40,
  },
  handle: {
    width: 36, height: 4, borderRadius: 2,
    backgroundColor: 'rgba(255,255,255,0.15)',
    alignSelf: 'center',
    marginTop: 10, marginBottom: 12,
  },
  sheetTitle: {
    color: Colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
    paddingHorizontal: 20,
    marginBottom: 16,
  },
  serverRow: { paddingHorizontal: 16, marginBottom: 12, maxHeight: 36 },
  serverChip: {
    paddingHorizontal: 12, paddingVertical: 6, borderRadius: 8,
    backgroundColor: 'rgba(255,255,255,0.04)',
    borderWidth: StyleSheet.hairlineWidth, borderColor: 'rgba(255,255,255,0.08)',
    marginRight: 8,
  },
  serverChipActive: { backgroundColor: 'rgba(91,157,255,0.15)', borderColor: Colors.accent },
  serverChipText: { color: Colors.textSecondary, fontSize: 12, fontFamily: Typography.uiFontMedium },
  serverChipTextActive: { color: Colors.accent },
  fieldGroup: { paddingHorizontal: 20, marginBottom: 14 },
  fieldLabel: {
    color: Colors.textSecondary, fontSize: 11, fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase', letterSpacing: 0.5, marginBottom: 6, opacity: 0.6,
  },
  titleInput: {
    backgroundColor: 'rgba(255,255,255,0.04)', borderRadius: 12,
    paddingHorizontal: 14, paddingVertical: 12,
    color: Colors.textPrimary, fontSize: 16, fontFamily: Typography.uiFontMedium,
    borderWidth: StyleSheet.hairlineWidth, borderColor: 'rgba(255,255,255,0.08)',
  },
  descInput: {
    backgroundColor: 'rgba(255,255,255,0.04)', borderRadius: 12,
    paddingHorizontal: 14, paddingVertical: 10,
    color: Colors.textPrimary, fontSize: 14, fontFamily: Typography.uiFont,
    borderWidth: StyleSheet.hairlineWidth, borderColor: 'rgba(255,255,255,0.08)',
    minHeight: 60, textAlignVertical: 'top',
  },
  actions: { paddingHorizontal: 20, gap: 10 },
  actionBtn: {
    flexDirection: 'row', alignItems: 'center', justifyContent: 'center',
    gap: 6, height: 44, borderRadius: 12,
    backgroundColor: 'rgba(255,255,255,0.06)',
    borderWidth: StyleSheet.hairlineWidth, borderColor: 'rgba(255,255,255,0.1)',
  },
  actionBtnPrimary: { backgroundColor: Colors.accent, borderColor: Colors.accent },
  actionBtnDisabled: { opacity: 0.4 },
  actionText: { color: Colors.textPrimary, fontSize: 14, fontFamily: Typography.uiFontMedium },
  actionTextPrimary: { color: Colors.bgPrimary },
});
