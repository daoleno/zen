import React, { useCallback, useMemo, useState } from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import * as Haptics from "expo-haptics";
import { useFocusEffect, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { BrainWorkspaceViewer } from "../../components/brain/BrainWorkspaceViewer";
import { BottomSheetFrame, IconButton } from "../../components/ui";
import { AppButton } from "../../components/ui/AppButton";
import { AppText } from "../../components/ui/AppText";
import { CodexChatSurface } from "../../components/terminal/CodexChatSurface";
import { AnimatedPressable } from "../../components/ui/AnimatedPressable";
import {
  buildTerminalChrome,
  resolveTerminalTheme,
} from "../../constants/terminalThemes";
import {
  Colors,
  Radii,
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

export default function BrainScreen() {
  const router = useRouter();
  const colors = useAppColors();
  const { isLight } = useAppTheme();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const terminalTheme = useMemo(
    () => resolveTerminalTheme(isLight ? "light" : "dark"),
    [isLight],
  );
  const chrome = useMemo(() => buildTerminalChrome(terminalTheme), [terminalTheme]);
  const { state: agentState } = useAgents();
  const { state: brainState } = useBrain();
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [adapterSheetVisible, setAdapterSheetVisible] = useState(false);
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
  const adapterChipLabel = brainAdapterChipLabel(activeBrain?.host_adapter);
  const ready = Boolean(activeServer && activeBrain?.hydrated && hostAgent?.id);
  const canUseCodexBrainInterface = Boolean(
    ready && hostAdapter?.provider === "codex",
  );
  const availableAdapters = activeBrain?.adapters ?? [];
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
    if (!activeBrain?.adapters?.length || !activeServer) {
      return;
    }
    setAdapterSwitchError(null);
    setAdapterSheetVisible(true);
  }, [activeBrain?.adapters?.length, activeServer]);

  const closeAdapterSheet = useCallback(() => {
    setAdapterSheetVisible(false);
    setAdapterSwitchError(null);
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

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <View style={styles.header}>
        <View style={styles.headerLeft}>
          <Text style={styles.title}>Brain</Text>
          {activeBrain?.adapters?.length && adapterChipLabel ? (
            <AnimatedPressable
              accessibilityRole="button"
              accessibilityLabel="Switch Brain adapter"
              onPress={() => {
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                openAdapterSheet();
              }}
              preset="press"
              scale={0.98}
              style={styles.adapterRow}
            >
              <Text style={styles.adapterText} numberOfLines={1}>
                {adapterChipLabel}
              </Text>
              <Ionicons name="chevron-down" size={13} color={colors.textTertiary} />
            </AnimatedPressable>
          ) : null}
        </View>
        <View style={styles.headerActions}>
          <IconButton
            icon="terminal-outline"
            size={36}
            iconSize={18}
            tone="ghost"
            color={colors.textSecondary}
            accessibilityRole="button"
            accessibilityLabel="Open Brain terminal"
            onPress={openBrainTerminal}
            disabled={!activeServer || !hostAgent?.id}
          />
          <IconButton
            icon="folder-open-outline"
            size={36}
            iconSize={18}
            tone="ghost"
            color={colors.textSecondary}
            accessibilityRole="button"
            accessibilityLabel="Browse Brain workspace"
            onPress={openWorkspaceViewer}
            disabled={!activeServer || connectionState !== "connected"}
          />
          <IconButton
            icon="add"
            size={36}
            iconSize={20}
            tone="ghost"
            color={colors.textSecondary}
            accessibilityRole="button"
            accessibilityLabel="New Brain chat"
            onPress={() => void startNewBrainChat()}
            disabled={!activeServer || !activeBrain?.hydrated || newChatLoading}
          />
        </View>
      </View>
      {brainActionError ? (
        <View style={styles.headerError}>
          <AppText variant="caption" tone="danger">
            {brainActionError}
          </AppText>
        </View>
      ) : null}

      <View style={styles.surface}>
        {canUseCodexBrainInterface ? (
          <CodexChatSurface
            key={`brain-codex-chat:${activeServer?.id}:${hostAgent?.id}:${brainChatScopeKey ?? ""}`}
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
            theme={terminalTheme}
            chrome={chrome}
            screenFocused
            placeholder="Message"
            minimalComposer
            showAttachmentControl
            keyboardVerticalOffset={keyboardVerticalOffset}
            showUnavailableAction
          />
        ) : ready ? (
          <BrainInterfaceUnavailableState provider={hostAdapter?.provider} />
        ) : (
          <BrainLoadingState connected={connectionState === "connected"} />
        )}
      </View>

      <BottomSheetFrame
        visible={adapterSheetVisible}
        onClose={closeAdapterSheet}
        keyboardAvoiding
        maxHeight="68%"
      >
        <View style={styles.sheetHeader}>
          <AppText variant="title" tone="primary">
            Adapter
          </AppText>
        </View>
        <View style={styles.sheetList}>
          {availableAdapters.map((adapter) => {
            const active = adapter.id === hostAdapter?.id;
            const busy = switchingAdapterId === adapter.id;
            return (
              <AnimatedPressable
                key={adapter.id}
                accessibilityRole="button"
                onPress={() => {
                  if (!busy) {
                    Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                    void switchBrainAdapter(adapter);
                  }
                }}
                disabled={busy}
                preset="press"
                scale={0.98}
                style={[
                  styles.sheetRow,
                  {
                    borderColor: colors.borderSubtle,
                    backgroundColor: active
                      ? colors.surfaceActive
                      : colors.surfaceSubtle,
                  },
                  busy ? styles.sheetRowBusy : null,
                ]}
              >
                <View style={styles.sheetRowMain}>
                  <View style={styles.sheetRowTitleLine}>
                    <AppText variant="body" tone="primary" style={styles.sheetRowTitle}>
                      {adapter.name || adapter.id}
                    </AppText>
                    {active ? (
                      <Ionicons name="checkmark" size={16} color={colors.accent} />
                    ) : null}
                  </View>
                  {adapter.runtime?.trim() ? (
                    <AppText variant="caption" tone="secondary">
                      {adapter.runtime.trim()}
                    </AppText>
                  ) : null}
                </View>
                {busy ? (
                  <ActivityIndicator size="small" color={colors.accent} />
                ) : null}
              </AnimatedPressable>
            );
          })}
        </View>
        {adapterSwitchError ? (
          <AppText variant="caption" tone="danger" style={styles.sheetError}>
            {adapterSwitchError}
          </AppText>
        ) : null}
        <View style={styles.sheetFooter}>
          <AppButton label="Close" variant="ghost" onPress={closeAdapterSheet} />
        </View>
      </BottomSheetFrame>

      <BrainWorkspaceViewer
        visible={workspaceViewerVisible}
        serverId={activeServer?.id}
        workspace={activeBrain?.workspace}
        chrome={chrome}
        theme={terminalTheme}
        onClose={closeWorkspaceViewer}
      />
    </SafeAreaView>
  );
}

function BrainLoadingState({ connected }: { connected: boolean }) {
  const colors = useAppColors();
  return (
    <View style={loadingStyles.root}>
      {connected ? (
        <ActivityIndicator size="small" color={colors.accent} />
      ) : (
        <Ionicons name="cloud-offline-outline" size={20} color={colors.textSecondary} />
      )}
    </View>
  );
}

function BrainInterfaceUnavailableState({ provider }: { provider?: string }) {
  const colors = useAppColors();
  const label = brainProviderLabel(provider);
  return (
    <View style={loadingStyles.root}>
      <Ionicons name="layers-outline" size={20} color={colors.textSecondary} />
      {label ? (
        <Text style={[loadingStyles.caption, { color: colors.textSecondary }]}>
          {label}
        </Text>
      ) : null}
    </View>
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

function brainAdapterChipLabel(adapter?: BrainAdapterRef | null) {
  if (!adapter) {
    return "";
  }
  if (adapter.name?.trim()) {
    return adapter.name.trim();
  }
  return brainProviderLabel(
    adapter.provider && adapter.provider !== "custom"
      ? adapter.provider
      : adapter.id,
  );
}

function brainProviderLabel(value?: string) {
  const normalized = value?.trim().toLowerCase();
  switch (normalized) {
    case "codex":
      return "Codex";
    case "claude":
      return "Claude Code";
    case "tmux":
      return "tmux";
    default:
      return value?.trim() || "";
  }
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    screen: {
      flex: 1,
      backgroundColor: colors.bgPrimary,
    },
    header: {
      paddingHorizontal: 18,
      paddingTop: 14,
      paddingBottom: 12,
      flexDirection: "row",
      alignItems: "flex-start",
      justifyContent: "space-between",
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    headerLeft: {
      flex: 1,
      minWidth: 0,
      paddingRight: 12,
      gap: 2,
    },
    headerActions: {
      flexDirection: "row",
      alignItems: "center",
      gap: 2,
      paddingTop: 2,
    },
    title: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 30,
      lineHeight: 34,
      letterSpacing: -0.6,
    },
    adapterRow: {
      flexDirection: "row",
      alignItems: "center",
      alignSelf: "flex-start",
      gap: 2,
      marginTop: 2,
      paddingVertical: 2,
      paddingRight: 4,
    },
    adapterText: {
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      lineHeight: 18,
      flexShrink: 1,
    },
    headerError: {
      paddingHorizontal: 16,
      paddingVertical: 7,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
      backgroundColor: colors.surfaceSubtle,
    },
    surface: {
      flex: 1,
      minHeight: 0,
      backgroundColor: colors.bgPrimary,
    },
    sheetHeader: {
      marginBottom: 12,
    },
    sheetList: {
      gap: 8,
    },
    sheetRow: {
      minHeight: 56,
      borderRadius: Radii.md,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 14,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 12,
    },
    sheetRowBusy: {
      opacity: 0.55,
    },
    sheetRowMain: {
      flex: 1,
      minWidth: 0,
    },
    sheetRowTitleLine: {
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 8,
    },
    sheetRowTitle: {
      flex: 1,
      minWidth: 0,
    },
    sheetError: {
      marginTop: 10,
    },
    sheetFooter: {
      marginTop: 12,
      alignItems: "flex-end",
    },
  });
}

const loadingStyles = StyleSheet.create({
  root: {
    flex: 1,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 28,
    gap: 10,
  },
  caption: {
    fontFamily: Typography.uiFont,
    fontSize: 12,
    lineHeight: 16,
    textAlign: "center",
    opacity: 0.72,
  },
});
