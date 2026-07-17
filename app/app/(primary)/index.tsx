import React, { useCallback, useMemo, useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import {
  useFocusEffect,
  useIsFocused,
  useLocalSearchParams,
  useRouter,
} from "expo-router";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import { BrainAdapterSheet } from "../../components/brain/BrainAdapterSheet";
import { BrainExecutorMentionPicker } from "../../components/brain/BrainExecutorMentionPicker";
import { BrainOverflowMenu } from "../../components/brain/BrainOverflowMenu";
import { BrainWorkspaceViewer } from "../../components/brain/BrainWorkspaceViewer";
import { brainProviderLabel } from "../../components/brain/brainPresentation";
import { usePrimaryPageAction } from "../../components/navigation/PrimaryPageAction";
import { resolvePrimaryAppBarGeometry } from "../../components/navigation/PrimaryDrawerShell";
import { ZenLoopSpinner } from "../../components/ui/ZenLoopSpinner";
import { ChatCanvas } from "../../components/terminal/ChatCanvas";
import { CHAT_CHROME_HORIZONTAL_INSET } from "../../components/terminal/chatChromeMetrics";
import { CodexChatSurface } from "../../components/terminal/CodexChatSurface";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { buildChatChrome } from "../../theme";
import {
  Colors,
  TypeScale,
  useAppColors,
  useAppTheme,
} from "../../constants/tokens";
import { getServers, type StoredServer } from "../../services/storage";
import { wsClient } from "../../services/websocket";
import {
  resolveBrainActiveServerId,
  shouldShowBrainLoadingState,
} from "../../services/connectionLifecycle";
import { isTargetedBrainThreadReadOnly } from "../../services/brainThreadRouting";
import { useAgents, type ConnectionState } from "../../store/agents";
import {
  useBrain,
  type BrainAdapterRef,
} from "../../store/brain";

const BRAIN_EMPTY_TITLE = "Ready when you are";
const BRAIN_EMPTY_BODY = "Ask Brain to plan, delegate, or inspect the workspace.";

export default function BrainScreen() {
  const router = useRouter();
  const params = useLocalSearchParams<{
    brainThreadId?: string;
    brainMessageId?: string;
    serverId?: string;
  }>();
  const colors = useAppColors();
  const { theme: zenTheme } = useAppTheme();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const insets = useSafeAreaInsets();
  const topChromeInset = resolvePrimaryAppBarGeometry(insets.top).contentInset;
  const { chrome, theme } = useMemo(
    () => buildChatChrome(zenTheme),
    [zenTheme],
  );
  const { state: agentState } = useAgents();
  const { state: brainState } = useBrain();
  const screenFocused = useIsFocused();
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [adapterSheetVisible, setAdapterSheetVisible] = useState(false);
  const [menuVisible, setMenuVisible] = useState(false);
  const [switchingAdapterId, setSwitchingAdapterId] = useState<string | null>(
    null,
  );
  const [adapterSwitchError, setAdapterSwitchError] = useState<string | null>(
    null,
  );
  const [newChatLoading, setNewChatLoading] = useState(false);
  const [brainActionError, setBrainActionError] = useState<string | null>(null);
  const [workspaceViewerVisible, setWorkspaceViewerVisible] = useState(false);

  useFocusEffect(
    React.useCallback(() => {
      let cancelled = false;
      void getServers().then((savedServers) => {
        if (!cancelled) {
          setServers(savedServers);
        }
      });
      return () => {
        cancelled = true;
      };
    }, []),
  );

  const connectedServers = useMemo(
    () =>
      servers.filter(
        (server) => agentState.serverConnections[server.id] === "connected",
      ),
    [agentState.serverConnections, servers],
  );

  const activeServer = useMemo(() => {
    const activeId =
      params.serverId &&
      servers.some((server) => server.id === params.serverId)
        ? params.serverId
        : resolveBrainActiveServerId({
            servers,
            connectedServerIds: connectedServers.map((server) => server.id),
            brainHydratedByServer: Object.fromEntries(
              Object.entries(brainState.byServer).map(([serverId, brain]) => [
                serverId,
                Boolean(brain?.hydrated),
              ]),
            ),
            connectionStates: agentState.serverConnections,
          });
    if (!activeId) {
      return null;
    }
    return (
      servers.find((server) => server.id === activeId) ||
      connectedServers.find((server) => server.id === activeId) ||
      null
    );
  }, [
    agentState.serverConnections,
    brainState.byServer,
    connectedServers,
    servers,
    params.serverId,
  ]);

  const activeBrain = activeServer
    ? brainState.byServer[activeServer.id]
    : null;
  const connectionState: ConnectionState = activeServer
    ? agentState.serverConnections[activeServer.id] || "offline"
    : "offline";
  const connectionIssue = activeServer
    ? (agentState.serverConnectionIssues[activeServer.id] ?? null)
    : null;
  const hostAgent = activeBrain?.host_agent ?? null;
  const hostAdapter = activeBrain?.host_adapter ?? null;
  const displayedThreadId = params.brainThreadId || activeBrain?.chat_thread_id;
  const targetedThreadReadOnly = isTargetedBrainThreadReadOnly(
    params.brainThreadId,
    activeBrain?.chat_thread_id,
  );
  const brainChatScopeKey = displayedThreadId
    ? `brain-thread:${displayedThreadId}`
    : undefined;

  const ready = Boolean(activeServer && activeBrain?.hydrated && hostAgent?.id);
  const showBrainLoading = shouldShowBrainLoadingState({
    hydrated: Boolean(activeBrain?.hydrated),
    hasHostAgent: Boolean(hostAgent?.id),
  });
  const canUseStructuredBrainInterface = Boolean(
    ready && hostAdapter?.capabilities?.structured_events,
  );
  const availableAdapters = activeBrain?.adapters ?? [];
  const canSwitchAdapter = availableAdapters.length > 1;
  const openAdapterSheet = useCallback(() => {
    if (!canSwitchAdapter || !activeServer) {
      return;
    }
    setAdapterSwitchError(null);
    setAdapterSheetVisible(true);
  }, [activeServer, canSwitchAdapter]);

  const closeAdapterSheet = useCallback(() => {
    setAdapterSheetVisible(false);
    setAdapterSwitchError(null);
  }, []);

  const openMenu = useCallback(() => {
    setMenuVisible(true);
  }, []);

  const closeMenu = useCallback(() => {
    setMenuVisible(false);
  }, []);

  const openWorkspaceViewer = useCallback(() => {
    if (!activeServer) {
      return;
    }
    setWorkspaceViewerVisible(true);
  }, [activeServer]);

  const closeWorkspaceViewer = useCallback(() => {
    setWorkspaceViewerVisible(false);
  }, []);

  const openBrainTerminal = useCallback(() => {
    if (!activeServer || !hostAgent?.id) {
      return;
    }
    router.push({
      pathname: "/terminal/[id]",
      params: {
        id: hostAgent.id,
        serverId: activeServer.id,
        initialCodexRenderMode: "terminal",
      },
    });
  }, [activeServer, hostAgent?.id, router]);

  const openCalendar = useCallback(() => {
    router.push("/calendar");
  }, [router]);

  const startNewBrainChat = useCallback(async () => {
    if (!activeServer || !activeBrain?.hydrated || newChatLoading) {
      return;
    }
    setBrainActionError(null);
    setNewChatLoading(true);
    try {
      await wsClient.startNewBrainChat(activeServer.id);
    } catch (error: any) {
      setBrainActionError(
        error?.message || "Failed to start a new Brain chat.",
      );
    } finally {
      setNewChatLoading(false);
    }
  }, [activeBrain?.hydrated, activeServer, newChatLoading]);

  const switchBrainAdapter = useCallback(
    async (adapter: BrainAdapterRef) => {
      if (!activeServer || !adapter.id || switchingAdapterId) {
        return;
      }
      if (adapter.id === hostAdapter?.id) {
        closeAdapterSheet();
        return;
      }
      setSwitchingAdapterId(adapter.id);
      setAdapterSwitchError(null);
      try {
        await wsClient.setBrainExecutor(activeServer.id, adapter.id);
        closeAdapterSheet();
      } catch (error: any) {
        setAdapterSwitchError(error?.message || "Failed to switch executor.");
      } finally {
        setSwitchingAdapterId(null);
      }
    },
    [activeServer, closeAdapterSheet, hostAdapter?.id, switchingAdapterId],
  );

  const canNewChat = Boolean(activeServer && activeBrain?.hydrated);
  const canOpenTerminal = Boolean(activeServer && hostAgent?.id);
  const canOpenWorkspace = Boolean(
    activeServer && connectionState === "connected",
  );
  const overflowDisabled =
    !canNewChat && !canOpenTerminal && !canOpenWorkspace && !canSwitchAdapter;

  const brainPageAction = useMemo(
    () => ({
      accessibilityLabel: "Brain actions",
      disabled: overflowDisabled,
      onPress: openMenu,
    }),
    [openMenu, overflowDisabled],
  );
  usePrimaryPageAction(brainPageAction);

  const menuActions = useMemo(
    () => [
      {
        key: "new-chat",
        label: "New chat",
        detail: "Start a fresh Brain thread",
        icon: newChatLoading
          ? ("hourglass-outline" as const)
          : ("create-outline" as const),
        disabled: !canNewChat || newChatLoading,
        onPress: () => {
          void startNewBrainChat();
        },
      },
      ...(canSwitchAdapter
        ? [
            {
              key: "executor",
              label: "Switch executor",
              detail: brainProviderLabel(hostAdapter?.provider),
              icon: "swap-horizontal-outline" as const,
              onPress: openAdapterSheet,
            },
          ]
        : []),
      {
        key: "terminal",
        label: "Open terminal",
        detail: "Raw session view",
        icon: "terminal-outline" as const,
        disabled: !canOpenTerminal,
        onPress: openBrainTerminal,
      },
      {
        key: "workspace",
        label: "Browse workspace",
        icon: "folder-open-outline" as const,
        disabled: !canOpenWorkspace,
        onPress: openWorkspaceViewer,
      },
      {
        key: "calendar",
        label: "Calendar",
        detail: "Commitments and scheduled Zen actions",
        icon: "calendar-outline" as const,
        onPress: openCalendar,
      },
    ],
    [
      canNewChat,
      canOpenTerminal,
      canOpenWorkspace,
      canSwitchAdapter,
      hostAdapter?.provider,
      newChatLoading,
      openAdapterSheet,
      openCalendar,
      openBrainTerminal,
      openWorkspaceViewer,
      startNewBrainChat,
    ],
  );

  const renderBrainComposerAccessory = useCallback(
    ({
      draft,
      setDraft,
    }: {
      draft: string;
      setDraft: (value: string) => void;
    }) => {
      const activeMention = activeExecutorMentionAtEnd(draft);
      if (!activeMention || availableAdapters.length === 0) {
        return null;
      }
      return (
        <BrainExecutorMentionPicker
          adapters={availableAdapters}
          activeAdapterId={hostAdapter?.id}
          query={activeMention.query}
          chrome={chrome}
          onSelect={(adapter) => {
            const before = draft.slice(0, activeMention.start);
            const next = `${before}@${adapter.id} `;
            setDraft(next);
          }}
        />
      );
    },
    [availableAdapters, chrome, hostAdapter?.id],
  );

  return (
    <SafeAreaView
      style={[styles.screen, { backgroundColor: chrome.appBackground }]}
      edges={[]}
    >
      {brainActionError || targetedThreadReadOnly ? (
        <View style={{ paddingTop: topChromeInset }}>
          {brainActionError ? (
            <View style={styles.bannerError}>
              <Text style={styles.bannerErrorText}>{brainActionError}</Text>
            </View>
          ) : null}
          {targetedThreadReadOnly ? (
            <View style={styles.bannerReadOnly}>
              <Ionicons
                name="lock-closed-outline"
                size={14}
                color={colors.textSecondary}
              />
              <Text style={styles.bannerReadOnlyText}>
                Historical Brain thread · read-only
              </Text>
            </View>
          ) : null}
        </View>
      ) : null}

      <View style={styles.surface}>
        <ChatCanvas chrome={chrome}>
          {canUseStructuredBrainInterface ? (
            <CodexChatSurface
              key={`brain-chat:${activeServer?.id}:${brainChatScopeKey ?? ""}`}
              visible
              serverId={activeServer?.id ?? ""}
              agentId={hostAgent?.id ?? ""}
              conversationScopeKey={brainChatScopeKey}
              agentInfo={{
                cwd: hostAgent?.cwd,
                command: hostAgent?.command,
                name: hostAgent?.name,
              }}
              connectionState={connectionState}
              connectionIssue={connectionIssue}
              theme={theme}
              chrome={chrome}
              screenFocused={screenFocused}
              topChromeInset={topChromeInset}
              readOnly={targetedThreadReadOnly}
              onSwitchToTerminal={openBrainTerminal}
              emptyTitle={BRAIN_EMPTY_TITLE}
              emptyBody={BRAIN_EMPTY_BODY}
              renderComposerAccessory={renderBrainComposerAccessory}
            />
          ) : showBrainLoading ? (
            <BrainLoadingState
              chrome={chrome}
              connected={
                connectionState === "connected" ||
                connectionState === "connecting"
              }
            />
          ) : (
            <BrainInterfaceUnavailableState
              chrome={chrome}
              provider={hostAdapter?.provider}
            />
          )}
        </ChatCanvas>
      </View>

      <BrainAdapterSheet
        visible={adapterSheetVisible}
        adapters={availableAdapters}
        activeAdapterId={hostAdapter?.id}
        switchingAdapterId={switchingAdapterId}
        error={adapterSwitchError}
        onClose={closeAdapterSheet}
        onSelect={(adapter) => void switchBrainAdapter(adapter)}
      />

      <BrainOverflowMenu
        visible={menuVisible}
        actions={menuActions}
        onClose={closeMenu}
      />

      <BrainWorkspaceViewer
        visible={workspaceViewerVisible}
        serverId={activeServer?.id}
        workspace={activeBrain?.workspace}
        chrome={chrome}
        theme={theme}
        onClose={closeWorkspaceViewer}
      />
    </SafeAreaView>
  );
}

function BrainStateCard({
  chrome,
  glyph,
  title,
  detail,
}: {
  chrome: TerminalThemeChrome;
  glyph: React.ReactNode;
  title: string;
  detail?: string;
}) {
  const styles = useMemo(() => createStateCardStyles(chrome), [chrome]);

  return (
    <View style={styles.card}>
      <View style={styles.glyphWrap}>{glyph}</View>
      <Text style={styles.title}>{title}</Text>
      {detail ? <Text style={styles.detail}>{detail}</Text> : null}
    </View>
  );
}

function BrainLoadingState({
  chrome,
  connected,
}: {
  chrome: TerminalThemeChrome;
  connected: boolean;
}) {
  return (
    <BrainStateCard
      chrome={chrome}
      glyph={
        connected ? (
          <ZenLoopSpinner size={36} />
        ) : (
          <Ionicons
            name="cloud-offline-outline"
            size={22}
            color={chrome.textMuted}
          />
        )
      }
      title={connected ? "Connecting to Brain" : "Brain is offline"}
      detail={
        connected
          ? "Fetching the latest workspace and chat thread."
          : "Connect a server in Settings to use Brain."
      }
    />
  );
}

function BrainInterfaceUnavailableState({
  chrome,
  provider,
}: {
  chrome: TerminalThemeChrome;
  provider?: string;
}) {
  const label = brainProviderLabel(provider);
  return (
    <BrainStateCard
      chrome={chrome}
      glyph={
        <Ionicons name="layers-outline" size={22} color={chrome.textMuted} />
      }
      title="Chat UI not available"
      detail={
        label
          ? `${label} is connected, but this executor does not expose structured chat events.`
          : "Switch the Brain host executor to one with structured chat events."
      }
    />
  );
}

function createStateCardStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    card: {
      flex: 1,
      alignItems: "center",
      justifyContent: "center",
      paddingHorizontal: 36,
      gap: 10,
    },
    glyphWrap: {
      width: 72,
      height: 72,
      borderRadius: 36,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: chrome.accentSoft,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: chrome.border,
      marginBottom: 4,
    },
    title: {
      ...TypeScale.heading,
      color: chrome.text,
      textAlign: "center",
    },
    detail: {
      ...TypeScale.compact,
      color: chrome.textMuted,
      textAlign: "center",
      maxWidth: 280,
    },
  });
}

