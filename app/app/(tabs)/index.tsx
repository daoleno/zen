import React, { useEffect, useMemo, useState } from 'react';
import {
  FlatList,
  Modal,
  Platform,
  StatusBar,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Agent, useAgents } from '../../store/agents';
import { AgentStatus, Colors, Spacing, Typography, statusColor } from '../../constants/tokens';
import { TerminalPreview } from '../../components/terminal/TerminalPreview';
import { TerminalThemeName, DefaultTerminalThemeName } from '../../constants/terminalThemes';
import {
  getInboxViewMode,
  getAgentAliases,
  getRecentAgentOpens,
  getServers,
  getTerminalTheme,
  markAgentOpened,
  setAgentAlias,
  setInboxViewMode,
  StoredAgentAliases,
  StoredInboxViewMode,
  StoredRecentAgentOpens,
} from '../../services/storage';

const STATUS_PRIORITY: Record<AgentStatus, number> = {
  failed: 0,
  blocked: 1,
  unknown: 2,
  running: 3,
  done: 4,
};

export default function InboxScreen() {
  const { state } = useAgents();
  const router = useRouter();
  const insets = useSafeAreaInsets();
  const topPadding = Platform.OS === 'android'
    ? (StatusBar.currentHeight || 0) + 4
    : insets.top;
  const [viewMode, setViewModeState] = useState<StoredInboxViewMode>('list');
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [recentAgentOpens, setRecentAgentOpens] = useState<StoredRecentAgentOpens>({});
  const [terminalTheme, setTerminalTheme] = useState<TerminalThemeName>(DefaultTerminalThemeName);
  const [configuredServerCount, setConfiguredServerCount] = useState(0);

  // Context menu state
  const [menuAgent, setMenuAgent] = useState<Agent | null>(null);
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState('');

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const [storedViewMode, storedRecentOpens, storedAliases, storedTheme, storedServers] = await Promise.all([
        getInboxViewMode(),
        getRecentAgentOpens(),
        getAgentAliases(),
        getTerminalTheme(),
        getServers(),
      ]);
      if (!cancelled) {
        setViewModeState(storedViewMode);
        setRecentAgentOpens(storedRecentOpens);
        setAgentAliases(storedAliases);
        setTerminalTheme(storedTheme as TerminalThemeName);
        setConfiguredServerCount(storedServers.length);
      }
    })();
    return () => { cancelled = true; };
  }, []);

  useFocusEffect(
    React.useCallback(() => {
      let cancelled = false;
      (async () => {
        const [storedRecentOpens, storedAliases, storedServers] = await Promise.all([
          getRecentAgentOpens(),
          getAgentAliases(),
          getServers(),
        ]);
        if (!cancelled) {
          setRecentAgentOpens(storedRecentOpens);
          setAgentAliases(storedAliases);
          setConfiguredServerCount(storedServers.length);
        }
      })();
      return () => { cancelled = true; };
    }, []),
  );

  const sortedAgents = useMemo(() => {
    return [...state.agents].sort((left, right) => {
      const leftOpenedAt = recentAgentOpens[left.key] ?? 0;
      const rightOpenedAt = recentAgentOpens[right.key] ?? 0;
      if (leftOpenedAt !== rightOpenedAt) return rightOpenedAt - leftOpenedAt;

      const leftPriority = STATUS_PRIORITY[left.status] ?? 5;
      const rightPriority = STATUS_PRIORITY[right.status] ?? 5;
      if (leftPriority !== rightPriority) return leftPriority - rightPriority;
      return (right.updated_at || 0) - (left.updated_at || 0);
    });
  }, [recentAgentOpens, state.agents]);

  const setViewMode = async (mode: StoredInboxViewMode) => {
    setViewModeState(mode);
    await setInboxViewMode(mode);
  };

  const openAgent = (agent: Agent) => {
    const openedAt = Date.now();
    setRecentAgentOpens(previous => ({
      ...previous,
      [agent.key]: openedAt,
    }));
    void markAgentOpened(agent.key, openedAt);
    router.push({
      pathname: '/terminal/[id]',
      params: { id: agent.id, serverId: agent.serverId },
    });
  };

  const openContextMenu = (agent: Agent) => {
    setMenuAgent(agent);
  };

  const closeContextMenu = () => {
    setMenuAgent(null);
  };

  const openRename = () => {
    if (!menuAgent) return;
    setRenameDraft(agentAliases[menuAgent.key] || menuAgent.name);
    setMenuAgent(null);
    setRenameVisible(true);
  };

  const handleRename = async () => {
    if (!menuAgent) return;
    const updated = await setAgentAlias(menuAgent.key, renameDraft);
    setAgentAliases(updated);
    setRenameVisible(false);
    setMenuAgent(null);
  };

  const closeRename = () => {
    setRenameVisible(false);
    setMenuAgent(null);
  };

  const openServerSettings = (addServer: boolean) => {
    router.push({
      pathname: '/settings',
      params: addServer ? { addServer: Date.now().toString() } : {},
    });
  };

  const hasConfiguredServers = configuredServerCount > 0;
  const hasConnection = Object.keys(state.serverConnections).length > 0;
  const anyConnected = Object.values(state.serverConnections).includes('connected');
  const anyConnecting = Object.values(state.serverConnections).includes('connecting');

  // ── List: compact row ──
  const renderListAgent = ({ item }: { item: Agent }) => (
    <TouchableOpacity
      style={styles.listRow}
      onPress={() => openAgent(item)}
      onLongPress={() => openContextMenu(item)}
      activeOpacity={0.82}
      delayLongPress={400}
    >
      <View style={[styles.statusDot, { backgroundColor: statusColor(item.status) }]} />
      <Text style={styles.listName} numberOfLines={1}>{resolveAgentName(item, agentAliases)}</Text>
      <Text style={styles.listMeta} numberOfLines={1}>
        {item.project || item.serverName}
      </Text>
    </TouchableOpacity>
  );

  // ── Grid: terminal preview card ──
  const renderGridAgent = ({ item }: { item: Agent }) => (
    <TouchableOpacity
      style={styles.gridCard}
      onPress={() => openAgent(item)}
      onLongPress={() => openContextMenu(item)}
      activeOpacity={0.84}
      delayLongPress={400}
    >
      <View style={styles.gridHeader}>
        <View style={[styles.statusDot, { backgroundColor: statusColor(item.status) }]} />
        <Text style={styles.gridTitle} numberOfLines={1}>{resolveAgentName(item, agentAliases)}</Text>
        <Text style={styles.gridMeta} numberOfLines={1}>
          {item.project || item.serverName}
        </Text>
      </View>
      <View style={styles.gridPreview}>
        <TerminalPreview key={item.key} lines={item.last_output_lines} themeName={terminalTheme} />
      </View>
    </TouchableOpacity>
  );

  return (
    <View style={[styles.container, { paddingTop: topPadding }]}>
      {hasConnection && !anyConnected && (
        <View style={styles.banner}>
          <View style={[styles.bannerDot, { backgroundColor: anyConnecting ? Colors.statusUnknown : '#65758A' }]} />
          <Text style={styles.bannerText}>
            {anyConnecting ? 'Connecting' : 'Offline'}
          </Text>
        </View>
      )}

      <View style={styles.header}>
        <Text style={styles.title}>zen</Text>
        <View style={styles.viewToggle}>
          <ToggleButton
            icon="reorder-three-outline"
            selected={viewMode === 'list'}
            onPress={() => setViewMode('list')}
          />
          <ToggleButton
            icon="grid-outline"
            selected={viewMode === 'grid'}
            onPress={() => setViewMode('grid')}
          />
        </View>
      </View>

      {sortedAgents.length === 0 ? (
        <View style={styles.emptyContainer}>
          <Text style={styles.emptyIcon}>☯</Text>
          <Text style={styles.emptyText}>
            {hasConfiguredServers ? (anyConnecting ? 'Connecting to servers' : 'No agents available') : 'No servers configured'}
          </Text>
          <Text style={styles.emptySubtext}>
            {hasConfiguredServers
              ? (anyConnected
                ? 'All is calm'
                : anyConnecting
                  ? 'zen is trying to reconnect. You can still change servers now.'
                  : 'Your saved servers are offline. You can edit them or add another one.')
              : 'Add your first server before zen can load agents.'}
          </Text>
          <View style={styles.emptyActions}>
            <TouchableOpacity
              style={[styles.emptyActionBtn, styles.emptyActionBtnPrimary]}
              onPress={() => openServerSettings(!hasConfiguredServers)}
              activeOpacity={0.82}
            >
              <Text style={[styles.emptyActionText, styles.emptyActionTextPrimary]}>
                {hasConfiguredServers ? 'Open Server Settings' : 'Add Server'}
              </Text>
            </TouchableOpacity>
            {hasConfiguredServers ? (
              <TouchableOpacity
                style={styles.emptyActionBtn}
                onPress={() => openServerSettings(true)}
                activeOpacity={0.82}
              >
                <Text style={styles.emptyActionText}>Add Another Server</Text>
              </TouchableOpacity>
            ) : null}
          </View>
        </View>
      ) : viewMode === 'list' ? (
        <FlatList
          data={sortedAgents}
          key="list"
          keyExtractor={item => item.key}
          renderItem={renderListAgent}
          contentContainerStyle={styles.listContent}
        />
      ) : (
        <FlatList
          data={sortedAgents}
          key="grid"
          keyExtractor={item => item.key}
          renderItem={renderGridAgent}
          contentContainerStyle={styles.gridContent}
          ItemSeparatorComponent={() => <View style={styles.gridGap} />}
          removeClippedSubviews={false}
          windowSize={21}
        />
      )}

      {/* Context Menu */}
      <Modal
        visible={menuAgent !== null && !renameVisible}
        transparent
        animationType="fade"
        onRequestClose={closeContextMenu}
      >
        <View style={styles.menuRoot}>
          <TouchableOpacity
            style={styles.menuBackdrop}
            activeOpacity={1}
            onPress={closeContextMenu}
          />
          <View style={styles.menuCard}>
            <Text style={styles.menuTitle} numberOfLines={1}>
              {menuAgent ? resolveAgentName(menuAgent, agentAliases) : ''}
            </Text>

            <TouchableOpacity style={styles.menuItem} onPress={openRename} activeOpacity={0.82}>
              <Ionicons name="pencil-outline" size={16} color={Colors.textPrimary} />
              <Text style={styles.menuItemText}>Rename</Text>
            </TouchableOpacity>

            <TouchableOpacity
              style={styles.menuItem}
              onPress={() => { if (menuAgent) openAgent(menuAgent); closeContextMenu(); }}
              activeOpacity={0.82}
            >
              <Ionicons name="terminal-outline" size={16} color={Colors.textPrimary} />
              <Text style={styles.menuItemText}>Open Terminal</Text>
            </TouchableOpacity>
          </View>
        </View>
      </Modal>

      {/* Rename Modal */}
      <Modal
        visible={renameVisible}
        transparent
        animationType="fade"
        onRequestClose={closeRename}
      >
        <View style={styles.menuRoot}>
          <TouchableOpacity
            style={styles.menuBackdrop}
            activeOpacity={1}
            onPress={closeRename}
          />
          <View style={styles.renameCard}>
            <Text style={styles.renameTitle}>Rename</Text>
            <TextInput
              style={styles.renameInput}
              value={renameDraft}
              onChangeText={setRenameDraft}
              placeholder="Agent name"
              placeholderTextColor="rgba(255,255,255,0.2)"
              autoCapitalize="none"
              autoCorrect={false}
              autoFocus
              selectTextOnFocus
            />
            <View style={styles.renameActions}>
              <TouchableOpacity style={styles.renameBtn} onPress={closeRename} activeOpacity={0.82}>
                <Text style={styles.renameBtnText}>Cancel</Text>
              </TouchableOpacity>
              <TouchableOpacity
                style={[styles.renameBtn, styles.renameBtnPrimary]}
                onPress={handleRename}
                activeOpacity={0.82}
              >
                <Text style={[styles.renameBtnText, styles.renameBtnPrimaryText]}>Save</Text>
              </TouchableOpacity>
            </View>
          </View>
        </View>
      </Modal>
    </View>
  );
}

