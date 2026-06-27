import React, { useEffect, useMemo, useRef, useState } from 'react';
import {
  ActivityIndicator,
  Alert,
  FlatList,
  LayoutAnimation,
  Platform,
  SectionList,
  Linking,
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
import { AgentStatus, Colors, Radii, Typography, useAppColors, shadow } from '../../constants/tokens';
import { IconButton } from '../../components/ui/IconButton';
import { AnimatedPressable } from '../../components/ui/AnimatedPressable';
import { SkyNatureBackdrop } from '../../components/ui/SkyNatureBackdrop';
import { RisingSheet } from '../../components/ui/RisingSheet';
import { Enter } from '../../components/ui/Enter';
import { TerminalPreview } from '../../components/terminal/TerminalPreview';
import { AgentKindIcon } from '../../components/terminal/AgentKindIcon';
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
    () => groupAgentsByDirectory(sortedAgents, { showServerName: showServerNames }),
    [showServerNames, sortedAgents],
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
    return (
      <AnimatedPressable
        style={styles.sessionRow}
        preset="card"
        onPress={() => openAgent(item)}
        onLongPress={() => {
          Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Medium);
          openContextMenu(item);
        }}
        delayLongPress={400}
      >
        <AgentKindIcon kind={presented.kind} size={15} />
        <View style={styles.sessionBody}>
          <View style={styles.sessionTitleRow}>
            <Text style={styles.sessionName} numberOfLines={1}>
              {sessionTitle}
            </Text>
            {item.delegated ? <BrainSessionBadge colors={colors} styles={styles} /> : null}
          </View>
        </View>
        <AgentRunningIndicator
          running={item.status === 'running'}
          colors={colors}
          styles={styles}
          compact
        />
      </AnimatedPressable>
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
            <AgentKindIcon kind={presented.kind} size={15} />
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
    <SafeAreaView style={styles.container} edges={['top']}>
      <SkyNatureBackdrop height={640} />

      {hasConnection && !anyConnected && (
        <View style={styles.banner}>
          <View style={[styles.bannerDot, { backgroundColor: bannerAccent }]} />
          <Text style={styles.bannerText}>{bannerText}</Text>
        </View>
      )}

      <View style={styles.header}>
        <View style={styles.headerTop}>
          <View style={styles.headerBrand}>
            <Text style={styles.title}>Zen</Text>
          </View>
          <View style={styles.headerActions}>
            <IconButton
              icon="globe-outline"
              tone="ghost"
              size={40}
              iconSize={20}
              color={anyConnected ? colors.textSecondary : colors.disabledText}
              accessibilityLabel="Services"
              onPress={openSessionServices}
              disabled={!anyConnected}
            />
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
            <AnimatedPressable
              style={[styles.fab, (!anyConnected || !!creatingServerId) && styles.fabDisabled]}
              preset="press"
              scale={0.9}
              onPress={openCreateTerminal}
              disabled={!!creatingServerId || !anyConnected}
              accessibilityLabel="New terminal"
            >
              <Ionicons name={creatingServerId ? 'hourglass-outline' : 'add'} size={22} color={colors.textOnAccent} />
            </AnimatedPressable>
          </View>
        </View>
      </View>

      {shouldShowInitialLoading ? (
        <View style={styles.loadingContainer}>
          <ActivityIndicator color={colors.accent} />
        </View>
      ) : sortedAgents.length === 0 ? (
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
      ) : viewMode === 'list' ? (
        <SectionList
          sections={listSections}
          key="list"
          keyExtractor={item => item.key}
          renderItem={renderListAgent}
          renderSectionHeader={renderListSectionHeader}
          stickySectionHeadersEnabled={false}
          contentContainerStyle={styles.promptContent}
          removeClippedSubviews={false}
          windowSize={15}
          showsVerticalScrollIndicator={false}
          ItemSeparatorComponent={() => <View style={styles.rowGap} />}
          SectionSeparatorComponent={() => <View style={styles.sectionGap} />}
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
        size={18}
        color={selected ? colors.accent : colors.disabledText}
        style={!selected && styles.viewIconInactive}
      />
    </TouchableOpacity>
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

function createStyles(colors: typeof Colors) {
  const light = colors.bgPrimary === '#F6F8FB';
  const themedSurface = light ? 'rgba(255,255,255,0.76)' : 'rgba(16,22,34,0.72)';
  const themedSurfaceStrong = light ? 'rgba(255,255,255,0.86)' : 'rgba(20,28,42,0.82)';
  const themedBorder = light ? 'rgba(46,124,255,0.16)' : 'rgba(107,160,255,0.18)';
  const themedSubtle = light ? 'rgba(255,255,255,0.44)' : 'rgba(76,141,255,0.10)';

  return StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: colors.bgPrimary,
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

  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: 20,
    paddingTop: 16,
    paddingBottom: 14,
  },
  headerTop: {
    flex: 1,
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    minWidth: 0,
  },
  headerBrand: {
    flex: 1,
    minWidth: 0,
  },
  title: {
    color: colors.textPrimary,
    fontSize: 32,
    lineHeight: 36,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: -0.8,
  },
  headerActions: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginLeft: 12,
  },
  viewToggle: {
    flexDirection: 'row',
    marginLeft: 2,
    borderRadius: Radii.sm,
    backgroundColor: themedSubtle,
    padding: 3,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: themedBorder,
  },
  viewBtn: {
    width: 34,
    height: 34,
    borderRadius: 9,
    alignItems: 'center',
    justifyContent: 'center',
  },
  viewBtnActive: {
    backgroundColor: themedSurfaceStrong,
  },
  viewIconInactive: {
    opacity: 0.72,
  },
  fab: {
    width: 42,
    height: 42,
    borderRadius: 21,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: colors.accent,
    marginLeft: 2,
    ...shadow('card', colors.shadowColor),
  },
  fabDisabled: {
    backgroundColor: colors.surfaceSubtle,
  },

  promptContent: {
    paddingHorizontal: 18,
    paddingTop: 6,
    paddingBottom: 40,
  },
  sectionHeader: {
    paddingTop: 18,
    paddingBottom: 12,
    paddingHorizontal: 4,
  },
  sectionTitle: {
    color: colors.textTertiary,
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
    height: 10,
  },
  sessionRow: {
    flexDirection: 'row',
    alignItems: 'center',
    minHeight: 60,
    gap: 13,
    paddingVertical: 13,
    paddingHorizontal: 16,
    borderRadius: Radii.md,
    backgroundColor: themedSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: themedBorder,
    ...shadow('card', colors.shadowColor),
  },
  sessionStatusBadge: {
    width: 11,
    height: 11,
    borderRadius: 6,
    alignItems: 'center',
    justifyContent: 'center',
  },
  sessionBody: {
    flex: 1,
    minWidth: 0,
    justifyContent: 'center',
  },
  sessionTitleRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 7,
    minWidth: 0,
  },
  sessionName: {
    flexShrink: 1,
    minWidth: 0,
    color: colors.textPrimary,
    fontSize: 15.5,
    lineHeight: 20,
    fontFamily: Typography.uiFontMedium,
    includeFontPadding: false,
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
    flex: 1,
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
    ...shadow('card', colors.shadowColor),
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
    lineHeight: 19,
    fontFamily: Typography.uiFontMedium,
  },
  gridPreview: {
    height: 216,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: colors.borderSubtle,
  },

  emptyContainer: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
    paddingHorizontal: 36,
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
    borderColor: colors.borderStrong,
    backgroundColor: colors.surfaceSubtle,
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
