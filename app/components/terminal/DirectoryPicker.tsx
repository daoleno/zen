import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  FlatList,
  StyleSheet,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, useAppColors } from '../../constants/tokens';
import { wsClient } from '../../services/websocket';
import { AppButton, AppText, BottomSheetFrame, IconButton, StateView } from '../ui';

interface DirectoryPickerProps {
  visible: boolean;
  serverId: string;
  initialPath?: string;
  onSelect(path: string): void;
  onClose(): void;
}

type DirEntry = { name: string; path: string };

export function DirectoryPicker({ visible, serverId, initialPath, onSelect, onClose }: DirectoryPickerProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
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
    <BottomSheetFrame visible={visible} onClose={onClose}>
      <View style={styles.header}>
        <AppText variant="title">Select Directory</AppText>
      </View>

      <View style={styles.pathRow}>
        <IconButton icon="arrow-up" size={32} onPress={goUp} />
        <AppText variant="mono" tone="secondary" style={styles.pathText} numberOfLines={1}>
          {currentPath}
        </AppText>
      </View>

      {loading ? (
        <StateView loading />
      ) : error ? (
        <StateView detail={error} danger />
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
              <Ionicons name="folder" size={18} color={colors.promptYellow} />
              <AppText variant="body" style={styles.dirName} numberOfLines={1}>
                {item.name}
              </AppText>
              <Ionicons name="chevron-forward" size={14} color={colors.textSecondary} />
            </TouchableOpacity>
          )}
          ListEmptyComponent={<StateView detail="No subdirectories" />}
        />
      )}

      <View style={styles.actions}>
        <AppButton
          label="Select This Directory"
          variant="primary"
          onPress={() => onSelect(currentPath)}
          disabled={loading || !currentPath}
        />
        <AppButton label="Cancel" variant="secondary" onPress={onClose} />
      </View>
    </BottomSheetFrame>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  header: {
    marginBottom: 12,
  },
  pathRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    marginBottom: 12,
    paddingHorizontal: 4,
  },
  pathText: {
    flex: 1,
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
    backgroundColor: colors.bgElevated,
  },
  dirName: {
    flex: 1,
  },
  actions: {
    marginTop: 14,
    gap: 8,
  },
  });
}
