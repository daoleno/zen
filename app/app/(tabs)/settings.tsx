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
  const [servers, setServers] = useState<Storage.StoredServer[]>([]);
  const [terminalTheme, setTerminalTheme] = useState<TerminalThemeName>(DefaultTerminalThemeName);
  const [loaded, setLoaded] = useState(false);
  const [editorVisible, setEditorVisible] = useState(false);
  const [editingServerId, setEditingServerId] = useState<string | null>(null);
  const [draftName, setDraftName] = useState('');
  const [draftUrl, setDraftUrl] = useState('');

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

  if (!loaded) return null;

  return (
    <SafeAreaView style={styles.container}>
      <ScrollView contentContainerStyle={styles.content}>
        <View style={styles.pageHeader}>
          <Text style={styles.pageTitle}>Settings</Text>
        </View>

        <View style={[styles.sectionHeader, styles.sectionTitleFirst]}>
          <View style={styles.sectionCopy}>
            <Text style={styles.sectionTitle}>Servers</Text>
            <Text style={styles.sectionSubtitle}>
              {servers.length === 0
                ? 'Add daemon endpoints and keep them online together.'
                : `${connectedCount}/${servers.length} connected. Each server keeps its own live agent stream.`}
            </Text>
          </View>
          <TouchableOpacity style={styles.addServerButton} onPress={openCreateServer} activeOpacity={0.84}>
            <Ionicons name="add" size={16} color={Colors.bgPrimary} />
            <Text style={styles.addServerButtonText}>Add</Text>
          </TouchableOpacity>
        </View>

        <View style={styles.serverList}>
          {servers.length === 0 ? (
            <View style={[styles.card, styles.emptyCard]}>
              <Text style={styles.emptyTitle}>No saved servers</Text>
              <Text style={styles.emptyText}>
                Save local, staging, and remote control planes as separate entries.
              </Text>
            </View>
          ) : (
            servers.map(server => {
              const connectionState = state.serverConnections[server.id] || 'offline';
              return (
                <View
                  key={server.id}
                  style={[styles.card, styles.serverCard, connectionState !== 'offline' && styles.serverCardConnected]}
                >
                  <View style={styles.serverRow}>
                    <View style={styles.serverMain}>
                      <View style={styles.serverTitleRow}>
                        <View style={[styles.statusDot, { backgroundColor: connectionColor(connectionState) }]} />
                        <Text style={styles.serverName}>{server.name}</Text>
                        <View style={[styles.serverBadge, connectionState !== 'offline' && styles.serverBadgeConnected]}>
                          <Text style={[styles.serverBadgeText, connectionState !== 'offline' && styles.serverBadgeTextConnected]}>
                            {connectionLabel(connectionState)}
                          </Text>
                        </View>
                      </View>
                      <Text style={styles.serverURL} numberOfLines={1}>
                        {server.url}
                      </Text>
                    </View>

                    <View style={styles.serverActions}>
                      <TouchableOpacity
                        style={[styles.compactAction, connectionState !== 'offline' && styles.compactActionPrimary]}
                        onPress={() => connectServer(server)}
                        activeOpacity={0.84}
                      >
                        <Text
                          style={[
                            styles.compactActionText,
                            connectionState !== 'offline' && styles.compactActionTextPrimary,
                          ]}
                        >
                          {connectionState === 'connected'
                            ? 'Reconnect'
                            : connectionState === 'connecting'
                              ? 'Retry'
                              : 'Connect'}
                        </Text>
                      </TouchableOpacity>

                      {connectionState !== 'offline' ? (
                        <TouchableOpacity
                          style={styles.iconAction}
                          onPress={() => disconnectServer(server.id)}
                          activeOpacity={0.84}
                        >
                          <Ionicons name="pause-outline" size={16} color={Colors.textPrimary} />
                        </TouchableOpacity>
                      ) : null}

                      <TouchableOpacity
                        style={styles.iconAction}
                        onPress={() => openEditServer(server)}
                        activeOpacity={0.84}
                      >
                        <Ionicons name="create-outline" size={16} color={Colors.textPrimary} />
                      </TouchableOpacity>

                      <TouchableOpacity
                        style={styles.iconAction}
                        onPress={() => handleDeleteServer(server)}
                        activeOpacity={0.84}
                      >
                        <Ionicons name="trash-outline" size={16} color="#F09999" />
                      </TouchableOpacity>
                    </View>
                  </View>
                </View>
              );
            })
          )}
        </View>

        <Text style={styles.sectionTitle}>Terminal</Text>
        <View style={styles.card}>
          <Text style={styles.label}>Theme</Text>
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
                    <View style={styles.themeSwatches}>
                      {[theme.red, theme.green, theme.yellow, theme.blue, theme.magenta, theme.cyan].map(color => (
                        <View key={color} style={[styles.themeSwatch, { backgroundColor: color }]} />
                      ))}
                    </View>
                    <Text style={[styles.themePreviewText, { color: theme.foreground }]}>
                      $ zen --watch
                    </Text>
                  </View>
                  <Text style={styles.themeName}>{themeName}</Text>
                </TouchableOpacity>
              );
            })}
          </View>
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
            <Text style={styles.modalHint}>
              Each saved server gets its own persistent socket and its own agent sessions.
            </Text>

            <Text style={styles.label}>Name</Text>
            <TextInput
              style={styles.input}
              value={draftName}
              onChangeText={setDraftName}
              placeholder="staging"
              placeholderTextColor={Colors.textSecondary}
              autoCapitalize="none"
              autoCorrect={false}
            />

            <Text style={[styles.label, styles.fieldSpacing]}>WebSocket URL</Text>
            <TextInput
              style={styles.input}
              value={draftUrl}
              onChangeText={setDraftUrl}
              placeholder="ws://your-server:9876"
              placeholderTextColor={Colors.textSecondary}
              autoCapitalize="none"
              autoCorrect={false}
            />

            <View style={styles.modalActions}>
              <TouchableOpacity style={styles.modalButton} onPress={closeEditor} activeOpacity={0.84}>
                <Text style={styles.modalButtonText}>Cancel</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[styles.modalButton, styles.modalButtonPrimary]}
                onPress={handleSaveServer}
                activeOpacity={0.84}
              >
                <Text style={[styles.modalButtonText, styles.modalButtonTextPrimary]}>Save</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.bgPrimary },
  content: { paddingHorizontal: Spacing.screenMargin, paddingTop: 12, paddingBottom: 24 },
  pageHeader: {
    paddingBottom: 8,
  },
  pageTitle: {
    color: Colors.textPrimary,
    fontSize: 24,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: -0.3,
  },
  sectionHeader: {
    flexDirection: 'row',
    alignItems: 'flex-end',
    justifyContent: 'space-between',
    gap: 12,
  },
  sectionCopy: {
    flex: 1,
  },
  sectionTitle: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase',
    letterSpacing: 1,
    marginTop: 24,
    marginBottom: 8,
  },
  sectionTitleFirst: {
    marginTop: 10,
  },
  sectionSubtitle: {
    color: '#8290A4',
    fontSize: 12,
    fontFamily: Typography.uiFont,
    lineHeight: 17,
  },
  addServerButton: {
    height: 34,
    borderRadius: 10,
    paddingHorizontal: 12,
    backgroundColor: Colors.accent,
    alignItems: 'center',
    justifyContent: 'center',
    flexDirection: 'row',
    gap: 4,
    marginBottom: 8,
  },
  addServerButtonText: {
    color: Colors.bgPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  card: {
    backgroundColor: Colors.bgSurface,
    borderRadius: 14,
    padding: 16,
  },
  serverList: {
    marginTop: 12,
    gap: 10,
  },
  serverCard: {
    paddingVertical: 14,
    paddingHorizontal: 14,
  },
  serverCardConnected: {
    borderWidth: 1,
    borderColor: 'rgba(92, 186, 123, 0.26)',
    backgroundColor: '#151C1A',
  },
  serverRow: {
    flexDirection: 'row',
    alignItems: 'center',
  },
  serverMain: {
    flex: 1,
    marginRight: 12,
  },
  serverTitleRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  statusDot: {
    width: 9,
    height: 9,
    borderRadius: 5,
  },
  serverName: {
    color: Colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
    flexShrink: 1,
  },
  serverURL: {
    color: '#90A0B3',
    fontSize: 12,
    fontFamily: Typography.terminalFont,
    marginTop: 6,
  },
  serverBadge: {
    borderRadius: 999,
    paddingHorizontal: 9,
    paddingVertical: 3,
    backgroundColor: '#242E3A',
  },
  serverBadgeConnected: {
    backgroundColor: 'rgba(92, 186, 123, 0.16)',
    borderWidth: 1,
    borderColor: 'rgba(92, 186, 123, 0.3)',
  },
  serverBadgeText: {
    color: '#A0B0C3',
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
  },
  serverBadgeTextConnected: {
    color: '#C6F3D2',
  },
  serverActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  compactAction: {
    height: 34,
    minWidth: 74,
    borderRadius: 10,
    paddingHorizontal: 12,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: Colors.bgElevated,
    borderWidth: 1,
    borderColor: '#2B394C',
  },
  compactActionPrimary: {
    backgroundColor: Colors.accent,
    borderColor: Colors.accent,
  },
  compactActionText: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  compactActionTextPrimary: {
    color: Colors.bgPrimary,
  },
  iconAction: {
    width: 34,
    height: 34,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: Colors.bgElevated,
    borderWidth: 1,
    borderColor: '#2B394C',
  },
  emptyCard: {
    marginTop: 12,
  },
  emptyTitle: {
    color: Colors.textPrimary,
    fontSize: 15,
    fontFamily: Typography.uiFontMedium,
  },
  emptyText: {
    color: '#8C9AAF',
    fontSize: 13,
    fontFamily: Typography.uiFont,
    lineHeight: 19,
    marginTop: 6,
  },
  label: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
    marginBottom: 8,
  },
  fieldSpacing: {
    marginTop: 14,
  },
  input: {
    backgroundColor: Colors.bgElevated,
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 10,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.terminalFont,
    borderWidth: 1,
    borderColor: Colors.bgElevated,
  },
  themeList: { gap: 12 },
  themeCard: {
    borderRadius: 12,
    borderWidth: 1,
    borderColor: Colors.bgElevated,
    padding: 10,
    backgroundColor: Colors.bgPrimary,
  },
  themeCardActive: {
    borderColor: Colors.accent,
    backgroundColor: '#182233',
  },
  themePreview: {
    borderRadius: 10,
    minHeight: 76,
    padding: 12,
    justifyContent: 'space-between',
  },
  themeSwatches: {
    flexDirection: 'row',
    gap: 6,
  },
  themeSwatch: {
    width: 16,
    height: 16,
    borderRadius: 8,
  },
  themePreviewText: {
    fontSize: 13,
    fontFamily: Typography.terminalFont,
  },
  themeName: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
    marginTop: 10,
    textTransform: 'capitalize',
  },
  version: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
    textAlign: 'center',
    marginTop: 32,
  },
  modalRoot: {
    flex: 1,
    justifyContent: 'center',
    paddingHorizontal: 20,
  },
  modalBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: 'rgba(5, 8, 12, 0.66)',
  },
  modalCard: {
    borderRadius: 18,
    padding: 18,
    backgroundColor: '#151D28',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
  },
  modalTitle: {
    color: Colors.textPrimary,
    fontSize: 18,
    fontFamily: Typography.uiFontMedium,
  },
  modalHint: {
    color: '#8796AA',
    fontSize: 12,
    fontFamily: Typography.uiFont,
    marginTop: 6,
    marginBottom: 16,
    lineHeight: 18,
  },
  modalActions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: 10,
    marginTop: 18,
  },
  modalButton: {
    minWidth: 74,
    height: 38,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: Colors.bgElevated,
  },
  modalButtonPrimary: {
    backgroundColor: Colors.accent,
  },
  modalButtonText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  modalButtonTextPrimary: {
    color: Colors.bgPrimary,
  },
});

function connectionLabel(state: ConnectionState): string {
  switch (state) {
    case 'connected':
      return 'Connected';
    case 'connecting':
      return 'Connecting';
    case 'offline':
      return 'Offline';
  }
}

function connectionColor(state: ConnectionState): string {
  switch (state) {
    case 'connected':
      return Colors.statusRunning;
    case 'connecting':
      return '#E7B65C';
    case 'offline':
      return '#65758A';
  }
}
