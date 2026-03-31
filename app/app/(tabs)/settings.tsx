import React, { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  Modal,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { useLocalSearchParams } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Colors, Spacing, Typography } from '../../constants/tokens';
import {
  DefaultTerminalThemeName,
  TerminalThemeName,
  TerminalThemes,
} from '../../constants/terminalThemes';
import { wsClient } from '../../services/websocket';
import { ConnectionState, useAgents } from '../../store/agents';
import * as Storage from '../../services/storage';

export default function SettingsScreen() {
  const { state, dispatch } = useAgents();
  const params = useLocalSearchParams<{ addServer?: string }>();
  const [servers, setServers] = useState<Storage.StoredServer[]>([]);
  const [terminalTheme, setTerminalTheme] = useState<TerminalThemeName>(DefaultTerminalThemeName);
  const [loaded, setLoaded] = useState(false);
  const [editorVisible, setEditorVisible] = useState(false);
  const [editingServerId, setEditingServerId] = useState<string | null>(null);
  const [draftName, setDraftName] = useState('');
  const [draftUrl, setDraftUrl] = useState('');
  const [expandedServer, setExpandedServer] = useState<string | null>(null);
  const [handledAutoOpenToken, setHandledAutoOpenToken] = useState<string | null>(null);

  const connectedCount = useMemo(
    () => servers.filter(server => state.serverConnections[server.id] === 'connected').length,
    [servers, state.serverConnections],
  );

  useEffect(() => {
    (async () => {
      const [savedServers, theme] = await Promise.all([
        Storage.getServers(),
        Storage.getTerminalTheme(),
      ]);
      setServers(savedServers);
      setTerminalTheme(theme);
      setLoaded(true);
    })();
  }, []);

  useEffect(() => {
    if (!loaded || !params.addServer || handledAutoOpenToken === params.addServer) return;
    openCreateServer();
    setHandledAutoOpenToken(params.addServer);
  }, [handledAutoOpenToken, loaded, params.addServer]);

  const refreshServers = async () => {
    setServers(await Storage.getServers());
  };

  const connectServer = (server: Storage.StoredServer) => {
    wsClient.connectServer(server);
  };

  const disconnectServer = (serverId: string) => {
    wsClient.disconnectServer(serverId);
  };

  const openCreateServer = () => {
    setEditingServerId(null);
    setDraftName('');
    setDraftUrl('');
    setEditorVisible(true);
  };

  const openEditServer = (server: Storage.StoredServer) => {
    setEditingServerId(server.id);
    setDraftName(server.name);
    setDraftUrl(server.url);
    setEditorVisible(true);
  };

  const closeEditor = () => {
    setEditorVisible(false);
    setEditingServerId(null);
    setDraftName('');
    setDraftUrl('');
  };

  const handleSaveServer = async () => {
    const normalizedURL = draftUrl.trim();
    if (!normalizedURL) {
      Alert.alert('Server URL required', 'Enter a WebSocket URL like ws://host:9876/ws.');
      return;
    }

    const wasConnected =
      editingServerId ? Boolean(state.serverConnections[editingServerId]) : true;

    const savedServer = await Storage.saveServer({
      id: editingServerId || undefined,
      name: draftName,
      url: normalizedURL,
    });

    await refreshServers();
    closeEditor();

    if (wasConnected) {
      wsClient.connectServer(savedServer);
    }
  };

  const handleDeleteServer = (server: Storage.StoredServer) => {
    Alert.alert(
      'Remove server',
      `Delete ${server.name}?`,
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Delete',
          style: 'destructive',
          onPress: async () => {
            wsClient.disconnectServer(server.id);
            dispatch({ type: 'REMOVE_SERVER', serverId: server.id });
            await Storage.removeServer(server.id);
            await refreshServers();
          },
        },
      ],
    );
  };

  const handleTerminalTheme = async (value: TerminalThemeName) => {
    setTerminalTheme(value);
    await Storage.setTerminalTheme(value);
  };

  const toggleServerExpand = (serverId: string) => {
    setExpandedServer(prev => prev === serverId ? null : serverId);
  };

  if (!loaded) return null;

  return (
    <SafeAreaView style={styles.container} edges={['top']}>
      <ScrollView contentContainerStyle={styles.content}>
        <Text style={styles.pageTitle}>Settings</Text>

        {/* Servers */}
        <View style={styles.sectionHeader}>
          <Text style={styles.sectionLabel}>Servers</Text>
          {servers.length > 0 && (
            <Text style={styles.sectionCount}>{connectedCount}/{servers.length}</Text>
          )}
        </View>

        <View style={styles.serverList}>
          {servers.length === 0 ? (
            <View style={styles.emptyCard}>
              <Text style={styles.emptyText}>No servers configured</Text>
            </View>
          ) : (
            servers.map(server => {
              const connectionState = state.serverConnections[server.id] || 'offline';
              const expanded = expandedServer === server.id;
              return (
                <TouchableOpacity
                  key={server.id}
                  style={styles.serverCard}
                  onPress={() => toggleServerExpand(server.id)}
                  activeOpacity={0.82}
                >
                  <View style={styles.serverRow}>
                    <View style={[styles.statusDot, { backgroundColor: connectionColor(connectionState) }]} />
                    <View style={styles.serverInfo}>
                      <Text style={styles.serverName}>{server.name}</Text>
                      <Text style={styles.serverUrl} numberOfLines={1}>{server.url}</Text>
                    </View>
                    <Text style={[styles.connectionLabel, connectionState === 'connected' && styles.connectionLabelActive]}>
                      {connectionLabel(connectionState)}
                    </Text>
                  </View>

                  {expanded && (
                    <View style={styles.serverActions}>
                      <TouchableOpacity
                        style={styles.actionBtn}
                        onPress={() => connectionState !== 'offline' ? disconnectServer(server.id) : connectServer(server)}
                        activeOpacity={0.82}
                      >
                        <Text style={styles.actionBtnText}>
                          {connectionState === 'connected' ? 'Disconnect' : connectionState === 'connecting' ? 'Retry' : 'Connect'}
                        </Text>
                      </TouchableOpacity>
                      <TouchableOpacity
                        style={styles.actionBtn}
                        onPress={() => openEditServer(server)}
                        activeOpacity={0.82}
                      >
                        <Text style={styles.actionBtnText}>Edit</Text>
                      </TouchableOpacity>
                      <TouchableOpacity
                        style={[styles.actionBtn, styles.actionBtnDanger]}
                        onPress={() => handleDeleteServer(server)}
                        activeOpacity={0.82}
                      >
                        <Text style={[styles.actionBtnText, styles.actionBtnDangerText]}>Remove</Text>
                      </TouchableOpacity>
                    </View>
                  )}
                </TouchableOpacity>
              );
            })
          )}
        </View>

        <TouchableOpacity style={styles.addBtn} onPress={openCreateServer} activeOpacity={0.82}>
          <Ionicons name="add" size={16} color={Colors.textSecondary} />
          <Text style={styles.addBtnText}>Add Server</Text>
        </TouchableOpacity>

        {/* Theme */}
        <Text style={styles.sectionLabel}>Theme</Text>
        <View style={styles.themeList}>
          {(Object.keys(TerminalThemes) as TerminalThemeName[]).map(themeName => {
            const theme = TerminalThemes[themeName];
            const active = terminalTheme === themeName;
            return (
              <TouchableOpacity
                key={themeName}
                style={[styles.themeCard, active && styles.themeCardActive]}
                onPress={() => handleTerminalTheme(themeName)}
                activeOpacity={0.84}
              >
                <View style={[styles.themePreview, { backgroundColor: theme.background }]}>
                  <Text style={[styles.themePreviewText, { color: theme.foreground }]}>
                    $ zen --watch{'\n'}
                    <Text style={{ color: theme.green }}>connected</Text>
                    <Text style={{ color: theme.brightBlack }}> · 3 agents</Text>
                  </Text>
                </View>
                <Text style={[styles.themeName, active && styles.themeNameActive]}>{themeName}</Text>
              </TouchableOpacity>
            );
          })}
        </View>

        <Text style={styles.version}>zen v0.1.0</Text>
      </ScrollView>

      <Modal
        visible={editorVisible}
        transparent
        animationType="fade"
        onRequestClose={closeEditor}
      >
        <View style={styles.modalRoot}>
          <TouchableOpacity
            style={styles.modalBackdrop}
            activeOpacity={1}
            onPress={closeEditor}
          />
          <View style={styles.modalCard}>
            <Text style={styles.modalTitle}>
              {editingServerId ? 'Edit Server' : 'Add Server'}
            </Text>

            <Text style={styles.fieldLabel}>Name</Text>
            <TextInput
              style={styles.input}
              value={draftName}
              onChangeText={setDraftName}
              placeholder="staging"
              placeholderTextColor="rgba(255,255,255,0.2)"
              autoCapitalize="none"
              autoCorrect={false}
            />

            <Text style={[styles.fieldLabel, { marginTop: 16 }]}>WebSocket URL</Text>
            <TextInput
              style={styles.input}
              value={draftUrl}
              onChangeText={setDraftUrl}
              placeholder="ws://your-server:9876"
              placeholderTextColor="rgba(255,255,255,0.2)"
              autoCapitalize="none"
              autoCorrect={false}
            />

            <View style={styles.modalActions}>
              <TouchableOpacity style={styles.modalBtn} onPress={closeEditor} activeOpacity={0.82}>
                <Text style={styles.modalBtnText}>Cancel</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[styles.modalBtn, styles.modalBtnPrimary]}
                onPress={handleSaveServer}
                activeOpacity={0.82}
              >
                <Text style={[styles.modalBtnText, styles.modalBtnPrimaryText]}>Save</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

