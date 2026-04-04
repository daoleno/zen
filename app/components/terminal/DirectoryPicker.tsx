import React, { useCallback, useEffect, useState } from 'react';
import {
  ActivityIndicator,
  FlatList,
  Modal,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography } from '../../constants/tokens';
import { wsClient } from '../../services/websocket';

interface DirectoryPickerProps {
  visible: boolean;
  serverId: string;
  initialPath?: string;
  onSelect(path: string): void;
  onClose(): void;
}

type DirEntry = { name: string; path: string };

export function DirectoryPicker({ visible, serverId, initialPath, onSelect, onClose }: DirectoryPickerProps) {
  const [currentPath, setCurrentPath] = useState('');
  const [entries, setEntries] = useState<DirEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const loadDir = useCallback(async (path?: string) => {
    setLoading(true);
    setError(null);
    try {
      const result = await wsClient.listDir(serverId, path);
      setCurrentPath(result.path);
      setEntries(result.entries);
    } catch (e: any) {
      setError(e.message ?? 'Failed to load directory');
    } finally {
      setLoading(false);
    }
  }, [serverId]);

  useEffect(() => {
    if (visible) {
      loadDir(initialPath || undefined);
    }
  }, [visible, initialPath, loadDir]);

  const goUp = () => {
    const parent = currentPath.replace(/\/[^/]+\/?$/, '') || '/';
    loadDir(parent);
  };

  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      <View style={styles.root}>
        <TouchableOpacity style={styles.backdrop} activeOpacity={1} onPress={onClose} />
        <View style={styles.card}>
          <View style={styles.handle} />

          {/* Header */}
          <View style={styles.header}>
            <Text style={styles.title}>Select Directory</Text>
          </View>

          {/* Current path + parent button */}
          <View style={styles.pathRow}>
            <TouchableOpacity onPress={goUp} style={styles.upBtn} activeOpacity={0.7}>
              <Ionicons name="arrow-up" size={18} color={Colors.textSecondary} />
            </TouchableOpacity>
            <Text style={styles.pathText} numberOfLines={1}>{currentPath}</Text>
          </View>

          {/* Content */}
          {loading ? (
            <View style={styles.center}>
              <ActivityIndicator color={Colors.textSecondary} />
            </View>
          ) : error ? (
            <View style={styles.center}>
              <Text style={styles.errorText}>{error}</Text>
            </View>
          ) : (
            <FlatList
              data={entries}
              keyExtractor={item => item.path}
              style={styles.list}
              renderItem={({ item }) => (
                <TouchableOpacity
                  style={styles.dirRow}
                  onPress={() => loadDir(item.path)}
                  activeOpacity={0.7}
                >
                  <Ionicons name="folder" size={18} color="#D6B16A" />
                  <Text style={styles.dirName}>{item.name}</Text>
                  <Ionicons name="chevron-forward" size={14} color="rgba(255,255,255,0.2)" />
                </TouchableOpacity>
              )}
              ListEmptyComponent={
                <View style={styles.center}>
                  <Text style={styles.emptyText}>No subdirectories</Text>
                </View>
              }
            />
          )}

          {/* Actions */}
          <View style={styles.actions}>
            <TouchableOpacity
              style={styles.selectBtn}
              onPress={() => onSelect(currentPath)}
              disabled={loading || !currentPath}
              activeOpacity={0.82}
            >
              <Text style={styles.selectBtnText}>Select This Directory</Text>
            </TouchableOpacity>
            <TouchableOpacity style={styles.cancelBtn} onPress={onClose} activeOpacity={0.82}>
              <Text style={styles.cancelBtnText}>Cancel</Text>
            </TouchableOpacity>
          </View>
        </View>
      </View>
    </Modal>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    justifyContent: 'flex-end',
  },
  backdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: 'rgba(6, 8, 12, 0.62)',
  },
  card: {
    maxHeight: '75%',
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    paddingHorizontal: 18,
    paddingTop: 12,
    paddingBottom: 28,
    backgroundColor: '#121A25',
    borderTopWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
  },
  handle: {
    alignSelf: 'center',
    width: 42,
    height: 4,
    borderRadius: 2,
    backgroundColor: '#3A475B',
    marginBottom: 14,
  },
  header: {
    marginBottom: 12,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
  },
  pathRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    marginBottom: 12,
    paddingHorizontal: 4,
  },
  upBtn: {
    width: 32,
    height: 32,
    borderRadius: 10,
    backgroundColor: '#18222F',
    alignItems: 'center',
    justifyContent: 'center',
  },
  pathText: {
    flex: 1,
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.terminalFont,
  },
  list: {
    maxHeight: 300,
  },
  dirRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingVertical: 12,
    paddingHorizontal: 12,
    borderRadius: 12,
    marginBottom: 4,
    backgroundColor: '#18222F',
  },
  dirName: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFont,
  },
  center: {
    height: 120,
    alignItems: 'center',
    justifyContent: 'center',
  },
  errorText: {
    color: '#E57373',
    fontSize: 13,
    fontFamily: Typography.uiFont,
    textAlign: 'center',
  },
  emptyText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
  },
  actions: {
    marginTop: 14,
    gap: 8,
  },
  selectBtn: {
    height: 40,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: Colors.accent,
  },
  selectBtnText: {
    color: Colors.bgPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  cancelBtn: {
    height: 40,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: 'rgba(255,255,255,0.04)',
  },
  cancelBtnText: {
    color: Colors.textSecondary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
});
