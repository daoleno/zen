import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  FlatList,
  LayoutAnimation,
  NativeScrollEvent,
  NativeSyntheticEvent,
  PanResponder,
  Platform,
  SectionList,
  Linking,
  ScrollView,
  StyleSheet,
  Text,
  TextInput,
  TouchableOpacity,
  View,
} from 'react-native';
import { useFocusEffect, useRouter } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Agent, useAgents } from '../../store/agents';
import { useWork, type WorkItem } from '../../store/work';
import { AgentStatus, Colors, Radii, Typography, useAppColors, useAppTheme, shadow } from '../../constants/tokens';
import type { ResolvedZenTheme } from '../../theme';
import { createThemedSurfaces, glassCardShadow } from '../../constants/themedSurfaces';
import { AgentsListHeader, type AgentsStatusFilter } from '../../components/agents/AgentsListHeader';
import { AnimatedPressable } from '../../components/ui/AnimatedPressable';
import { MeditationModal } from '../../components/meditation/MeditationModal';
import { MeditationPullPreview } from '../../components/meditation/MeditationPullPreview';
import { RisingSheet } from '../../components/ui/RisingSheet';
import { Enter } from '../../components/ui/Enter';
import { TerminalPreview } from '../../components/terminal/TerminalPreview';
import { AgentKindIcon } from '../../components/terminal/AgentKindIcon';
import { AgentSessionRow } from '../../components/agents/AgentSessionRow';
import { formatTelegramListTime } from '../../constants/telegramPresentation';
import { formatAgentSessionPreview } from '../../services/sessionPreview';
import { NewTerminalSheet } from '../../components/terminal/NewTerminalSheet';
import { SessionServicesSheet } from '../../components/SessionServicesSheet';
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
  type AgentDirectorySection,
} from '../../services/serverSelection';
import {
  serviceProjectLabel,
  shortAgentLabel,
  type DiscoveredSessionService,
} from '../../services/sessionServicesPresentation';

const STATUS_PRIORITY: Record<AgentStatus, number> = {
  running: 0,
  blocked: 1,
  failed: 1,
  unknown: 1,
  done: 1,
};
const MEDITATION_PULL_THRESHOLD = 132;

