import React, { useCallback, useMemo, useState } from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useFocusEffect, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { BrainAdapterSheet } from "../../components/brain/BrainAdapterSheet";
import { BrainChatHeader } from "../../components/brain/BrainChatHeader";
import { BrainOverflowMenu } from "../../components/brain/BrainOverflowMenu";
import { BrainWorkspaceViewer } from "../../components/brain/BrainWorkspaceViewer";
import { brainProviderLabel } from "../../components/brain/brainPresentation";
import { CodexChatSurface } from "../../components/terminal/CodexChatSurface";
import { buildChatChrome } from "../../theme";
import {
  Colors,
  Typography,
  useAppColors,
  useAppTheme,
} from "../../constants/tokens";
import { getServers, type StoredServer } from "../../services/storage";
import { wsClient } from "../../services/websocket";
import { useAgents, type ConnectionState } from "../../store/agents";
import {
  useBrain,
  type BrainAdapterRef,
  type BrainServerState,
} from "../../store/brain";

const BRAIN_EMPTY_TITLE = "Ready when you are";
const BRAIN_EMPTY_BODY =
  "Message Brain to delegate work, plan a task, or inspect the workspace.";

export default function BrainScreen() {
  const router = useRouter();
  const colors = useAppColors();
  const { theme: zenTheme } = useAppTheme();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const { chrome, theme } = useMemo(
    () => buildChatChrome(zenTheme),
    [zenTheme],
  );
  const { state: agentState } = useAgents();
  const { state: brainState } = useBrain();
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [adapterSheetVisible, setAdapterSheetVisible] = useState(false);
  const [menuVisible, setMenuVisible] = useState(false);
  const [switchingAdapterId, setSwitchingAdapterId] = useState<string | null>(null);
  const [adapterSwitchError, setAdapterSwitchError] = useState<string | null>(null);
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

  const activeServer = useMemo(
    () =>
      resolveActiveServer({
        connectedServers,
        servers,
        brainByServer: brainState.byServer,
        connectionStates: agentState.serverConnections,
      }),
    [
      agentState.serverConnections,
      brainState.byServer,
      connectedServers,
      servers,
    ],
  );

  const activeBrain = activeServer ? brainState.byServer[activeServer.id] : null;
  const connectionState: ConnectionState = activeServer
    ? agentState.serverConnections[activeServer.id] || "offline"
    : "offline";
  const connectionIssue = activeServer
    ? agentState.serverConnectionIssues[activeServer.id] ?? null
    : null;
  const hostAgent = activeBrain?.host_agent ?? null;
  const hostAdapter = activeBrain?.host_adapter ?? null;
  const brainChatScopeKey = activeBrain?.chat_thread_id
    ? `brain-thread:${activeBrain.chat_thread_id}`
    : undefined;
  const ready = Boolean(activeServer && activeBrain?.hydrated && hostAgent?.id);
  const canUseStructuredBrainInterface = Boolean(
    ready && hostAdapter?.capabilities?.structured_events,
  );
  const availableAdapters = activeBrain?.adapters ?? [];
  const canSwitchAdapter = availableAdapters.length > 1;
  const keyboardVerticalOffset = 0;

  useFocusEffect(
    useCallback(() => {
      if (!activeServer || connectionState !== "connected") {
        return;
      }
      wsClient.requestBrainSnapshot(activeServer.id);
    }, [activeServer?.id, connectionState]),
  );

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

  const startNewBrainChat = useCallback(async () => {
    if (!activeServer || !activeBrain?.hydrated || newChatLoading) {
      return;
    }
    setBrainActionError(null);
    setNewChatLoading(true);
    try {
      await wsClient.startNewBrainChat(activeServer.id);
    } catch (error: any) {
      setBrainActionError(error?.message || "Failed to start a new Brain chat.");
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
        await wsClient.setBrainAdapter(activeServer.id, adapter.id);
        closeAdapterSheet();
      } catch (error: any) {
        setAdapterSwitchError(error?.message || "Failed to switch adapter.");
      } finally {
        setSwitchingAdapterId(null);
      }
    },
    [activeServer, closeAdapterSheet, hostAdapter?.id, switchingAdapterId],
  );

  const menuActions = useMemo(
    () => [
      ...(canSwitchAdapter
        ? [
            {
              key: "engine",
              label: "Switch engine",
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
        disabled: !activeServer || !hostAgent?.id,
        onPress: openBrainTerminal,
      },
      {
        key: "workspace",
        label: "Browse workspace",
        icon: "folder-open-outline" as const,
        disabled: !activeServer || connectionState !== "connected",
        onPress: openWorkspaceViewer,
      },
    ],
    [
      activeServer,
      canSwitchAdapter,
      connectionState,
      hostAdapter?.provider,
      hostAgent?.id,
      openAdapterSheet,
      openBrainTerminal,
      openWorkspaceViewer,
    ],
  );

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <BrainChatHeader
        adapter={hostAdapter}
        workspace={hostAgent?.cwd}
        canSwitchAdapter={canSwitchAdapter}
        newChatLoading={newChatLoading}
        canNewChat={Boolean(activeServer && activeBrain?.hydrated)}
        canOpenTerminal={Boolean(activeServer && hostAgent?.id)}
        canOpenWorkspace={Boolean(activeServer && connectionState === "connected")}
        onOpenAdapterSheet={openAdapterSheet}
        onOpenMenu={openMenu}
        onNewChat={() => void startNewBrainChat()}
      />

      {brainActionError ? (
        <View style={styles.bannerError}>
          <Text style={styles.bannerErrorText}>{brainActionError}</Text>
        </View>
      ) : null}

      <View style={styles.chatSurface}>
        {canUseStructuredBrainInterface ? (
          <CodexChatSurface
            key={`brain-chat:${activeServer?.id}:${hostAgent?.id}:${brainChatScopeKey ?? ""}`}
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
            screenFocused
            placeholder="Message"
            minimalComposer
            showAttachmentControl
            keyboardVerticalOffset={keyboardVerticalOffset}
            showUnavailableAction
            emptyTitle={BRAIN_EMPTY_TITLE}
            emptyBody={BRAIN_EMPTY_BODY}
          />
        ) : ready ? (
          <BrainInterfaceUnavailableState provider={hostAdapter?.provider} />
        ) : (
          <BrainLoadingState connected={connectionState === "connected"} />
        )}
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
  glyph,
  title,
  detail,
}: {
  glyph: React.ReactNode;
  title: string;
  detail?: string;
}) {
  const colors = useAppColors();
  const styles = useMemo(() => createStateCardStyles(colors), [colors]);

  return (
    <View style={styles.card}>
      <View style={styles.glyphWrap}>{glyph}</View>
      <Text style={styles.title}>{title}</Text>
      {detail ? <Text style={styles.detail}>{detail}</Text> : null}
    </View>
  );
}

function BrainLoadingState({ connected }: { connected: boolean }) {
  const colors = useAppColors();
  return (
    <BrainStateCard
      glyph={
        connected ? (
          <ActivityIndicator size="small" color={colors.accent} />
        ) : (
          <Ionicons name="cloud-offline-outline" size={22} color={colors.textSecondary} />
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

function BrainInterfaceUnavailableState({ provider }: { provider?: string }) {
  const colors = useAppColors();
  const label = brainProviderLabel(provider);
  return (
    <BrainStateCard
      glyph={<Ionicons name="layers-outline" size={22} color={colors.textSecondary} />}
      title="Chat UI not available"
      detail={
        label
          ? `${label} is connected, but Brain chat requires Codex or Grok.`
          : "Switch the Brain engine to Codex or Grok to use this chat surface."
      }
    />
  );
}

function resolveActiveServer({
  connectedServers,
  servers,
  brainByServer,
  connectionStates,
}: {
  connectedServers: StoredServer[];
  servers: StoredServer[];
  brainByServer: Record<string, BrainServerState>;
  connectionStates: Record<string, ConnectionState>;
}): StoredServer | null {
  const hydratedConnected = connectedServers.find(
    (server) => brainByServer[server.id]?.hydrated,
  );
  if (hydratedConnected) {
    return hydratedConnected;
  }
  if (connectedServers[0]) {
    return connectedServers[0];
  }
  const connectedByState = servers.find(
    (server) => connectionStates[server.id] === "connected",
  );
  return connectedByState || servers[0] || null;
}

function createStateCardStyles(colors: typeof Colors) {
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
      backgroundColor: colors.accentSoft,
      marginBottom: 4,
    },
    title: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 18,
      lineHeight: 24,
      textAlign: "center",
    },
    detail: {
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 14,
      lineHeight: 20,
      textAlign: "center",
      maxWidth: 280,
      opacity: 0.9,
    },
  });
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    screen: {
      flex: 1,
      backgroundColor: colors.bgPrimary,
    },
    chatSurface: {
      flex: 1,
      minHeight: 0,
    },
    bannerError: {
      marginHorizontal: 12,
      marginBottom: 6,
      paddingHorizontal: 14,
      paddingVertical: 8,
      borderRadius: 12,
      backgroundColor: colors.dangerSoft,
      zIndex: 2,
    },
    bannerErrorText: {
      color: colors.dangerText,
      fontFamily: Typography.uiFont,
      fontSize: 12.5,
      lineHeight: 17,
    },
  });
}