function resolveAgentName(agent: Agent, aliases: StoredAgentAliases): string {
  return aliases[agent.key] || agent.name;
}

function ToggleButton({
  icon,
  selected,
  onPress,
}: {
  icon: React.ComponentProps<typeof Ionicons>['name'];
  selected: boolean;
  onPress: () => void;
}) {
  return (
    <TouchableOpacity
      style={[styles.viewBtn, selected && styles.viewBtnActive]}
      onPress={onPress}
      activeOpacity={0.82}
    >
      <Ionicons
        name={icon}
        size={17}
        color={selected ? Colors.textPrimary : Colors.textSecondary}
      />
    </TouchableOpacity>
  );
}

function stripAnsi(value: string): string {
  return value.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '');
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },

  // Banner
  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    paddingVertical: 6,
    backgroundColor: 'rgba(255,255,255,0.03)',
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: 'rgba(255,255,255,0.06)',
  },
  bannerDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  bannerText: {
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },

  // Header
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: 20,
    paddingTop: 8,
    paddingBottom: 12,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 22,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 1,
    opacity: 0.9,
  },
  viewToggle: {
    flexDirection: 'row',
    gap: 2,
    borderRadius: 12,
    backgroundColor: 'rgba(255,255,255,0.04)',
    padding: 3,
  },
  viewBtn: {
    width: 32,
    height: 32,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
  },
  viewBtnActive: {
    backgroundColor: 'rgba(255,255,255,0.08)',
  },

  // ── List: compact rows ──
  listContent: {
    paddingHorizontal: 20,
    paddingBottom: 32,
  },
  listRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: 'rgba(255,255,255,0.04)',
  },
  statusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  listName: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  listMeta: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
    opacity: 0.5,
    maxWidth: '40%',
    textAlign: 'right',
  },

  // ── Grid: terminal preview cards ──
  gridContent: {
    paddingHorizontal: 20,
    paddingBottom: 32,
  },
  gridGap: {
    height: 12,
  },
  gridCard: {
    borderRadius: 14,
    backgroundColor: 'rgba(255,255,255,0.03)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.06)',
    overflow: 'hidden',
  },
  gridHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingHorizontal: 14,
    paddingVertical: 10,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: 'rgba(255,255,255,0.04)',
  },
  gridTitle: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
    flexShrink: 1,
  },
  gridMeta: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFont,
    marginLeft: 'auto',
    opacity: 0.5,
  },
  gridPreview: {
    height: 220,
  },

  // Empty state
  emptyContainer: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: 24,
  },
  emptyIcon: {
    fontSize: 44,
    color: Colors.textSecondary,
    marginBottom: 16,
    opacity: 0.6,
  },
  emptyText: {
    color: Colors.textPrimary,
    fontSize: 17,
    fontFamily: Typography.uiFontMedium,
    opacity: 0.8,
  },
  emptySubtext: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontFamily: Typography.uiFont,
    marginTop: 6,
    maxWidth: 280,
    textAlign: 'center',
    opacity: 0.6,
  },
  emptyActions: {
    width: '100%',
    maxWidth: 280,
    gap: 10,
    marginTop: 22,
  },
  emptyActionBtn: {
    width: '100%',
    minHeight: 40,
    paddingHorizontal: 14,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.12)',
    backgroundColor: 'rgba(255,255,255,0.04)',
  },
  emptyActionBtnPrimary: {
    backgroundColor: Colors.accent,
    borderColor: Colors.accent,
  },
  emptyActionText: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
    textAlign: 'center',
  },
  emptyActionTextPrimary: {
    color: Colors.bgPrimary,
  },

  // Context menu
  menuRoot: {
    flex: 1,
    justifyContent: 'flex-end',
  },
  menuBackdrop: {
    ...StyleSheet.absoluteFillObject,
    backgroundColor: 'rgba(0,0,0,0.6)',
  },
  menuCard: {
    marginHorizontal: 12,
    marginBottom: 32,
    borderRadius: 16,
    backgroundColor: '#1A1A22',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    overflow: 'hidden',
  },
  menuTitle: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    paddingHorizontal: 18,
    paddingTop: 16,
    paddingBottom: 10,
    opacity: 0.6,
  },
  menuItem: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingHorizontal: 18,
    paddingVertical: 14,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: 'rgba(255,255,255,0.05)',
  },
  menuItemText: {
    color: Colors.textPrimary,
    fontSize: 15,
    fontFamily: Typography.uiFont,
  },

  // Rename modal
  renameCard: {
    marginHorizontal: 24,
    marginBottom: 100,
    borderRadius: 16,
    padding: 20,
    backgroundColor: '#141418',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
  },
  renameTitle: {
    color: Colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 16,
  },
  renameInput: {
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
  renameActions: {
    flexDirection: 'row',
    justifyContent: 'flex-end',
    gap: 10,
    marginTop: 20,
  },
  renameBtn: {
    minWidth: 70,
    height: 36,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: 'rgba(255,255,255,0.05)',
  },
  renameBtnPrimary: {
    backgroundColor: Colors.accent,
  },
  renameBtnText: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  renameBtnPrimaryText: {
    color: Colors.bgPrimary,
  },
});