export default function InboxScreen() {
  const { state } = useAgents();
  const { state: workState } = useWork();
  const router = useRouter();
  const colors = useAppColors();
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);

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
  const [searchQuery, setSearchQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState<AgentsStatusFilter>('all');
  const [headerMenuVisible, setHeaderMenuVisible] = useState(false);
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
  const [meditationVisible, setMeditationVisible] = useState(false);
  const [meditationPullDistance, setMeditationPullDistance] = useState(0);
  const meditationPullDistanceRef = useRef(0);
  const scrollYRef = useRef(0);
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

  const filterOptions = useMemo(
    () => [
      { key: 'all' as const, label: 'All', count: sortedAgents.length },
      {
        key: 'running' as const,
        label: 'Running',
        count: sortedAgents.filter((agent) => agent.status === 'running').length,
      },
      {
        key: 'brain' as const,
        label: 'Brain',
        count: sortedAgents.filter((agent) => agent.delegated).length,
      },
    ],
    [sortedAgents],
  );

  const visibleAgents = useMemo(() => {
    const query = searchQuery.trim().toLowerCase();
    return sortedAgents.filter((agent) => {
      if (statusFilter === 'running' && agent.status !== 'running') return false;
      if (statusFilter === 'brain' && !agent.delegated) return false;
      if (!query) return true;

      const presented = presentAgent(agent, agentAliases[agent.key]);
      const linkedWork = agentWorkMap[`${agent.serverId}:${agent.id}`];
      return [
        presented.title,
        presented.shortTitle,
        agent.name,
        agent.cwd,
        agent.command,
        agent.serverName,
        linkedWork?.title,
      ].some(value => (value || '').toLowerCase().includes(query));
    });
  }, [agentAliases, agentWorkMap, searchQuery, sortedAgents, statusFilter]);

  const showServerNames = useMemo(
    () => new Set(visibleAgents.map((agent) => agent.serverId)).size > 1,
    [visibleAgents],
  );

  // Animate row insertions/removals/reorders with a gentle layout transition,
  // so the list settles instead of snapping when sessions come and go.
  const prevAgentKeysRef = useRef<string[]>([]);
  useEffect(() => {
    const nextKeys = sortedAgents.map(a => a.key);
    const prevKeys = prevAgentKeysRef.current;
    const changed =
      nextKeys.length !== prevKeys.length ||
      nextKeys.some((k, i) => prevKeys[i] !== k);
    if (changed && prevKeys.length > 0 && nextKeys.length > 0) {
      LayoutAnimation.configureNext(
        LayoutAnimation.create(
          260,
          LayoutAnimation.Types.easeInEaseOut,
          LayoutAnimation.Properties.opacity,
        ),
      );
    }
    prevAgentKeysRef.current = nextKeys;
  }, [sortedAgents]);
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
  const listSections = useMemo(
    () => groupAgentsByDirectory(visibleAgents, { showServerName: showServerNames }),
    [showServerNames, visibleAgents],
  );
  const useSectionHeaders = listSections.length > 1;
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

  const openMeditation = () => {
    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
    meditationPullDistanceRef.current = 0;
    setMeditationPullDistance(0);
    setMeditationVisible(true);
  };

  const updateMeditationPull = (distance: number) => {
    const nextDistance = Math.max(0, distance);
    meditationPullDistanceRef.current = nextDistance;
    setMeditationPullDistance(nextDistance);
  };

  const handleContentScroll = (event: NativeSyntheticEvent<NativeScrollEvent>) => {
    scrollYRef.current = Math.max(0, event.nativeEvent.contentOffset.y);
    if (scrollYRef.current > 0 && meditationPullDistanceRef.current > 0) {
      updateMeditationPull(0);
    }
  };

  const finishMeditationPull = () => {
    if (meditationPullDistanceRef.current >= MEDITATION_PULL_THRESHOLD) {
      openMeditation();
      return;
    }
    updateMeditationPull(0);
  };

  const meditationPullResponder = useMemo(
    () =>
      PanResponder.create({
        onMoveShouldSetPanResponderCapture: (_, gesture) =>
          scrollYRef.current <= 0 &&
          gesture.dy > 8 &&
          Math.abs(gesture.dy) > Math.abs(gesture.dx) * 1.25,
        onPanResponderMove: (_, gesture) => {
          if (scrollYRef.current > 0) {
            updateMeditationPull(0);
            return;
          }
          updateMeditationPull(gesture.dy);
        },
        onPanResponderRelease: finishMeditationPull,
        onPanResponderTerminate: finishMeditationPull,
      }),
    [],
  );

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
    ? 'No servers'
    : anyConnected
      ? 'No sessions yet'
      : primaryIssue?.title || (anyConnecting ? 'Connecting' : 'Offline');
  const emptySubtext = !hasConfiguredServers
    ? 'Add a server in Settings.'
    : anyConnected
      ? 'Start an agent on your daemon, or create a terminal.'
      : primaryIssue?.detail || (anyConnecting ? null : 'Check server connection in Settings.');

  const renderListAgent = ({ item }: { item: Agent }) => {
    const presented = presentAgent(item, agentAliases[item.key]);
    const sessionTitle = resolveSessionTitle(item, presented, agentWorkMap);
    const sessionPreview = formatAgentSessionPreview(item, {
      showServerName: showServerNames,
      serverName: item.serverName,
    });
    return (
      <AgentSessionRow
        title={sessionTitle}
        kind={presented.kind}
        terminalFlavor={presented.terminalFlavor}
        preview={sessionPreview.text}
        previewTone={sessionPreview.tone}
        previewPrefix={sessionPreview.prefix}
        timeLabel={item.status === 'running' ? 'live' : formatTelegramListTime(item.updated_at)}
        timeActive={item.status === 'running'}
        running={item.status === 'running'}
        showBrainBadge={Boolean(item.delegated)}
        active={item.status === 'running'}
        onPress={() => openAgent(item)}
        onLongPress={() => {
          Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
          openContextMenu(item);
        }}
      />
    );
  };

  const renderListSectionHeader = ({ section }: { section: AgentDirectorySection }) => {
    if (!useSectionHeaders) {
      return null;
    }
    return (
      <View style={styles.sectionHeader}>
        <Text style={styles.sectionTitle} numberOfLines={1}>
          {section.title}
        </Text>
      </View>
    );
  };

  const renderGridAgent = ({ item }: { item: Agent }) => {
    const presented = presentAgent(item, agentAliases[item.key]);
    const sessionTitle = resolveSessionTitle(item, presented, agentWorkMap);
    return (
      <AnimatedPressable
        style={styles.gridCard}
        preset="card"
        scale={0.97}
        onPress={() => openAgent(item)}
        onLongPress={() => {
          Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
          openContextMenu(item);
        }}
        delayLongPress={400}
      >
        <View style={styles.gridHeader}>
          <View style={styles.gridHeaderMain}>
            <AgentKindIcon
              kind={presented.kind}
              flavor={presented.terminalFlavor}
              size={15}
            />
            <Text style={styles.gridTitle} numberOfLines={1}>
              {sessionTitle}
            </Text>
            {item.delegated ? (
              <BrainSessionBadge colors={colors} styles={styles} compact />
            ) : null}
          </View>
          <AgentRunningIndicator
            running={item.status === 'running'}
            colors={colors}
            styles={styles}
          />
        </View>
        <View style={styles.gridPreview}>
          <TerminalPreview key={item.key} lines={item.last_output_lines} />
        </View>
      </AnimatedPressable>
    );
  };

  return (
    <SafeAreaView
      style={styles.container}
      edges={['top']}
      {...meditationPullResponder.panHandlers}
    >
      <MeditationPullPreview
        pullDistance={meditationPullDistance}
        progress={meditationPullDistance / MEDITATION_PULL_THRESHOLD}
      />

      {hasConnection && !anyConnected && (
        <View style={styles.banner}>
          <View style={[styles.bannerDot, { backgroundColor: bannerAccent }]} />
          <Text style={styles.bannerText}>{bannerText}</Text>
        </View>
      )}

      <AgentsListHeader
        searchQuery={searchQuery}
        statusFilter={statusFilter}
        filterOptions={filterOptions}
        onSearchChange={setSearchQuery}
        onFilterChange={setStatusFilter}
        onOpenMenu={() => setHeaderMenuVisible(true)}
      />

      {shouldShowInitialLoading ? (
        <ScrollView
          style={styles.flex}
          contentContainerStyle={styles.loadingContainer}
          onScroll={handleContentScroll}
          scrollEventThrottle={16}
          alwaysBounceVertical
          showsVerticalScrollIndicator={false}
        >
          <ActivityIndicator color={colors.accent} />
        </ScrollView>
      ) : visibleAgents.length === 0 ? (
        <ScrollView
          style={styles.flex}
          contentContainerStyle={styles.emptyScrollContent}
          onScroll={handleContentScroll}
          scrollEventThrottle={16}
          alwaysBounceVertical
          showsVerticalScrollIndicator={false}
        >
          <Enter preset="rise" style={styles.emptyContainer}>
            <Enter preset="pop">
              <View style={styles.emptyBadge}>
                <Text style={styles.emptyIcon}>☯</Text>
              </View>
            </Enter>
            <Text style={styles.emptyText}>{emptyTitle}</Text>
            {emptySubtext ? (
              <Text style={styles.emptySubtext}>{emptySubtext}</Text>
            ) : null}
            <View style={styles.emptyActions}>
              {connectedServers.length > 0 ? (
                <AnimatedPressable
                  style={[styles.emptyActionBtn, styles.emptyActionBtnPrimary]}
                  preset="press"
                  scale={0.95}
                  onPress={openCreateTerminal}
                  disabled={!!creatingServerId}
                >
                  <Ionicons name="add" size={18} color={colors.textOnAccent} style={styles.emptyActionIcon} />
                  <Text style={[styles.emptyActionText, styles.emptyActionTextPrimary]}>
                    {creatingServerId ? 'Starting…' : 'New terminal'}
                  </Text>
                </AnimatedPressable>
              ) : (
                <AnimatedPressable
                  style={[styles.emptyActionBtn, styles.emptyActionBtnPrimary]}
                  preset="press"
                  scale={0.95}
                  onPress={() => openServerSettings(true)}
                >
                  <Ionicons name="server-outline" size={18} color={colors.textOnAccent} style={styles.emptyActionIcon} />
                  <Text style={[styles.emptyActionText, styles.emptyActionTextPrimary]}>
                    Add server
                  </Text>
                </AnimatedPressable>
              )}
              {hasConfiguredServers ? (
                <AnimatedPressable
                  style={styles.emptyActionLink}
                  preset="press"
                  scale={0.96}
                  onPress={() => openServerSettings(false)}
                >
                  <Text style={styles.emptyActionLinkText}>Open Settings</Text>
                </AnimatedPressable>
              ) : null}
            </View>
          </Enter>
        </ScrollView>
      ) : viewMode === 'list' ? (
        <SectionList
          sections={listSections}
          key="list"
          keyExtractor={item => item.key}
          renderItem={renderListAgent}
          renderSectionHeader={renderListSectionHeader}
          stickySectionHeadersEnabled={false}
          contentContainerStyle={styles.promptContent}
          onScroll={handleContentScroll}
          scrollEventThrottle={16}
          alwaysBounceVertical
          removeClippedSubviews={false}
          windowSize={15}
          showsVerticalScrollIndicator={false}
          ItemSeparatorComponent={() => <View style={styles.rowGap} />}
          SectionSeparatorComponent={() => <View style={styles.sectionGap} />}
        />
      ) : (
        <FlatList
          data={visibleAgents}
          key="grid"
          keyExtractor={item => item.key}
          renderItem={renderGridAgent}
          contentContainerStyle={styles.gridContent}
          onScroll={handleContentScroll}
          scrollEventThrottle={16}
          alwaysBounceVertical
          ItemSeparatorComponent={() => <View style={styles.gridGap} />}
          removeClippedSubviews={false}
          windowSize={21}
        />
      )}

      <SessionServicesSheet
        visible={serviceSheetVisible}
        services={sessionServices}
        loading={servicesLoading}
        error={servicesError}
        showServerSections={connectedServers.length > 1}
        onClose={() => setServiceSheetVisible(false)}
        onRefresh={() => void refreshSessionServices()}
        onOpenTerminal={openServiceTerminal}
        onOpenURL={url => void openServiceURL(url)}
      />

      <NewTerminalSheet
        visible={createSheetVisible}
        title="Session"
        subtitle=""
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

      {visibleAgents.length > 0 ? (
        <AnimatedPressable
          style={[styles.listFab, (!anyConnected || !!creatingServerId) && styles.listFabDisabled]}
          preset="press"
          scale={0.92}
          onPress={openCreateTerminal}
          disabled={!!creatingServerId || !anyConnected}
          accessibilityLabel="New terminal"
        >
          <Ionicons
            name={creatingServerId ? 'hourglass-outline' : 'add'}
            size={28}
            color={colors.textOnAccent}
          />
        </AnimatedPressable>
      ) : null}

      {meditationVisible ? (
        <MeditationModal
          visible={meditationVisible}
          colors={colors}
          onClose={() => setMeditationVisible(false)}
        />
      ) : null}

      <RisingSheet
        visible={headerMenuVisible}
        onClose={() => setHeaderMenuVisible(false)}
        cardStyle={styles.menuCard}
        align="bottom"
      >
        <Text style={styles.menuTitle}>Agents</Text>

        <AnimatedPressable
          style={styles.menuItem}
          preset="press"
          scale={0.98}
          onPress={() => {
            setHeaderMenuVisible(false);
            void setViewMode('list');
          }}
        >
          <Ionicons
            name="reorder-three-outline"
            size={16}
            color={viewMode === 'list' ? colors.accent : colors.textPrimary}
          />
          <Text style={styles.menuItemText}>List view</Text>
        </AnimatedPressable>

        <AnimatedPressable
          style={styles.menuItem}
          preset="press"
          scale={0.98}
          onPress={() => {
            setHeaderMenuVisible(false);
            void setViewMode('grid');
          }}
        >
          <Ionicons
            name="grid-outline"
            size={16}
            color={viewMode === 'grid' ? colors.accent : colors.textPrimary}
          />
          <Text style={styles.menuItemText}>Grid view</Text>
        </AnimatedPressable>

        <AnimatedPressable
          style={styles.menuItem}
          preset="press"
          scale={0.98}
          disabled={!anyConnected}
          onPress={() => {
            setHeaderMenuVisible(false);
            openSessionServices();
          }}
        >
          <Ionicons
            name="globe-outline"
            size={16}
            color={anyConnected ? colors.textPrimary : colors.disabledText}
          />
          <Text style={[styles.menuItemText, !anyConnected && { color: colors.disabledText }]}>
            Session services
          </Text>
        </AnimatedPressable>

        <AnimatedPressable
          style={styles.menuItem}
          preset="press"
          scale={0.98}
          onPress={() => {
            setHeaderMenuVisible(false);
            openServerSettings(false);
          }}
        >
          <Ionicons name="settings-outline" size={16} color={colors.textPrimary} />
          <Text style={styles.menuItemText}>Settings</Text>
        </AnimatedPressable>
      </RisingSheet>

      <RisingSheet
        visible={menuAgent !== null && !renameVisible}
        onClose={closeContextMenu}
        cardStyle={styles.menuCard}
        align="bottom"
      >
        <Text style={styles.menuTitle} numberOfLines={1}>
          {menuAgent ? presentAgent(menuAgent, agentAliases[menuAgent.key]).title : ''}
        </Text>

        <AnimatedPressable
          style={styles.menuItem}
          preset="press"
          scale={0.98}
          onPress={openRename}
        >
          <Ionicons name="pencil-outline" size={16} color={colors.textPrimary} />
          <Text style={styles.menuItemText}>Rename</Text>
        </AnimatedPressable>

        <AnimatedPressable
          style={styles.menuItem}
          preset="press"
          scale={0.98}
          onPress={() => { if (menuAgent) openAgent(menuAgent); closeContextMenu(); }}
        >
          <Ionicons name="terminal-outline" size={16} color={colors.textPrimary} />
          <Text style={styles.menuItemText}>Open Terminal</Text>
        </AnimatedPressable>

        <AnimatedPressable
          style={styles.menuItem}
          preset="press"
          scale={0.98}
          onPress={handleTerminateAgent}
        >
          <Ionicons name="power-outline" size={16} color={colors.dangerText} />
          <Text style={[styles.menuItemText, styles.menuItemTextDestructive]}>Terminate</Text>
        </AnimatedPressable>
      </RisingSheet>

      <RisingSheet
        visible={renameVisible}
        onClose={closeRename}
        cardStyle={styles.renameCard}
        avoidKeyboard
      >
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
          <AnimatedPressable style={styles.renameBtn} preset="press" scale={0.94} onPress={closeRename}>
            <Text style={styles.renameBtnText}>Cancel</Text>
          </AnimatedPressable>
          <AnimatedPressable
            style={[styles.renameBtn, styles.renameBtnPrimary]}
            preset="press"
            scale={0.94}
            onPress={handleRename}
          >
            <Text style={[styles.renameBtnText, styles.renameBtnPrimaryText]}>Save</Text>
          </AnimatedPressable>
        </View>
      </RisingSheet>

    </SafeAreaView>
  );
}

