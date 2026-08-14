import React, { useCallback, useEffect, useMemo, useState } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import {
  useIsFocused,
  useLocalSearchParams,
  useRouter,
} from "expo-router";
import {
  SafeAreaView,
  useSafeAreaInsets,
} from "react-native-safe-area-context";
import {
  BrainAdapterSheet,
  type ExecutorTarget,
} from "../../components/brain/BrainAdapterSheet";
import { BrainAdapterIcon } from "../../components/brain/BrainAdapterIcon";
import { BrainExecutorMentionPicker } from "../../components/brain/BrainExecutorMentionPicker";
import { BrainOverflowMenu } from "../../components/brain/BrainOverflowMenu";
import { BrainWorkspaceViewer } from "../../components/brain/BrainWorkspaceViewer";
import { SessionModelSheet } from "../../components/providers/SessionModelSheet";
import { useSessionProviderSheet } from "../../components/terminal/screen/useSessionProviderSheet";
import {
  brainProviderLabel,
  distinctExecutorAdapters,
  switchExecutorAccessibilityLabel,
} from "../../components/brain/brainPresentation";
import { usePrimaryPageAction } from "../../components/navigation/PrimaryPageAction";
import { resolvePrimaryAppBarGeometry } from "../../components/navigation/PrimaryDrawerShell";
import { ZenLoopSpinner } from "../../components/ui/ZenLoopSpinner";
import { ChatCanvas } from "../../components/terminal/ChatCanvas";
import { CHAT_CHROME_HORIZONTAL_INSET } from "../../components/terminal/chatChromeMetrics";
import { InterfaceChatSurface } from "../../components/terminal/InterfaceChatSurface";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { buildChatChrome } from "../../theme";
import {
  Colors,
  TypeScale,
  useAppColors,
  useAppTheme,
} from "../../constants/tokens";
import { wsClient } from "../../services/websocket";
import { shouldShowBrainLoadingState } from "../../services/connectionLifecycle";
import { isTargetedBrainThreadReadOnly } from "../../services/brainThreadRouting";
import { useAgents, type ConnectionState } from "../../store/agents";
import {
  useBrain,
  type BrainAdapterRef,
} from "../../store/brain";
import type { BrainWorkResultEvent } from "../../components/brain/brainWorkEvent";
import { useCurrentServer } from "../../store/currentServer";

