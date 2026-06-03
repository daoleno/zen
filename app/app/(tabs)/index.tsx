import React, { useEffect, useMemo, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  FlatList,
  KeyboardAvoidingView,
  Linking,
  Modal,
  Platform,
  ScrollView,
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
import { useWork, type WorkItem } from '../../store/work';
import { AgentStatus, Colors, Typography, statusColor, useAppColors } from '../../constants/tokens';
import { TerminalPreview } from '../../components/terminal/TerminalPreview';
import { AgentKindIcon } from '../../components/terminal/AgentKindIcon';
import { NewTerminalSheet } from '../../components/terminal/NewTerminalSheet';
import {
  getInboxViewMode,
  getAgentAliases,
  getRecentAgentOpens,
  getServers,
  markAgentOpened,
  setAgentAlias,
  setInboxViewMode,
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
import type { SessionService } from '../../services/sessionServices';

const STATUS_PRIORITY: Record<AgentStatus, number> = {
  failed: 0,
  blocked: 1,
  unknown: 2,
  running: 3,
  done: 4,
};

type DiscoveredSessionService = SessionService & {
  serverId: string;
  serverName: string;
};

export default function InboxScreen() {
  const { state } = useAgents();
  const { state: workState } = useWork();
  const router = useRouter();
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);

  const agentWorkMap = useMemo(() => {
    const map: Record<string, WorkItem> = {};
    for (const current of Object.values(workState.byKey)) {
      if (current.frontmatter.done || !current.frontmatter.agent_session) {
        continue;
      }
      map[`${current.serverId}:${current.frontmatter.agent_session}`] = current;
    }
    return map;
  }, [workState.byKey]);
  const [viewMode, setViewModeState] = useState<StoredInboxViewMode>('list');
  const [agentAliases, setAgentAliases] = useState<StoredAgentAliases>({});
  const [recentAgentOpens, setRecentAgentOpens] = useState<StoredRecentAgentOpens>({});
  const [configuredServerCount, setConfiguredServerCount] = useState(0);
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [storageHydrated, setStorageHydrated] = useState(false);
  const [createSheetVisible, setCreateSheetVisible] = useState(false);
  const [selectedCreateServerId, setSelectedCreateServerId] = useState<string | null>(null);
  const [creatingServerId, setCreatingServerId] = useState<string | null>(null);
  const [serviceSheetVisible, setServiceSheetVisible] = useState(false);
  const [sessionServices, setSessionServices] = useState<DiscoveredSessionService[]>([]);
  const [servicesLoading, setServicesLoading] = useState(false);
  const [servicesError, setServicesError] = useState<string | null>(null);
  const agentsHydrated = useMemo(
    () => servers.some(server => state.hydratedServers[server.id]),
    [servers, state.hydratedServers],
  );

  const [menuAgent, setMenuAgent] = useState<Agent | null>(null);
  const [renameVisible, setRenameVisible] = useState(false);
  const [renameDraft, setRenameDraft] = useState('');
  const [renameAgentKey, setRenameAgentKey] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      const [storedViewMode, storedRecentOpens, storedAliases, storedServers] = await Promise.all([
        getInboxViewMode(),
        getRecentAgentOpens(),
        getAgentAliases(),
        getServers(),
      ]);
      if (!cancelled) {
        setViewModeState(storedViewMode);
        setRecentAgentOpens(storedRecentOpens);
        setAgentAliases(storedAliases);
        setConfiguredServerCount(storedServers.length);
        setServers(storedServers);
        setStorageHydrated(true);
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
  const connectedServerIds = useMemo(
    () => servers
      .filter(server => state.serverConnections[server.id] === 'connected')
      .map(server => server.id),
    [servers, state.serverConnections],
  );
  const waitingForInitialAgentSnapshot = storageHydrated &&
    connectedServerIds.some(serverId => !state.hydratedServers[serverId]);
  const shouldShowInitialLoading =
    (!storageHydrated && sortedAgents.length === 0) ||
    (
      !agentsHydrated &&
      sortedAgents.length === 0 &&
      hasConfiguredServers &&
      (anyConnecting || waitingForInitialAgentSnapshot)
    );
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

  const finishCreateTerminal = async (
    serverId: string,
    agentId: string,
    hint?: { cwd: string; command: string; name: string; startedAt: number },
  ) => {
    const sessionKey = makeSessionKey(serverId, agentId);
    const openedAt = Date.now();
    void markAgentOpened(sessionKey, openedAt);
    setRecentAgentOpens(previous => ({
      ...previous,
      [sessionKey]: openedAt,
    }));
    router.push({
      pathname: '/terminal/[id]',
      params: hint
        ? {
            id: agentId,
            serverId,
            cwd: hint.cwd,
            command: hint.command,
            name: hint.name,
            startedAt: String(hint.startedAt),
          }
        : { id: agentId, serverId },
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
      const startedAt = Date.now();
      const agentId = await wsClient.createSession(server.id, {
        cwd: input.cwd,
        command: input.command,
        name: input.name,
      });
      await finishCreateTerminal(server.id, agentId, { ...input, startedAt });
    } catch (error: any) {
      Alert.alert('Could not create terminal', error?.message || 'Try reconnecting to that daemon first.');
    } finally {
      setCreatingServerId(null);
    }
  };

  const refreshSessionServices = async () => {
    if (connectedServers.length === 0) {
      setSessionServices([]);
      setServicesError(null);
      return;
    }

    setServicesLoading(true);
    setServicesError(null);
    try {
      const results = await Promise.allSettled(
        connectedServers.map(async (server) => {
          const snapshot = await wsClient.listSessionServices(server.id);
          return snapshot.services.map<DiscoveredSessionService>((service) => ({
            ...service,
            serverId: server.id,
            serverName: server.name,
          }));
        }),
      );

      const services = results
        .flatMap(result => result.status === 'fulfilled' ? result.value : [])
        .sort((left, right) => {
          if (left.serverName !== right.serverName) return left.serverName.localeCompare(right.serverName);
          const leftProject = serviceProjectLabel(left);
          const rightProject = serviceProjectLabel(right);
          if (leftProject !== rightProject) return leftProject.localeCompare(rightProject);
          return left.port - right.port;
        });

      const failures = results.filter(result => result.status === 'rejected');
      setSessionServices(services);
      setServicesError(
        failures.length > 0
          ? `${failures.length} daemon${failures.length === 1 ? '' : 's'} did not return services.`
          : null,
      );
    } catch (error: any) {
      setSessionServices([]);
      setServicesError(error?.message || 'Failed to load services.');
    } finally {
      setServicesLoading(false);
    }
  };

  const openSessionServices = () => {
    if (connectedServers.length === 0) {
      Alert.alert(
        'Daemon unavailable',
        'Connect to a daemon before viewing session services.',
      );
      return;
    }
    setServiceSheetVisible(true);
    void refreshSessionServices();
  };

  const openServiceTerminal = (service: DiscoveredSessionService) => {
    setServiceSheetVisible(false);
    router.push({
      pathname: '/terminal/[id]',
      params: { id: service.agent_id, serverId: service.serverId },
    });
  };

  const openServiceURL = async (url: string) => {
    try {
      await Linking.openURL(url);
    } catch (error: any) {
      Alert.alert('Could not open URL', error?.message || url);
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

  useEffect(() => {
    if (!createSheetVisible || !selectedCreateServerId) {
      return;
    }
    wsClient.requestBrainSnapshot(selectedCreateServerId);
  }, [createSheetVisible, selectedCreateServerId]);


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
      'This will terminate ' + presentAgent(target, agentAliases[target.key]).title + ' on ' + target.serverName + '.',
      [
        { text: 'Cancel', style: 'cancel' },
        {
          text: 'Terminate',
          style: 'destructive',
          onPress: () => {
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
    ? connectionIssueAccent(primaryIssue, colors)
    : anyConnecting
      ? colors.statusUnknown
      : colors.disabledText;
  const bannerText = primaryIssue?.title || (anyConnecting ? 'Connecting' : 'Offline');
  const emptyTitle = !hasConfiguredServers
    ? 'No servers configured'
    : anyConnected
      ? 'Connected, waiting for agent data'
      : primaryIssue?.title || (anyConnecting ? 'Connecting to servers' : 'No agents available');
  const emptySubtext = !hasConfiguredServers
    ? 'Add your first server before Zen can load agents.'
    : anyConnected
      ? 'Zen is connected to your daemon, but no agent data has arrived yet. Start Claude or Codex, or check the tmux watcher.'
      : primaryIssue
        ? `${primaryIssue.detail} ${primaryIssue.hint}`
        : anyConnecting
          ? 'Zen is trying to reconnect. You can still change servers now.'
          : 'Your saved servers are offline. You can edit them or add another one.';

  const renderListAgent = ({ item, index }: { item: Agent; index: number }) => {
    const presented = presentAgent(item, agentAliases[item.key]);
    const sessionTitle = resolveSessionTitle(item, presented, agentWorkMap);
    const directoryLabel = relativeDirectoryLabel(item, showServerNames);
    const activityLine = resolveSessionActivity(item, presented);
    return (
      <TouchableOpacity
        style={[
          styles.sessionRow,
          index === sortedAgents.length - 1 && styles.sessionRowLast,
        ]}
        onPress={() => openAgent(item)}
        onLongPress={() => openContextMenu(item)}
        activeOpacity={0.82}
        delayLongPress={400}
      >
        <View style={styles.sessionStatusColumn}>
          <View style={[styles.sessionStatusDot, { backgroundColor: statusColor(item.status) }]} />
        </View>
        <View style={styles.sessionBody}>
          <View style={styles.sessionTitleLine}>
            <Text style={styles.sessionDirectory} numberOfLines={1}>
              {directoryLabel}
            </Text>
            <Text style={styles.sessionPathSeparator}>{'>'}</Text>
            <Text style={styles.sessionName} numberOfLines={1}>
              {sessionTitle}
            </Text>
          </View>
          <View style={styles.sessionMetaLine}>
            <AgentKindIcon kind={presented.kind} size={11} />
            <Text style={styles.sessionMetaText} numberOfLines={1}>
              {activityLine}
            </Text>
          </View>
        </View>
        <Ionicons name="chevron-forward" size={14} color={colors.disabledText} />
      </TouchableOpacity>
    );
  };

  const renderGridAgent = ({ item }: { item: Agent }) => {
    const presented = presentAgent(item, agentAliases[item.key]);
    const sessionTitle = resolveSessionTitle(item, presented, agentWorkMap);
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
          <Text style={styles.gridTitle} numberOfLines={1}>
            {relativeDirectoryLabel(item, showServerNames)} {'>'} {sessionTitle}
          </Text>
          {item.serverName ? (
            <Text style={styles.gridMeta} numberOfLines={1}>{item.serverName}</Text>
          ) : null}
          <View style={[styles.statusDot, { backgroundColor: statusColor(item.status) }]} />
        </View>
        <View style={styles.gridPreview}>
          <TerminalPreview key={item.key} lines={item.last_output_lines} />
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
          <Text style={styles.title}>Zen</Text>
          <Text style={styles.headerSummary}>{headerSummary}</Text>
        </View>
        <View style={styles.headerActions}>
          <TouchableOpacity
            style={[styles.serviceButton, !anyConnected && { opacity: 0.5 }]}
            onPress={openSessionServices}
            activeOpacity={0.82}
          >
            <Ionicons
              name="globe-outline"
              size={18}
              color={anyConnected ? colors.accent : colors.disabledText}
            />
          </TouchableOpacity>
          <TouchableOpacity
            style={[styles.addButton, creatingServerId && { opacity: 0.5 }]}
            onPress={openCreateTerminal}
            disabled={!!creatingServerId}
            activeOpacity={0.82}
          >
            <Ionicons name="add" size={19} color={colors.accent} />
          </TouchableOpacity>
          <View style={styles.viewToggle}>
          <ToggleButton
            icon="reorder-three-outline"
            selected={viewMode === 'list'}
            onPress={() => setViewMode('list')}
            colors={colors}
            styles={styles}
          />
          <ToggleButton
            icon="grid-outline"
            selected={viewMode === 'grid'}
            onPress={() => setViewMode('grid')}
            colors={colors}
            styles={styles}
          />
          </View>
        </View>
      </View>

      {shouldShowInitialLoading ? (
        <View style={styles.loadingContainer}>
          <ActivityIndicator color={colors.accent} />
        </View>
      ) : sortedAgents.length === 0 ? (
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
          renderItem={renderListAgent}
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

      <Modal
        visible={serviceSheetVisible}
        transparent
        animationType="slide"
        onRequestClose={() => setServiceSheetVisible(false)}
      >
        <View style={styles.serviceModalRoot}>
          <TouchableOpacity
            style={styles.menuBackdrop}
            activeOpacity={1}
            onPress={() => setServiceSheetVisible(false)}
          />
          <View style={styles.serviceSheet}>
            <View style={styles.serviceSheetHeader}>
              <View style={styles.serviceSheetTitleBlock}>
                <Text style={styles.serviceSheetTitle}>Services</Text>
                <Text style={styles.serviceSheetMeta}>
                  {connectedServers.length} daemon{connectedServers.length === 1 ? '' : 's'} · {sessionServices.length} port{sessionServices.length === 1 ? '' : 's'}
                </Text>
              </View>
              <TouchableOpacity
                style={styles.serviceIconButton}
                onPress={() => void refreshSessionServices()}
                disabled={servicesLoading}
                activeOpacity={0.82}
              >
                {servicesLoading ? (
                  <ActivityIndicator size="small" color={colors.accent} />
                ) : (
                  <Ionicons name="refresh" size={17} color={colors.textSecondary} />
                )}
              </TouchableOpacity>
              <TouchableOpacity
                style={styles.serviceIconButton}
                onPress={() => setServiceSheetVisible(false)}
                activeOpacity={0.82}
              >
                <Ionicons name="close" size={19} color={colors.textSecondary} />
              </TouchableOpacity>
            </View>

            {servicesError ? (
              <Text style={styles.serviceError}>{servicesError}</Text>
            ) : null}

            {servicesLoading && sessionServices.length === 0 ? (
              <View style={styles.serviceLoading}>
                <ActivityIndicator color={colors.accent} />
              </View>
            ) : sessionServices.length === 0 ? (
              <View style={styles.serviceEmpty}>
                <Ionicons name="radio-outline" size={22} color={colors.textSecondary} />
                <Text style={styles.serviceEmptyText}>No listening services found.</Text>
              </View>
            ) : (
              <ScrollView
                style={styles.serviceScroll}
                contentContainerStyle={styles.serviceList}
                showsVerticalScrollIndicator={false}
              >
                {sessionServices.map(service => (
                  <View key={`${service.serverId}:${service.id}`} style={styles.serviceItem}>
                    <TouchableOpacity
                      style={styles.serviceItemHeader}
                      onPress={() => openServiceTerminal(service)}
                      activeOpacity={0.82}
                    >
                      <View style={styles.serviceMain}>
                        <Text style={styles.serviceTitle} numberOfLines={1}>
                          {serviceProjectLabel(service)}
                        </Text>
                        <Text style={styles.servicePort}>:{service.port}</Text>
                      </View>
                      <Ionicons name="terminal-outline" size={16} color={colors.textSecondary} />
                    </TouchableOpacity>
                    <Text style={styles.serviceMeta} numberOfLines={1}>
                      {service.serverName} · {shortAgentLabel(service.agent_name)}
                    </Text>
                    <Text style={styles.serviceProcess} numberOfLines={1}>
                      {shortProcessLabel(service.process || service.command || '')}
                    </Text>
                    <View style={styles.serviceURLRow}>
                      {(service.urls ?? []).length > 0 ? (
                        (service.urls ?? []).map(item => (
                          <TouchableOpacity
                            key={item.url}
                            style={styles.serviceURLChip}
                            onPress={() => void openServiceURL(item.url)}
                            activeOpacity={0.82}
                          >
                            <Text style={styles.serviceURLLabel}>{item.label}</Text>
                            <Text style={styles.serviceURLText} numberOfLines={1}>
                              {item.address}:{service.port}
                            </Text>
                          </TouchableOpacity>
                        ))
                      ) : (
                        <View style={styles.serviceLocalChip}>
                          <Text style={styles.serviceLocalText}>
                            {(service.binds ?? []).length > 0 ? (service.binds ?? []).join(', ') : 'localhost'}:{service.port}
                          </Text>
                        </View>
                      )}
                    </View>
                  </View>
                ))}
              </ScrollView>
            )}
          </View>
        </View>
      </Modal>

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
              <Ionicons name="pencil-outline" size={16} color={colors.textPrimary} />
              <Text style={styles.menuItemText}>Rename</Text>
            </TouchableOpacity>

            <TouchableOpacity
              style={styles.menuItem}
              onPress={() => { if (menuAgent) openAgent(menuAgent); closeContextMenu(); }}
              activeOpacity={0.82}
            >
              <Ionicons name="terminal-outline" size={16} color={colors.textPrimary} />
              <Text style={styles.menuItemText}>Open Terminal</Text>
            </TouchableOpacity>

            <TouchableOpacity style={styles.menuItem} onPress={handleTerminateAgent} activeOpacity={0.82}>
              <Ionicons name="power-outline" size={16} color={colors.dangerText} />
              <Text style={[styles.menuItemText, styles.menuItemTextDestructive]}>Terminate</Text>
            </TouchableOpacity>
          </View>
        </View>
      </Modal>

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
              placeholderTextColor={colors.textSecondary}
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
  colors,
  styles,
}: {
  icon: React.ComponentProps<typeof Ionicons>['name'];
  selected: boolean;
  onPress: () => void;
  colors: typeof Colors;
  styles: ReturnType<typeof createStyles>;
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
        color={selected ? colors.accent : colors.disabledText}
        style={!selected && styles.viewIconInactive}
      />
    </TouchableOpacity>
  );
}

function resolveSessionTitle(
  agent: Agent,
  presented: ReturnType<typeof presentAgent>,
  workMap: Record<string, WorkItem>,
): string {
  if (presented.titleSource !== 'default') {
    return presented.title;
  }

  const linkedWork = workMap[`${agent.serverId}:${agent.id}`];
  const workTitle = linkedWork?.title?.trim();
  if (workTitle) {
    return workTitle;
  }

  return shortAgentLabel(agent.name) || presented.shortTitle || presented.title;
}

function relativeDirectoryLabel(agent: Agent, showServerName: boolean): string {
  const directory = compactDirectoryPath(agent.cwd, agent.project);
  const fallback = agent.project?.trim() || 'no directory';
  const label = directory || fallback;
  if (!showServerName || !agent.serverName) {
    return label;
  }
  return `${agent.serverName}/${label}`;
}

function compactDirectoryPath(value?: string, project?: string): string {
  const trimmed = value?.trim().replace(/\/+$/, '') || '';
  if (!trimmed) {
    return '';
  }
  if (trimmed === '/') {
    return '/';
  }

  const relative = trimmed.replace(/^\.\//, '');
  if (!relative.startsWith('/')) {
    return relative;
  }

  const parts = relative.split('/').filter(Boolean);
  if (parts.length === 0) {
    return '/';
  }

  const projectName = project?.trim();
  if (projectName) {
    const projectIndex = findLastPathIndex(parts, part => part === projectName);
    if (projectIndex >= 0) {
      return parts.slice(projectIndex).join('/');
    }
  }

  const markerIndex = findLastPathIndex(parts, part =>
    ['workspace', 'workspaces', 'project', 'projects', 'code', 'src', 'repo', 'repos'].includes(part.toLowerCase()),
  );
  if (markerIndex >= 0 && markerIndex < parts.length - 1) {
    return parts.slice(markerIndex + 1).join('/');
  }

  return parts.slice(-2).join('/');
}

function findLastPathIndex(parts: string[], predicate: (part: string) => boolean): number {
  for (let index = parts.length - 1; index >= 0; index -= 1) {
    if (predicate(parts[index])) {
      return index;
    }
  }
  return -1;
}

function resolveSessionActivity(agent: Agent, presented: ReturnType<typeof presentAgent>): string {
  const preview = compactActivityText(agent.summary) || compactActivityText(lastNonEmptyLine(agent.last_output_lines));
  return [statusLabel(agent.status), presented.typeLabel, preview].filter(Boolean).join(' · ');
}

function statusLabel(status: AgentStatus): string {
  switch (status) {
    case 'running':
      return 'running';
    case 'blocked':
      return 'blocked';
    case 'done':
      return 'done';
    case 'failed':
      return 'failed';
    default:
      return 'unknown';
  }
}

function lastNonEmptyLine(lines: string[]): string {
  for (let index = lines.length - 1; index >= 0; index -= 1) {
    const line = lines[index]?.trim();
    if (line) {
      return line;
    }
  }
  return '';
}

function compactActivityText(value?: string): string {
  const compact = stripAnsi(value || '').replace(/\s+/g, ' ').trim();
  if (!compact) {
    return '';
  }
  return compact.length > 120 ? `${compact.slice(0, 117)}...` : compact;
}

function stripAnsi(value: string): string {
  return value.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '');
}

function lastPathSegment(value?: string): string {
  const trimmed = value?.trim().replace(/\/+$/, '') || '';
  if (!trimmed || trimmed === '/') {
    return trimmed;
  }

  const parts = trimmed.split('/').filter(Boolean);
  return parts[parts.length - 1] || trimmed;
}

function serviceProjectLabel(service: Pick<DiscoveredSessionService, 'project' | 'cwd' | 'agent_name'>): string {
  return service.project?.trim() || lastPathSegment(service.cwd) || shortAgentLabel(service.agent_name) || 'service';
}

function shortAgentLabel(value?: string): string {
  const trimmed = value?.trim() || '';
  return trimmed.replace(/\s+\([^)]+\)\s*$/, '') || trimmed;
}

function shortProcessLabel(value: string): string {
  const trimmed = value.replace(/\s+/g, ' ').trim();
  if (!trimmed) {
    return 'process';
  }
  return trimmed.length > 96 ? `${trimmed.slice(0, 93)}...` : trimmed;
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.bgPrimary,
  },

  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 6,
    paddingVertical: 6,
    backgroundColor: colors.surfaceSubtle,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.borderSubtle,
  },
  bannerDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  bannerText: {
    color: colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },

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
    color: colors.textPrimary,
    fontSize: 22,
    lineHeight: 28,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 0,
    opacity: 0.9,
    paddingRight: 4,
  },
  headerSummary: {
    marginTop: 2,
    color: colors.textSecondary,
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
    backgroundColor: colors.surfaceActive,
  },
  serviceButton: {
    width: 34,
    height: 34,
    borderRadius: 11,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.surfaceSubtle,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
  },
  viewToggle: {
    flexDirection: 'row',
    gap: 2,
    borderRadius: 12,
    backgroundColor: colors.surfaceSubtle,
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
    backgroundColor: colors.surfaceActive,
  },
  viewIconInactive: {
    opacity: 0.72,
  },

  promptContent: {
    paddingHorizontal: 14,
    paddingTop: 2,
    paddingBottom: 30,
  },
  sessionRow: {
    minHeight: 52,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    paddingVertical: 8,
    paddingHorizontal: 2,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.borderSubtle,
  },
  sessionRowLast: {
    borderBottomWidth: 0,
  },
  sessionStatusColumn: {
    width: 10,
    minHeight: 36,
    alignItems: 'center',
    justifyContent: 'center',
  },
  sessionStatusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  sessionBody: {
    flex: 1,
    minWidth: 0,
    gap: 4,
  },
  sessionTitleLine: {
    minWidth: 0,
    flexDirection: 'row',
    alignItems: 'baseline',
  },
  sessionDirectory: {
    flexShrink: 1,
    maxWidth: '48%',
    color: colors.textSecondary,
    fontSize: 13,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
    opacity: 0.78,
    includeFontPadding: false,
  },
  sessionPathSeparator: {
    flexShrink: 0,
    color: colors.textSecondary,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.terminalFontBold,
    marginHorizontal: 7,
    opacity: 0.46,
    includeFontPadding: false,
  },
  sessionName: {
    flex: 1,
    minWidth: 0,
    color: colors.textPrimary,
    fontSize: 14,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
    opacity: 0.92,
    includeFontPadding: false,
  },
  sessionMetaLine: {
    minWidth: 0,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 7,
  },
  sessionMetaText: {
    flex: 1,
    minWidth: 0,
    color: colors.textSecondary,
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFont,
    opacity: 0.58,
    includeFontPadding: false,
  },
  statusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },

  loadingContainer: {
    flex: 1,
    alignItems: 'center',
    justifyContent: 'center',
  },
  gridContent: {
    paddingHorizontal: 20,
    paddingBottom: 32,
  },
  gridGap: {
    height: 12,
  },
  gridCard: {
    borderRadius: 14,
    backgroundColor: colors.surfaceSubtle,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
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
    borderBottomColor: colors.borderSubtle,
  },
  gridTitle: {
    flex: 1,
    minWidth: 0,
    color: colors.textPrimary,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  gridMeta: {
    color: colors.textSecondary,
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

  emptyContainer: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: 24,
  },
  emptyIcon: {
    fontSize: 44,
    color: colors.textSecondary,
    marginBottom: 16,
    opacity: 0.6,
  },
  emptyText: {
    color: colors.textPrimary,
    fontSize: 17,
    fontFamily: Typography.uiFontMedium,
    opacity: 0.8,
  },
  emptySubtext: {
    color: colors.textSecondary,
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
    borderColor: colors.borderStrong,
    backgroundColor: colors.surfaceSubtle,
  },
  emptyActionBtnPrimary: {
    backgroundColor: colors.accent,
    borderColor: colors.accent,
  },
  emptyActionText: {
    color: colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
    textAlign: 'center',
  },
  emptyActionTextPrimary: {
    color: colors.textOnAccent,
  },

  serviceModalRoot: {
    flex: 1,
    justifyContent: 'flex-end',
  },
  serviceSheet: {
    maxHeight: '78%',
    marginHorizontal: 10,
    marginBottom: 20,
    borderRadius: 16,
    backgroundColor: colors.modalSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    overflow: 'hidden',
  },
  serviceSheetHeader: {
    minHeight: 58,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    paddingHorizontal: 16,
    paddingVertical: 12,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: colors.borderSubtle,
  },
  serviceSheetTitleBlock: {
    flex: 1,
    minWidth: 0,
  },
  serviceSheetTitle: {
    color: colors.textPrimary,
    fontSize: 16,
    lineHeight: 21,
    fontFamily: Typography.uiFontMedium,
  },
  serviceSheetMeta: {
    marginTop: 2,
    color: colors.textSecondary,
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFont,
    opacity: 0.62,
  },
  serviceIconButton: {
    width: 34,
    height: 34,
    borderRadius: 10,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.surfaceSubtle,
  },
  serviceError: {
    paddingHorizontal: 16,
    paddingTop: 10,
    color: colors.dangerText,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFont,
  },
  serviceLoading: {
    minHeight: 160,
    alignItems: 'center',
    justifyContent: 'center',
  },
  serviceEmpty: {
    minHeight: 180,
    alignItems: 'center',
    justifyContent: 'center',
    gap: 10,
  },
  serviceEmptyText: {
    color: colors.textSecondary,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  serviceScroll: {
    maxHeight: 520,
  },
  serviceList: {
    padding: 12,
    gap: 10,
  },
  serviceItem: {
    borderRadius: 12,
    backgroundColor: colors.surfaceSubtle,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderSubtle,
    padding: 12,
  },
  serviceItemHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
    minHeight: 24,
  },
  serviceMain: {
    flex: 1,
    minWidth: 0,
    flexDirection: 'row',
    alignItems: 'baseline',
  },
  serviceTitle: {
    flexShrink: 1,
    color: colors.textPrimary,
    fontSize: 14,
    lineHeight: 19,
    fontFamily: Typography.uiFontMedium,
  },
  servicePort: {
    color: colors.promptYellow,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.terminalFontBold,
    marginLeft: 2,
  },
  serviceMeta: {
    marginTop: 2,
    color: colors.textSecondary,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
    opacity: 0.6,
  },
  serviceProcess: {
    marginTop: 5,
    color: colors.textSecondary,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
    opacity: 0.78,
  },
  serviceURLRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 8,
    marginTop: 10,
  },
  serviceURLChip: {
    maxWidth: '100%',
    minHeight: 34,
    borderRadius: 10,
    paddingHorizontal: 10,
    paddingVertical: 7,
    backgroundColor: colors.surfaceActive,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.borderStrong,
  },
  serviceURLLabel: {
    color: colors.accent,
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase',
  },
  serviceURLText: {
    marginTop: 1,
    color: colors.textPrimary,
    fontSize: 12,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  serviceLocalChip: {
    minHeight: 32,
    borderRadius: 10,
    paddingHorizontal: 10,
    paddingVertical: 8,
    backgroundColor: colors.surfacePressed,
  },
  serviceLocalText: {
    color: colors.textSecondary,
    fontSize: 12,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
    opacity: 0.72,
  },

  menuRoot: {
    flex: 1,
    justifyContent: 'flex-end',
  },
  menuBackdrop: {
    ...StyleSheet.absoluteFill,
    backgroundColor: colors.modalBackdrop,
  },
  menuCard: {
    marginHorizontal: 12,
    marginBottom: 32,
    borderRadius: 16,
    backgroundColor: colors.modalSurfaceAlt,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    overflow: 'hidden',
  },
  menuTitle: {
    color: colors.textSecondary,
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
    borderTopColor: colors.borderSubtle,
  },
  menuItemText: {
    color: colors.textPrimary,
    fontSize: 15,
    fontFamily: Typography.uiFont,
  },
  menuItemTextDestructive: {
    color: colors.dangerText,
  },

  renameCard: {
    marginHorizontal: 24,
    marginBottom: 100,
    borderRadius: 16,
    padding: 20,
    backgroundColor: colors.modalSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
  },
  renameTitle: {
    color: colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 16,
  },
  renameInput: {
    backgroundColor: colors.inputBackground,
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 10,
    color: colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.terminalFont,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
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
    backgroundColor: colors.surfacePressed,
  },
  renameBtnPrimary: {
    backgroundColor: colors.accent,
  },
  renameBtnText: {
    color: colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  renameBtnPrimaryText: {
    color: colors.textOnAccent,
  },
  });
}
