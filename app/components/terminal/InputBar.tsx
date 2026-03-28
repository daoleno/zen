import React, { forwardRef, useImperativeHandle, useState } from 'react';
import {
  Alert,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as DocumentPicker from 'expo-document-picker';
import * as Haptics from 'expo-haptics';
import { Colors, Typography } from '../../constants/tokens';
import { getServerUrl } from '../../services/storage';
import type { TerminalSurfaceHandle } from './TerminalSurface';

const SHORTCUT_KEYS = [
  { label: 'Ctrl', sequence: '__ctrl__' },
  { label: 'Esc', sequence: '\x1b' },
  { label: 'Tab', sequence: '\t' },
  { label: 'Ctrl-C', sequence: '\x03' },
  { label: '←', sequence: '\x1b[D' },
  { label: '↑', sequence: '\x1b[A' },
  { label: '↓', sequence: '\x1b[B' },
  { label: '→', sequence: '\x1b[C' },
] as const;

export interface InputBarHandle {
  focus(): void;
}

interface InputBarProps {
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
}

export const InputBar = forwardRef<InputBarHandle, InputBarProps>(
  ({ terminalRef }, ref) => {
    const [ctrlArmed, setCtrlArmed] = useState(false);

    useImperativeHandle(ref, () => ({
      focus() {
        terminalRef.current?.focus();
      },
    }));

    const sendInput = (data: string) => {
      terminalRef.current?.sendInput(data);
    };

    const applyModifiers = (sequence: string) => {
      let next = sequence;
      if (ctrlArmed && sequence.length === 1) {
        const code = sequence.toUpperCase().charCodeAt(0);
        if (code >= 64 && code <= 95) {
          next = String.fromCharCode(code - 64);
        }
        setCtrlArmed(false);
      }
      return next;
    };

    const handleShortcut = (sequence: string) => {
      if (sequence === '__ctrl__') {
        setCtrlArmed(v => !v);
        return;
      }
      sendInput(applyModifiers(sequence));
    };

    const handleFilePick = async () => {
      try {
        const result = await DocumentPicker.getDocumentAsync({
          type: ['*/*'],
          copyToCacheDirectory: true,
        });
        if (result.canceled || !result.assets?.length) return;

        const asset = result.assets[0];
        const serverUrl = await getServerUrl();
        const uploadUrl = buildUploadUrl(serverUrl);
        if (!uploadUrl) {
          throw new Error('Server URL is not configured');
        }

        const formData = new FormData();
        formData.append('file', {
          uri: asset.uri,
          name: asset.name || 'upload',
          type: asset.mimeType || 'application/octet-stream',
        } as any);

        const response = await fetch(uploadUrl, {
          method: 'POST',
          body: formData,
        });
        if (!response.ok) {
          throw new Error(`Upload failed (${response.status})`);
        }

        const payload = await response.json() as { path?: string };
        if (!payload.path) {
          throw new Error('Upload response missing file path');
        }

        const uploadedPath = payload.path;
        sendInput(appendShellPath('', uploadedPath));
        terminalRef.current?.focus();
        Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
      } catch (err: any) {
        Alert.alert('Error', err?.message || 'Failed to upload file');
      }
    };

    return (
      <View style={styles.container}>
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          style={styles.shortcutRow}
          contentContainerStyle={styles.shortcutRowContent}
        >
          <TouchableOpacity style={styles.attachBtn} onPress={handleFilePick}>
            <Ionicons name="attach-outline" size={18} color={Colors.textPrimary} />
          </TouchableOpacity>

          {SHORTCUT_KEYS.map(key => {
            const isCtrl = key.sequence === '__ctrl__';
            const active = isCtrl && ctrlArmed;
            return (
              <TouchableOpacity
                key={key.label}
                style={[styles.shortcutBtn, active && styles.shortcutBtnActive]}
                onPress={() => handleShortcut(key.sequence)}
              >
                <Text style={[styles.shortcutText, active && styles.shortcutTextActive]}>
                  {key.label}
                </Text>
              </TouchableOpacity>
            );
          })}
        </ScrollView>
      </View>
    );
  },
);

function buildUploadUrl(serverUrl: string): string | null {
  if (!serverUrl) return null;

  try {
    const url = new URL(serverUrl);
    if (url.protocol === 'ws:') {
      url.protocol = 'http:';
    } else if (url.protocol === 'wss:') {
      url.protocol = 'https:';
    }
    url.pathname = '/upload';
    url.search = '';
    url.hash = '';
    return url.toString();
  } catch {
    return null;
  }
}

function appendShellPath(current: string, path: string): string {
  const quoted = shellQuote(path);
  return current.trim() ? `${current} ${quoted}` : quoted;
}

function shellQuote(value: string): string {
  return `'${value.replace(/'/g, `'\"'\"'`)}'`;
}

const styles = StyleSheet.create({
  container: {
    backgroundColor: Colors.bgPrimary,
    paddingBottom: 4,
  },
  shortcutRow: {
    paddingTop: 8,
    paddingBottom: 8,
  },
  shortcutRowContent: {
    paddingLeft: 0,
    paddingRight: 18,
  },
  shortcutBtn: {
    paddingHorizontal: 12,
    height: 36,
    borderRadius: 10,
    backgroundColor: Colors.bgSurface,
    marginRight: 6,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#27364a',
  },
  shortcutBtnActive: {
    backgroundColor: Colors.accent,
    borderColor: Colors.accent,
  },
  shortcutText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.terminalFont,
  },
  shortcutTextActive: {
    color: '#fff',
  },
  attachBtn: {
    width: 36,
    height: 36,
    borderRadius: 10,
    marginRight: 6,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: '#27364a',
    backgroundColor: Colors.bgSurface,
  },
});