const BRAIN_EMPTY_TITLE = "Ready when you are";
const BRAIN_EMPTY_BODY =
  "Ask Brain to plan, delegate, or inspect the workspace.";

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
  const {
    currentServer: activeServer,
    hydrated: currentServerHydrated,
    switchCurrentServer,
  } = useCurrentServer();
  const screenFocused = useIsFocused();
  const [adapterSheetVisible, setAdapterSheetVisible] = useState(false);
  const [menuVisible, setMenuVisible] = useState(false);
  const [switchingAdapterId, setSwitchingAdapterId] = useState<string | null>(
    null,
  );
  const [switchingTarget, setSwitchingTarget] = useState<ExecutorTarget | null>(
    null,
  );
  const [adapterSwitchError, setAdapterSwitchError] = useState<string | null>(
    null,
  );
  const [newChatLoading, setNewChatLoading] = useState(false);
  const [brainActionError, setBrainActionError] = useState<string | null>(null);
  const [workspaceViewerVisible, setWorkspaceViewerVisible] = useState(false);

  const routeServerId = params.serverId?.trim() || "";
  const routeServerMatches =
    !routeServerId || activeServer?.id === routeServerId;

  useEffect(() => {
    if (
      !currentServerHydrated ||
      !routeServerId ||
      activeServer?.id === routeServerId
    ) {
      return;
    }
    setBrainActionError(null);
    void switchCurrentServer(routeServerId).catch((error) => {
      setBrainActionError(
        error instanceof Error
          ? error.message
          : "Unable to switch to the requested server.",
      );
    });
  }, [
    activeServer?.id,
    currentServerHydrated,
    routeServerId,
    switchCurrentServer,
  ]);

  const activeBrain = activeServer && routeServerMatches
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
  const delegatedAdapter = activeBrain?.delegated_adapter ?? null;
  const routedThreadId = routeServerMatches ? params.brainThreadId : undefined;
  const displayedThreadId = routedThreadId || activeBrain?.chat_thread_id;
  const targetedThreadReadOnly = isTargetedBrainThreadReadOnly(
    routedThreadId,
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
  const brainModelSheet = useSessionProviderSheet({
    serverId: activeServer?.id ?? "",
    agentId: hostAgent?.id ?? "",
    capabilities: hostAgent?.capabilities ?? null,
    connectionConnected: connectionState === "connected",
    eagerLoad: true,
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
        initialInterfaceRenderMode: "terminal",
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

  const switchExecutor = useCallback(
    async (adapter: BrainAdapterRef, target: ExecutorTarget) => {
      if (!activeServer || !adapter.id || switchingAdapterId) {
        return;
      }
      const currentId =
        target === "brain" ? hostAdapter?.id : delegatedAdapter?.id;
      if (adapter.id === currentId) {
        closeAdapterSheet();
        return;
      }
      setSwitchingTarget(target);
      setSwitchingAdapterId(adapter.id);
      setAdapterSwitchError(null);
      try {
        if (target === "brain") {
          await wsClient.setBrainExecutor(activeServer.id, adapter.id);
        } else {
          await wsClient.setDelegatedExecutor(activeServer.id, adapter.id);
        }
        closeAdapterSheet();
      } catch (error: any) {
        setAdapterSwitchError(error?.message || "Failed to switch executor.");
      } finally {
        setSwitchingAdapterId(null);
        setSwitchingTarget(null);
      }
    },
    [
      activeServer,
      closeAdapterSheet,
      delegatedAdapter?.id,
      hostAdapter?.id,
      switchingAdapterId,
    ],
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
              accessibilityLabel: switchExecutorAccessibilityLabel(
                hostAdapter,
                delegatedAdapter,
              ),
              icon: "swap-horizontal-outline" as const,
              trailing: distinctExecutorAdapters(
                hostAdapter,
                delegatedAdapter,
              ).map((adapter) => (
                <BrainAdapterIcon
                  key={adapter.id}
                  adapter={adapter}
                  size={14}
                />
              )),
              onPress: openAdapterSheet,
            },
          ]
        : []),
      {
        key: "terminal",
        label: "Open terminal",
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
        icon: "calendar-outline" as const,
        onPress: openCalendar,
      },
    ],
    [
      canNewChat,
      canOpenTerminal,
      canOpenWorkspace,
      canSwitchAdapter,
      delegatedAdapter,
      hostAdapter,
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

  const activateWorkResult = useCallback(
    (event: BrainWorkResultEvent, canOpenSession: boolean) => {
      if (!activeServer) {
        return;
      }
      if (event.unread) {
        wsClient.markBrainWorkRead(activeServer.id, event.work_id);
      }
      if (event.session_id && canOpenSession) {
        router.push({
          pathname: "/terminal/[id]",
          params: {
            id: event.session_id,
            serverId: activeServer.id,
          },
        });
      }
    },
    [activeServer, router],
  );
  const openSessionIds = useMemo(
    () => new Set((activeBrain?.agents ?? []).map((agent) => agent.id)),
    [activeBrain?.agents],
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
            <InterfaceChatSurface
              key={`brain-chat:${activeServer?.id}:${brainChatScopeKey ?? ""}`}
              visible
              serverId={activeServer?.id ?? ""}
              serverUrl={activeServer?.url ?? ""}
              daemonId={activeServer?.daemonId ?? ""}
              agentId={hostAgent?.id ?? ""}
              conversationScopeKey={brainChatScopeKey}
              agentInfo={{
                cwd: hostAgent?.cwd,
                command: hostAgent?.command,
                name: hostAgent?.name,
                processId: hostAgent?.process_id,
                startedAt: hostAgent?.started_at,
              }}
              connectionState={connectionState}
              connectionIssue={connectionIssue}
              theme={theme}
              chrome={chrome}
              screenFocused={screenFocused}
              topChromeInset={topChromeInset}
              onBrainWorkEventActivate={
                targetedThreadReadOnly ? undefined : activateWorkResult
              }
              openSessionIds={openSessionIds}
              readOnly={targetedThreadReadOnly}
              onSwitchToTerminal={openBrainTerminal}
              emptyTitle={BRAIN_EMPTY_TITLE}
              emptyBody={BRAIN_EMPTY_BODY}
              renderComposerAccessory={renderBrainComposerAccessory}
              composerModelControl={brainModelSheet.composerControl}
              onComposerModelControlPress={() => brainModelSheet.open()}
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
        hostAdapterId={hostAdapter?.id}
        delegatedAdapterId={delegatedAdapter?.id}
        hostAdapter={hostAdapter}
        delegatedAdapter={delegatedAdapter}
        switchingAdapterId={switchingAdapterId}
        switchingTarget={switchingTarget}
        error={adapterSwitchError}
        onClose={closeAdapterSheet}
        onSelect={(adapter, target) => void switchExecutor(adapter, target)}
      />

      <BrainOverflowMenu
        visible={menuVisible}
        actions={menuActions}
        onClose={closeMenu}
      />

      <SessionModelSheet
        visible={brainModelSheet.visible}
        loading={brainModelSheet.loading}
        activating={brainModelSheet.activating}
        error={brainModelSheet.error}
        selection={brainModelSheet.selection}
        groups={brainModelSheet.groups}
        chrome={chrome}
        onClose={brainModelSheet.close}
        onRetry={brainModelSheet.retry}
        onActivate={(choice) => {
          void brainModelSheet.activate(choice);
        }}
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