function activeExecutorMentionAtEnd(
  value: string,
): { start: number; query: string } | null {
  const end = value.length;
  let cursor = end - 1;
  while (cursor >= 0) {
    const char = value[cursor];
    if (char === "@") {
      if (cursor === 0 || /\s/.test(value[cursor - 1])) {
        return {
          start: cursor,
          query: value.slice(cursor + 1, end),
        };
      }
      return null;
    }
    if (!/[a-z0-9_.-]/i.test(char)) {
      return null;
    }
    cursor -= 1;
  }
  return null;
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    screen: {
      flex: 1,
    },
    surface: {
      flex: 1,
    },
    bannerError: {
      marginHorizontal: CHAT_CHROME_HORIZONTAL_INSET,
      marginBottom: 4,
      paddingHorizontal: 14,
      paddingVertical: 8,
      borderRadius: 14,
      backgroundColor: colors.dangerSoft,
      zIndex: 2,
    },
    bannerErrorText: {
      ...TypeScale.caption,
      color: colors.dangerText,
    },
    bannerReadOnly: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
      marginHorizontal: CHAT_CHROME_HORIZONTAL_INSET,
      marginBottom: 4,
      paddingHorizontal: 12,
      paddingVertical: 7,
      borderRadius: 12,
      backgroundColor: colors.bgElevated,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.border,
    },
    bannerReadOnlyText: {
      ...TypeScale.caption,
      color: colors.textSecondary,
    },
  });
}