function connectionLabel(state: ConnectionState): string {
  switch (state) {
    case 'connected': return 'Connected';
    case 'connecting': return 'Connecting';
    case 'offline': return 'Offline';
  }
}

function connectionColor(state: ConnectionState): string {
  switch (state) {
    case 'connected': return Colors.statusRunning;
    case 'connecting': return '#E7B65C';
    case 'offline': return '#65758A';
  }
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  content: {
    paddingHorizontal: 20,
    paddingTop: 16,
    paddingBottom: 40,
  },
  pageTitle: {
    color: Colors.textPrimary,
    fontSize: 22,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 0.5,
    marginBottom: 8,
    opacity: 0.9,
  },

  // Section
  sectionHeader: {
    flexDirection: 'row',
    alignItems: 'baseline',
    justifyContent: 'space-between',
    marginBottom: 12,
  },
  sectionLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase',
    letterSpacing: 1.2,
    marginBottom: 10,
    marginTop: 20,
    opacity: 0.7,
  },
  sectionCount: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.terminalFont,
    opacity: 0.5,
  },

  // Server list
  serverList: {
    gap: 6,
  },
  serverCard: {
    borderRadius: 12,
    paddingHorizontal: 14,
    paddingVertical: 12,
    backgroundColor: 'rgba(255,255,255,0.03)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.06)',
  },
  serverRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  statusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  serverInfo: {
    flex: 1,
  },
  serverName: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  serverUrl: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
    marginTop: 2,
    opacity: 0.6,
  },
  connectionLabel: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFont,
    opacity: 0.5,
  },
  connectionLabelActive: {
    color: Colors.statusRunning,
    opacity: 0.8,
  },
  serverActions: {
    flexDirection: 'row',
    gap: 8,
    marginTop: 12,
    paddingTop: 12,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: 'rgba(255,255,255,0.06)',
  },
  actionBtn: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 8,
    backgroundColor: 'rgba(255,255,255,0.05)',
  },
  actionBtnText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    opacity: 0.8,
  },
  actionBtnDanger: {
    backgroundColor: 'rgba(255,82,82,0.08)',
    marginLeft: 'auto',
  },
  actionBtnDangerText: {
    color: '#F09999',
  },
  addBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    paddingVertical: 10,
    marginTop: 8,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    borderStyle: 'dashed',
  },
  addBtnText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
  },
  emptyCard: {
    paddingVertical: 20,
    alignItems: 'center',
  },
  emptyText: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
    opacity: 0.5,
  },

  // Theme
  themeList: {
    gap: 10,
  },
  themeCard: {
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.06)',
    overflow: 'hidden',
  },
  themeCardActive: {
    borderColor: 'rgba(91,157,255,0.3)',
  },
  themePreview: {
    minHeight: 80,
    padding: 14,
    justifyContent: 'flex-end',
  },
  themePreviewText: {
    fontSize: 12,
    fontFamily: Typography.terminalFont,
    lineHeight: 18,
  },
  themeName: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    paddingHorizontal: 14,
    paddingVertical: 10,
    textTransform: 'capitalize',
    opacity: 0.6,
  },
  themeNameActive: {
    color: Colors.accent,
    opacity: 0.9,
  },
  version: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFont,
    textAlign: 'center',
    marginTop: 40,
    opacity: 0.3,
  },

  // Modal
  modalRoot: {
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: 24,
  },
  modalBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: 'rgba(0,0,0,0.7)',
  },
  modalCard: {
    borderRadius: 16,
    padding: 20,
    backgroundColor: '#141418',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
  },
  modalTitle: {
    color: Colors.textPrimary,
    fontSize: 17,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 20,
  },
  fieldLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
    marginBottom: 6,
    opacity: 0.6,
  },
  input: {
    backgroundColor: 'rgba(255,255,255,0.04)',
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 10,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.terminalFont,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
  },
  modalActions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: 10,
    marginTop: 24,
  },
  modalBtn: {
    minWidth: 70,
    height: 36,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: 'rgba(255,255,255,0.05)',
  },
  modalBtnPrimary: {
    backgroundColor: Colors.accent,
  },
  modalBtnText: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  modalBtnPrimaryText: {
    color: Colors.bgPrimary,
  },
});