function BrainSessionBadge({
  colors,
  styles,
  compact = false,
}: {
  colors: typeof Colors;
  styles: ReturnType<typeof createStyles>;
  compact?: boolean;
}) {
  return (
    <View
      style={[
        styles.brainBadge,
        compact && styles.brainBadgeCompact,
        { borderColor: colors.borderStrong, backgroundColor: colors.surfaceSubtle },
      ]}
    >
      <Text style={[styles.brainBadgeText, { color: colors.textSecondary }]}>
        Brain
      </Text>
    </View>
  );
}

function AgentRunningIndicator({
  running,
  compact = false,
  colors,
  styles,
}: {
  running: boolean;
  compact?: boolean;
  colors: typeof Colors;
  styles: ReturnType<typeof createStyles>;
}) {
  if (running) {
    return (
      <View style={compact ? styles.sessionStatusBadge : styles.statusIndicator}>
        <ActivityIndicator
          size="small"
          color={colors.statusRunning}
          style={compact ? styles.compactSpinner : styles.statusSpinner}
        />
      </View>
    );
  }

  return (
    <View
      style={[
        compact ? styles.sessionStatusBadge : styles.statusDot,
        { backgroundColor: colors.disabledText },
      ]}
    />
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

  return presented.shortTitle || shortAgentLabel(agent.name) || presented.title;
}

function createStyles(theme: ResolvedZenTheme) {
  const colors = theme.colors;
  const {
    surface: themedSurface,
    border: themedBorder,
    sectionLabel,
  } = createThemedSurfaces(theme);

  return StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.bgPrimary,
  },
  flex: {
    flex: 1,
  },

  banner: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 7,
    paddingVertical: 8,
    marginHorizontal: 18,
    marginTop: 6,
    borderRadius: Radii.pill,
    backgroundColor: themedSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: themedBorder,
  },
  bannerDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  bannerText: {
    color: colors.textSecondary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 12.5,
  },

  listFab: {
    position: 'absolute',
    right: 20,
    bottom: 88,
    width: 56,
    height: 56,
    borderRadius: 28,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.accent,
    ...shadow('float', colors.shadowColor),
    zIndex: 4,
  },
  listFabDisabled: {
    backgroundColor: colors.surfaceSubtle,
    opacity: 0.72,
  },
  promptContent: {
    paddingHorizontal: 0,
    paddingTop: 6,
    paddingBottom: 120,
  },
  sectionHeader: {
    paddingTop: 18,
    paddingBottom: 12,
    paddingHorizontal: 4,
  },
  sectionTitle: {
    color: sectionLabel,
    fontSize: 11,
    lineHeight: 14,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 1,
    textTransform: 'uppercase',
  },
  sectionGap: {
    height: 8,
  },
  rowGap: {
    height: 0,
  },
  sessionStatusBadge: {
    width: 11,
    height: 11,
    borderRadius: 6,
    alignItems: 'center',
    justifyContent: 'center',
  },
  brainBadge: {
    height: 19,
    paddingHorizontal: 7,
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: 6,
    alignItems: 'center',
    justifyContent: 'center',
  },
  brainBadgeCompact: {
    height: 18,
    paddingHorizontal: 6,
  },
  brainBadgeText: {
    fontSize: 10,
    lineHeight: 12,
    fontFamily: Typography.uiFontMedium,
    includeFontPadding: false,
  },
  statusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  statusIndicator: {
    width: 14,
    height: 14,
    alignItems: 'center',
    justifyContent: 'center',
  },
  compactSpinner: {
    transform: [{ scale: 0.42 }],
  },
  statusSpinner: {
    transform: [{ scale: 0.55 }],
  },

  loadingContainer: {
    flexGrow: 1,
    minHeight: 420,
    alignItems: 'center',
    justifyContent: 'center',
  },
  gridContent: {
    paddingHorizontal: 18,
    paddingBottom: 40,
  },
  gridGap: {
    height: 14,
  },
  gridCard: {
    borderRadius: Radii.lg,
    backgroundColor: themedSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: themedBorder,
    overflow: 'hidden',
    ...glassCardShadow(colors.shadowColor),
  },
  gridHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 11,
    paddingHorizontal: 15,
    paddingVertical: 13,
    minHeight: 52,
  },
  gridHeaderMain: {
    flex: 1,
    minWidth: 0,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 10,
  },
  gridTitle: {
    flex: 1,
    minWidth: 0,
    color: colors.textPrimary,
    fontSize: 14.5,
    lineHeight: 22,
    fontFamily: Typography.uiFontMedium,
  },
  gridPreview: {
    height: 216,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.borderSubtle,
  },

  emptyContainer: {
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: 36,
  },
  emptyScrollContent: {
    flexGrow: 1,
    justifyContent: 'center',
    paddingVertical: 44,
  },
  emptyBadge: {
    width: 88,
    height: 88,
    borderRadius: 44,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.accentSoft,
    marginBottom: 22,
  },
  emptyIcon: {
    fontSize: 42,
    color: colors.accent,
    lineHeight: 48,
  },
  emptyText: {
    color: colors.textPrimary,
    fontSize: 18,
    fontFamily: Typography.uiFontMedium,
  },
  emptySubtext: {
    color: colors.textSecondary,
    fontSize: 14,
    fontFamily: Typography.uiFont,
    marginTop: 9,
    maxWidth: 280,
    textAlign: 'center',
    lineHeight: 20,
    opacity: 0.85,
  },
  emptyActions: {
    width: '100%',
    maxWidth: 260,
    gap: 14,
    marginTop: 30,
    alignItems: 'center',
  },
  emptyActionLink: {
    paddingVertical: 6,
    paddingHorizontal: 10,
  },
  emptyActionLinkText: {
    color: colors.accent,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  emptyActionBtn: {
    width: '100%',
    flexDirection: 'row',
    minHeight: 50,
    paddingHorizontal: 18,
    borderRadius: Radii.sm,
    alignItems: 'center',
    justifyContent: 'center',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: themedBorder,
    backgroundColor: themedSurface,
    gap: 8,
  },
  emptyActionBtnPrimary: {
    backgroundColor: colors.accent,
    borderColor: colors.accent,
    ...shadow('card', colors.shadowColor),
  },
  emptyActionIcon: {
    marginTop: 1,
  },
  emptyActionText: {
    color: colors.textPrimary,
    fontSize: 15,
    fontFamily: Typography.uiFontMedium,
    textAlign: 'center',
  },
  emptyActionTextPrimary: {
    color: colors.textOnAccent,
  },

  menuCard: {
    borderRadius: Radii.lg,
    backgroundColor: colors.modalSurfaceAlt,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
    overflow: 'hidden',
    ...shadow('float', colors.shadowColor),
  },
  menuTitle: {
    color: colors.textSecondary,
    fontSize: 12.5,
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
    paddingVertical: 15,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.borderSubtle,
  },
  menuItemText: {
    color: colors.textPrimary,
    fontSize: 15.5,
    fontFamily: Typography.uiFont,
  },
  menuItemTextDestructive: {
    color: colors.dangerText,
  },

  renameCard: {
    borderRadius: Radii.lg,
    padding: 20,
    backgroundColor: colors.modalSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: colors.border,
  },
  renameTitle: {
    color: colors.textPrimary,
    fontSize: 17,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 16,
  },
  renameInput: {
    backgroundColor: colors.inputBackground,
    borderRadius: Radii.sm,
    paddingHorizontal: 14,
    paddingVertical: 12,
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
    minWidth: 76,
    height: 40,
    borderRadius: Radii.sm,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.surfacePressed,
  },
  renameBtnPrimary: {
    backgroundColor: colors.accent,
  },
  renameBtnText: {
    color: colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  renameBtnPrimaryText: {
    color: colors.textOnAccent,
  },
  });
}
