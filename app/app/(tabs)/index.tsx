import React, { useEffect, useMemo, useState } from 'react';
import {
  Alert,
  FlatList,
  KeyboardAvoidingView,
  Modal,
  Platform,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Agent, useAgents } from '../../store/agents';
import { useIssues, type Issue } from '../../store/issues';
import { AgentStatus, Colors, Typography, statusColor } from '../../constants/tokens';
import { TerminalPreview } from '../../components/terminal/TerminalPreview';
import { AgentKindIcon } from '../../components/terminal/AgentKindIcon';
import { NewTerminalSheet } from '../../components/terminal/NewTerminalSheet';
import { TerminalThemeName, DefaultTerminalThemeName } from '../../constants/terminalThemes';
import {
  closeTerminalTab,
  getInboxViewMode,
  getAgentAliases,
  getRecentAgentOpens,
  getServers,
  getTerminalTheme,
  markAgentOpened,
  setAgentAlias,
  setInboxViewMode,
  touchTerminalTab,
  StoredAgentAliases,
  StoredInboxViewMode,
  StoredRecentAgentOpens,
  StoredServer,
} from '../../services/storage';
import { connectionIssueAccent } from '../../services/connectionIssue';
import { wsClient } from '../../services/websocket';
import { makeSessionKey } from '../../services/sessionKeys';
import { presentAgent } from '../../services/agentPresentation';
import {
  filterAgentsByPreferredServers,
  groupAgentsByDirectory,
} from '../../services/serverSelection';

const STATUS_PRIORITY: Record<AgentStatus, number> = {
  failed: 0,
  blocked: 1,
  unknown: 2,
  running: 3,
  done: 4,
};

