import React, { useMemo } from 'react';
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
import { AgentStatus, Colors, Typography } from '../../constants/tokens';
import { buildAuthorizationHeader } from '../../services/auth';
import type { TerminalSurfaceHandle } from './TerminalSurface';

type ShortcutKey =
  | { label: 'Ctrl'; type: 'modifier' }
  | { label: string; type: 'sequence'; sequence: string };

type QuickAction = {
  label: string;
  prompt: string;
  tone?: 'primary' | 'neutral';
};

const SHORTCUT_KEYS: readonly ShortcutKey[] = [
  { label: 'Ctrl', type: 'modifier' },
  { label: 'Esc', type: 'sequence', sequence: '\x1b' },
  { label: 'Tab', type: 'sequence', sequence: '\t' },
  { label: 'Ctrl-B', type: 'sequence', sequence: '\x02' },
  { label: 'Ctrl-C', type: 'sequence', sequence: '\x03' },
  { label: '←', type: 'sequence', sequence: '\x1b[D' },
  { label: '↑', type: 'sequence', sequence: '\x1b[A' },
  { label: '↓', type: 'sequence', sequence: '\x1b[B' },
  { label: '→', type: 'sequence', sequence: '\x1b[C' },
];

interface TerminalAccessoryBarProps {
  terminalRef: React.RefObject<TerminalSurfaceHandle | null>;
  serverUrl: string;
  authSecret?: string;
  ctrlArmed: boolean;
  agentStatus?: AgentStatus;
  onCtrlArmedChange(next: boolean): void;
}

export function TerminalAccessoryBar({
  terminalRef,
  serverUrl,
  authSecret,
  ctrlArmed,
  agentStatus,
  onCtrlArmedChange,
}: TerminalAccessoryBarProps) {
  const uploadEnabled = !!buildUploadUrl(serverUrl);
  const quickActions = useMemo(() => buildQuickActions(agentStatus), [agentStatus]);

  const sendInput = (data: string) => {
    terminalRef.current?.sendInput(data);
  };

  const handleCtrlToggle = () => {
    onCtrlArmedChange(!ctrlArmed);
  };

  const handleShortcut = (sequence: string) => {
    onCtrlArmedChange(false);
    sendInput(sequence);
  };

  const handleQuickAction = async (prompt: string) => {
    onCtrlArmedChange(false);
    terminalRef.current?.resumeInput();
    sendInput(prompt);
    await Haptics.selectionAsync();
  };

  const handleFilePick = async () => {
    try {
      const result = await DocumentPicker.getDocumentAsync({
        type: ['*/*'],
        copyToCacheDirectory: true,
      });
      if (result.canceled || !result.assets?.length) return;

      const asset = result.assets[0];
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
        headers: buildRequestHeaders(authSecret),
        body: formData,
      });
      if (!response.ok) {
        throw new Error(`Upload failed (${response.status})`);
      }

      const payload = await response.json() as { path?: string };
      if (!payload.path) {
        throw new Error('Upload response missing file path');
      }

      onCtrlArmedChange(false);
      terminalRef.current?.resumeInput();
      sendInput(appendShellPath('', payload.path));
      await Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success);
    } catch (err: any) {
      Alert.alert('Error', err?.message || 'Failed to upload file');
    }
  };

  return (
    <View style={styles.container}>
      {quickActions.length > 0 ? (
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          keyboardShouldPersistTaps="handled"
          style={styles.quickRow}
          contentContainerStyle={styles.quickRowContent}
        >
          {quickActions.map(action => (
            <TouchableOpacity
              key={action.label}
              style={[
                styles.quickAction,
                action.tone === 'primary' && styles.quickActionPrimary,
              ]}
              onPress={() => void handleQuickAction(action.prompt)}
              activeOpacity={0.82}
            >
              <Text
                style={[
                  styles.quickActionText,
                  action.tone === 'primary' && styles.quickActionTextPrimary,
                ]}
              >
                {action.label}
              </Text>
            </TouchableOpacity>
          ))}
        </ScrollView>
      ) : null}

      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        keyboardShouldPersistTaps="handled"
        style={styles.shortcutRow}
        contentContainerStyle={styles.shortcutRowContent}
      >
        <TouchableOpacity
          style={[styles.attachBtn, !uploadEnabled && styles.attachBtnDisabled]}
          onPress={() => void handleFilePick()}
          disabled={!uploadEnabled}
          activeOpacity={0.82}
        >
          <Ionicons name="attach-outline" size={18} color={Colors.textPrimary} />
        </TouchableOpacity>

        {SHORTCUT_KEYS.map(key => {
          const isModifier = key.type === 'modifier';
          const active = isModifier && ctrlArmed;
          return (
            <TouchableOpacity
              key={key.type === 'sequence' ? key.sequence : key.label}
              style={[
                styles.shortcutBtn,
                active && styles.shortcutBtnActive,
              ]}
              onPress={() => {
                if (key.type === 'modifier') {
                  handleCtrlToggle();
                  return;
                }
                handleShortcut(key.sequence);
              }}
              activeOpacity={0.82}
            >
              <Text
                style={[
                  styles.shortcutText,
                  active && styles.shortcutTextActive,
                ]}
              >
                {key.label}
              </Text>
            </TouchableOpacity>
          );
        })}
      </ScrollView>
    </View>
  );
}

function buildQuickActions(status: AgentStatus | undefined): QuickAction[] {
  switch (status) {
    case 'blocked':
      return [
        {
          label: 'Approve',
          prompt: 'Approved. Continue with the safest next step.\n',
          tone: 'primary',
        },
        {
          label: 'Clarify',
          prompt: 'Summarize the blocker, the exact command you want to run, and the expected risk.\n',
        },
        {
          label: 'Plan',
          prompt: 'Pause and give me the smallest safe plan from here.\n',
        },
      ];
    case 'failed':
      return [
        {
          label: 'Explain',
          prompt: 'Explain the failure, likely root cause, and the smallest fix.\n',
          tone: 'primary',
        },
        {
          label: 'Retry',
          prompt: 'Retry from the last stable point and call out anything risky first.\n',
        },
      ];
    case 'done':
      return [
        {
          label: 'Summary',
          prompt: 'Summarize what changed, how you verified it, and any follow-up risks.\n',
          tone: 'primary',
        },
      ];
    case 'running':
    case 'unknown':
    default:
      return [];
  }
}

function buildRequestHeaders(secret: string | undefined): Record<string, string> | undefined {
  const authHeader = buildAuthorizationHeader(secret);
  if (!authHeader) return undefined;
  return { Authorization: authHeader };
}

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
  return `'${value.replace(/'/g, `'"'"'`)}'`;
}

const styles = StyleSheet.create({
  container: {
    backgroundColor: Colors.bgPrimary,
    paddingBottom: 4,
  },
  quickRow: {
    paddingTop: 4,
    paddingBottom: 4,
  },
  quickRowContent: {
    paddingRight: 18,
  },
  quickAction: {
    minHeight: 34,
    paddingHorizontal: 12,
    borderRadius: 999,
    marginRight: 8,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.09)',
    backgroundColor: 'rgba(255,255,255,0.05)',
  },
  quickActionPrimary: {
    backgroundColor: 'rgba(91,157,255,0.16)',
    borderColor: 'rgba(91,157,255,0.38)',
  },
  quickActionText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  quickActionTextPrimary: {
    color: '#DCEBFF',
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
  attachBtnDisabled: {
    opacity: 0.45,
  },
});
