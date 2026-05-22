import React, { useCallback, useEffect, useState } from 'react';
import { wsClient } from '../../services/websocket';
import { BottomSheetFrame } from '../ui';
import {
  DirectoryPickerContent,
  type DirectoryPickerEntry,
} from './DirectoryPickerContent';

interface DirectoryPickerProps {
  visible: boolean;
  serverId: string;
  initialPath?: string;
  onSelect(path: string): void;
  onClose(): void;
}

export function DirectoryPicker({ visible, serverId, initialPath, onSelect, onClose }: DirectoryPickerProps) {
  const [currentPath, setCurrentPath] = useState('');
  const [entries, setEntries] = useState<DirectoryPickerEntry[]>([]);
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
      <DirectoryPickerContent
        currentPath={currentPath}
        entries={entries}
        loading={loading}
        error={error}
        onGoUp={goUp}
        onOpenDirectory={loadDir}
        onSelectCurrent={() => onSelect(currentPath)}
        onClose={onClose}
      />
    </BottomSheetFrame>
  );
}