export default function InboxScreen() {
  const { state } = useAgents();
  const { state: issuesState } = useIssues();
  const router = useRouter();

  // Build agent→issue lookup for subtitle display
  const agentIssueMap = useMemo(() => {
    const map: Record<string, Issue> = {};
    for (const current of Object.values(issuesState.byKey)) {
      if (current.frontmatter.done || !current.frontmatter.agent_session) {
        continue;
      }
      map[`${current.serverId}:${current.frontmatter.agent_session}`] = current;
    }
    return map;
  }, [issuesState.byKey]);
  const [viewMode, setViewModeState] = useState<StoredInboxViewMode>('list');
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [recentAgentOpens, setRecentAgentOpens] = useState<StoredRecentAgentOpens>({});
  const [terminalTheme, setTerminalTheme] = useState<TerminalThemeName>(DefaultTerminalThemeName);
  const [configuredServerCount, setConfiguredServerCount] = useState(0);
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [createSheetVisible, setCreateSheetVisible] = useState(false);
  const [selectedCreateServerId, setSelectedCreateServerId] = useState<string | null>(null);
  const [creatingServerId, setCreatingServerId] = useState<string | null>(null);

  // Context menu state
  const [menuAgent, setMenuAgent] = useState<Agent | null>(null);
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState('');
  const [renameAgentKey, setRenameAgentKey] = useState<string | null>(null);

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
        setServers(storedServers);
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
          setServers(storedServers);
        }
      })();
      return () => { cancelled = true; };
    }, []),
  );

  const displayAgents = useMemo(
    () =>
      filterAgentsByPreferredServers({
        agents: state.agents,
        servers,
        connectionStates: state.serverConnections,
        latencyById: state.serverLatencyById,
      }),
    [servers, state.agents, state.serverConnections, state.serverLatencyById],
  );

  const sortedAgents = useMemo(() => {
    const agentsByPriority = [...displayAgents].sort((left, right) => {
      const leftOpenedAt = recentAgentOpens[left.key] ?? 0;
      const rightOpenedAt = recentAgentOpens[right.key] ?? 0;
      if (leftOpenedAt !== rightOpenedAt) return rightOpenedAt - leftOpenedAt;

      const leftPriority = STATUS_PRIORITY[left.status] ?? 5;
      const rightPriority = STATUS_PRIORITY[right.status] ?? 5;
      if (leftPriority !== rightPriority) return leftPriority - rightPriority;
      return (right.updated_at || 0) - (left.updated_at || 0);
    });
    return groupAgentsByDirectory(agentsByPriority).flatMap(section => section.data);
  }, [displayAgents, recentAgentOpens]);

  const showServerNames = useMemo(
    () => new Set(sortedAgents.map((agent) => agent.serverId)).size > 1,
    [sortedAgents],
  );
  const hasConfiguredServers = configuredServerCount > 0;
  const hasConnection = Object.keys(state.serverConnections).length > 0;
  const anyConnected = Object.values(state.serverConnections).includes('connected');
  const anyConnecting = Object.values(state.serverConnections).includes('connecting');
  const groupedAgents = useMemo(
    () => groupAgentsByDirectory(sortedAgents, { showServerName: showServerNames }),
    [showServerNames, sortedAgents],
  );
  const headerSummary = useMemo(() => {
    if (sortedAgents.length === 0) {
      if (anyConnecting) return 'reconnecting';
      if (anyConnected) return 'connected';
      if (hasConfiguredServers) return 'offline';
      return 'no servers';
    }

    const workspaceLabel = groupedAgents.length === 1 ? '1 workspace' : `${groupedAgents.length} workspaces`;
    const sessionLabel = sortedAgents.length === 1 ? '1 session' : `${sortedAgents.length} sessions`;
    return `${workspaceLabel} · ${sessionLabel}`;
  }, [anyConnected, anyConnecting, groupedAgents.length, hasConfiguredServers, sortedAgents.length]);
  const primaryIssue = useMemo(() => {
    let nextIssue: (typeof state.serverConnectionIssues)[string] | null = null;
    for (const issue of Object.values(state.serverConnectionIssues)) {
      if (!issue) {
        continue;
      }
      if (!nextIssue || issue.checkedAt > nextIssue.checkedAt) {
        nextIssue = issue;
      }
    }
    return nextIssue;
  }, [state.serverConnectionIssues]);

  const connectedServers = useMemo(
    () => servers.filter(server => state.serverConnections[server.id] === 'connected'),
    [servers, state.serverConnections],
  );
  const createServerOptions = useMemo(
    () => connectedServers.map(server => ({ id: server.id, name: server.name })),
    [connectedServers],
  );

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
    setRenameAgentKey(menuAgent.key);
    setRenameDraft(agentAliases[menuAgent.key] || menuAgent.name);
    setMenuAgent(null);
    setRenameVisible(true);
  };

  const handleRename = async () => {
    if (!renameAgentKey) return;
    const updated = await setAgentAlias(renameAgentKey, renameDraft);
    setAgentAliases(updated);
    setRenameVisible(false);
    setRenameAgentKey(null);
  };

  const closeRename = () => {
    setRenameVisible(false);
    setRenameAgentKey(null);
  };

  const finishCreateTerminal = async (serverId: string, agentId: string) => {
    const sessionKey = makeSessionKey(serverId, agentId);
    const openedAt = Date.now();
    await touchTerminalTab(sessionKey);
    void markAgentOpened(sessionKey, openedAt);
    setRecentAgentOpens(previous => ({
      ...previous,
      [sessionKey]: openedAt,
    }));
    router.push({
      pathname: '/terminal/[id]',
      params: { id: agentId, serverId },
    });
  };

  const findSuggestedCwd = (serverId: string): string => {
    const onServer = sortedAgents.filter(agent => agent.serverId === serverId && agent.cwd);
    return onServer[0]?.cwd?.trim() || '';
  };

  const createTerminalOnServer = async (input: {
    serverId: string;
    cwd: string;
    command: string;
    name: string;
  }) => {
    const server = connectedServers.find(item => item.id === input.serverId);
    if (!server) {
      Alert.alert('Daemon unavailable', 'Connect to a daemon before creating a new terminal.');
      return;
    }

    setCreateSheetVisible(false);
    setCreatingServerId(server.id);
    try {
      const agentId = await wsClient.createSession(server.id, {
        cwd: input.cwd,
        command: input.command,
        name: input.name,
      });
      await finishCreateTerminal(server.id, agentId);
    } catch (error: any) {
      Alert.alert('Could not create terminal', error?.message || 'Try reconnecting to that daemon first.');
    } finally {
      setCreatingServerId(null);
    }
  };

  const openCreateTerminal = () => {
    if (connectedServers.length === 0) {
      Alert.alert(
        'Daemon unavailable',
        'Connect to a daemon before creating a new terminal.',
      );
      return;
    }
    setSelectedCreateServerId(previous => previous && connectedServers.some(server => server.id === previous)
      ? previous
      : connectedServers[0].id);
    setCreateSheetVisible(true);
  };


  const handleTerminateAgent = () => {
    if (!menuAgent) return;

    const target = menuAgent;
    closeContextMenu();

    if (state.serverConnections[target.serverId] !== 'connected') {
      Alert.alert(
        'Daemon unavailable',
        'Reconnect to that daemon before terminating the agent.',
      );
      return;
    }

    Alert.alert(
      'Terminate?',
      'This will terminate ' + presentAgent(target, agentAliases[target.key]).title + ' on ' + target.serverName + '. It does more than closing the tab.',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Terminate',
          style: 'destructive',
          onPress: () => {
            void closeTerminalTab(target.key);
            wsClient.killAgent(target.serverId, target.id);
          },
        },
      ],
    );
  };


  const openServerSettings = (addServer: boolean) => {
    router.push({
      pathname: '/settings',
      params: addServer ? { addServer: Date.now().toString() } : {},
    });
  };

  const bannerAccent = primaryIssue
    ? connectionIssueAccent(primaryIssue)
    : anyConnecting
      ? Colors.statusUnknown
      : '#65758A';
  const bannerText = primaryIssue?.title || (anyConnecting ? 'Connecting' : 'Offline');
  const emptyTitle = !hasConfiguredServers
    ? 'No servers configured'
    : anyConnected
      ? 'Connected, waiting for agent data'
      : primaryIssue?.title || (anyConnecting ? 'Connecting to servers' : 'No agents available');
  const emptySubtext = !hasConfiguredServers
    ? 'Add your first server before zen can load agents.'
    : anyConnected
      ? 'zen is connected to your daemon, but no agent data has arrived yet. Start Claude or Codex, or check the tmux watcher.'
      : primaryIssue
        ? `${primaryIssue.detail} ${primaryIssue.hint}`
        : anyConnecting
          ? 'zen is trying to reconnect. You can still change servers now.'
          : 'Your saved servers are offline. You can edit them or add another one.';

  const renderPromptAgent = ({ item }: { item: Agent }) => {
    const presented = presentAgent(item, agentAliases[item.key]);
    const directoryLabel = promptDirectoryLabel(item, presented.cwdBase, showServerNames);
    const promptTitle = resolvePromptTitle(item, presented, agentIssueMap);
    const meta = buildPromptMeta(item, presented, promptTitle, agentIssueMap);
    const hasMeta = Boolean(meta);
    return (
      <TouchableOpacity
        style={[styles.promptRow, hasMeta && styles.promptRowWithMeta]}
        onPress={() => openAgent(item)}
        onLongPress={() => openContextMenu(item)}
        activeOpacity={0.82}
        delayLongPress={400}
      >
        <View style={styles.promptLine}>
          <View style={styles.promptIcon}>
            <AgentKindIcon kind={presented.kind} size={13} />
          </View>
          <View style={styles.promptPrefix}>
            <Text style={styles.promptDirectory} numberOfLines={1}>{directoryLabel}</Text>
            <Text style={styles.promptArrow}>❯</Text>
          </View>
          <Text style={styles.promptTitle} numberOfLines={1}>{promptTitle}</Text>
          <View style={[styles.promptStatusDot, { backgroundColor: statusColor(item.status) }]} />
        </View>
        {meta ? (
          <Text style={styles.promptMeta} numberOfLines={1}>{meta}</Text>
        ) : null}
      </TouchableOpacity>
    );
  };

  // ── Grid: terminal preview card ──
  const renderGridAgent = ({ item }: { item: Agent }) => {
    const presented = presentAgent(item, agentAliases[item.key]);
    return (
      <TouchableOpacity
        style={styles.gridCard}
        onPress={() => openAgent(item)}
        onLongPress={() => openContextMenu(item)}
        activeOpacity={0.84}
        delayLongPress={400}
      >
        <View style={styles.gridHeader}>
          <AgentKindIcon kind={presented.kind} size={16} />
          <Text style={styles.gridTitle} numberOfLines={1}>{presented.cwdBase || presented.title}</Text>
          {item.serverName ? (
            <Text style={styles.gridMeta} numberOfLines={1}>{item.serverName}</Text>
          ) : null}
          <View style={[styles.statusDot, { backgroundColor: statusColor(item.status) }]} />
        </View>
        <View style={styles.gridPreview}>
          <TerminalPreview key={item.key} lines={item.last_output_lines} themeName={terminalTheme} />
        </View>
      </TouchableOpacity>
    );
  };

  return (
    <SafeAreaView style={styles.container} edges={['top']}>
      {hasConnection && !anyConnected && (
        <View style={styles.banner}>
          <View style={[styles.bannerDot, { backgroundColor: bannerAccent }]} />
          <Text style={styles.bannerText}>{bannerText}</Text>
        </View>
      )}

      <View style={styles.header}>
        <View style={styles.headerBrand}>
          <Text style={styles.title}>zen</Text>
          <Text style={styles.headerSummary}>{headerSummary}</Text>
        </View>
        <View style={styles.headerActions}>
          <TouchableOpacity
            style={[styles.addButton, creatingServerId && { opacity: 0.5 }]}
            onPress={openCreateTerminal}
            disabled={!!creatingServerId}
            activeOpacity={0.82}
          >
            <Ionicons name="add" size={19} color={Colors.textPrimary} />
          </TouchableOpacity>
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
      </View>

      {sortedAgents.length === 0 ? (
        <View style={styles.emptyContainer}>
          <Text style={styles.emptyIcon}>☯</Text>
          <Text style={styles.emptyText}>{emptyTitle}</Text>
          <Text style={styles.emptySubtext}>{emptySubtext}</Text>
          <View style={styles.emptyActions}>
            {connectedServers.length > 0 ? (
              <TouchableOpacity
                style={[styles.emptyActionBtn, styles.emptyActionBtnPrimary]}
                onPress={openCreateTerminal}
                disabled={!!creatingServerId}
                activeOpacity={0.82}
              >
                <Text style={[styles.emptyActionText, styles.emptyActionTextPrimary]}>
                  {creatingServerId ? 'Starting Terminal…' : 'New Terminal'}
                </Text>
              </TouchableOpacity>
            ) : null}
            <TouchableOpacity
              style={[
                styles.emptyActionBtn,
                connectedServers.length === 0 && styles.emptyActionBtnPrimary,
              ]}
              onPress={() => openServerSettings(!hasConfiguredServers)}
              activeOpacity={0.82}
            >
              <Text style={[
                styles.emptyActionText,
                connectedServers.length === 0 && styles.emptyActionTextPrimary,
              ]}>
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
          renderItem={renderPromptAgent}
          contentContainerStyle={styles.promptContent}
          removeClippedSubviews={false}
          windowSize={15}
          showsVerticalScrollIndicator={false}
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

      <NewTerminalSheet
        visible={createSheetVisible}
        title="New Terminal"
        subtitle="Open a plain shell, or launch Claude/Codex in a real working directory."
        initialCwd={selectedCreateServerId ? findSuggestedCwd(selectedCreateServerId) : ''}
        serverOptions={createServerOptions}
        selectedServerId={selectedCreateServerId}
        onSelectServer={setSelectedCreateServerId}
        submitting={!!creatingServerId}
        onClose={() => setCreateSheetVisible(false)}
        onSubmit={input => {
          if (!input.serverId) return;
          void createTerminalOnServer({
            serverId: input.serverId,
            cwd: input.cwd,
            command: input.command,
            name: input.name,
          });
        }}
      />

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
              {menuAgent ? presentAgent(menuAgent, agentAliases[menuAgent.key]).title : ''}
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

            <TouchableOpacity style={styles.menuItem} onPress={handleTerminateAgent} activeOpacity={0.82}>
              <Ionicons name="power-outline" size={16} color="#F09999" />
              <Text style={[styles.menuItemText, styles.menuItemTextDestructive]}>Terminate</Text>
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
        <KeyboardAvoidingView
          style={styles.menuRoot}
          behavior={Platform.OS === 'ios' ? 'padding' : 'height'}
        >
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
        </KeyboardAvoidingView>
      </Modal>

    </SafeAreaView>
  );
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

function resolvePromptTitle(
  agent: Agent,
  presented: ReturnType<typeof presentAgent>,
  issueMap: Record<string, Issue>,
): string {
  if (presented.titleSource !== 'default') {
    return presented.title;
  }

  const linkedIssue = issueMap[`${agent.serverId}:${agent.id}`];
  const issueTitle = linkedIssue?.title?.trim();
  if (issueTitle) {
    return issueTitle;
  }

  return presented.title;
}

function buildPromptMeta(
  agent: Agent,
  presented: ReturnType<typeof presentAgent>,
  promptTitle: string,
  issueMap: Record<string, Issue>,
): string {
  const summary = agent.summary.trim();
  if ((agent.status === 'blocked' || agent.status === 'failed') && summary) {
    return summary;
  }

  const linkedIssue = issueMap[`${agent.serverId}:${agent.id}`];
  const issueTitle = linkedIssue?.title?.trim() || linkedIssue?.id || '';

  if (issueTitle && issueTitle !== promptTitle) {
    return issueTitle;
  }

  if (presented.titleSource === 'default') {
    return '';
  }

  return '';
}

function promptDirectoryLabel(agent: Agent, cwdBase: string, showServerNames: boolean): string {
  const directory = cwdBase || agent.project?.trim() || lastPathSegment(agent.cwd) || 'session';
  if (!showServerNames || !agent.serverName) {
    return directory;
  }

  return `${agent.serverName}@${directory}`;
}

function lastPathSegment(value?: string): string {
  const trimmed = value?.trim().replace(/\/+$/, '') || '';
  if (!trimmed || trimmed === '/') {
    return trimmed;
  }

  const parts = trimmed.split('/').filter(Boolean);
  return parts[parts.length - 1] || trimmed;
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
    paddingHorizontal: 16,
    paddingTop: 8,
    paddingBottom: 10,
  },
  headerBrand: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 22,
    lineHeight: 28,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 1,
    opacity: 0.9,
    paddingRight: 4,
  },
  headerSummary: {
    marginTop: 2,
    color: Colors.textSecondary,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
    opacity: 0.58,
  },
  headerActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  addButton: {
    width: 34,
    height: 34,
    borderRadius: 11,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: 'rgba(255,255,255,0.05)',
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

  // ── Prompt list ──
  promptContent: {
    paddingHorizontal: 16,
    paddingTop: 6,
    paddingBottom: 28,
  },
  promptRow: {
    minHeight: 42,
    paddingVertical: 7,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: 'rgba(235,225,207,0.055)',
  },
  promptRowWithMeta: {
    minHeight: 54,
    paddingVertical: 8,
  },
  promptLine: {
    minHeight: 24,
    flexDirection: 'row',
    alignItems: 'center',
  },
  promptPrefix: {
    maxWidth: '48%',
    flexDirection: 'row',
    alignItems: 'center',
    flexShrink: 1,
    marginRight: 10,
  },
  promptDirectory: {
    flexShrink: 1,
    color: '#8FB573',
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
    letterSpacing: -0.1,
  },
  promptArrow: {
    color: '#E6B450',
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.terminalFontBold,
    marginLeft: 6,
  },
  promptIcon: {
    marginRight: 10,
    opacity: 0.86,
  },
  promptTitle: {
    flex: 1,
    minWidth: 0,
    color: Colors.textPrimary,
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
    opacity: 0.92,
  },
  promptStatusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
    marginLeft: 8,
  },
  promptMeta: {
    marginTop: 1,
    paddingLeft: 1,
    color: Colors.textSecondary,
    fontSize: 10,
    lineHeight: 13,
    fontFamily: Typography.uiFont,
    opacity: 0.42,
  },
  statusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
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
    paddingVertical: 11,
    minHeight: 44,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: 'rgba(255,255,255,0.04)',
  },
  gridTitle: {
    flex: 1,
    minWidth: 0,
    color: Colors.textPrimary,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  gridMeta: {
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
    flexShrink: 1,
    maxWidth: '42%',
    marginLeft: 12,
    textAlign: 'right',
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
  menuItemTextDestructive: {
    color: '#F09999',
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
