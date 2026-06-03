import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  Pressable,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useFocusEffect, useRouter } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
import { BottomSheetFrame, IconButton } from "../../components/ui";
import { AppButton } from "../../components/ui/AppButton";
import { AppText } from "../../components/ui/AppText";
import { CodexChatSurface } from "../../components/terminal/CodexChatSurface";
import {
  buildTerminalChrome,
  resolveTerminalTheme,
} from "../../constants/terminalThemes";
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

  useEffect(() => {
    if (!activeServer) {
      return;
    }
    wsClient.requestBrainSnapshot(activeServer.id);
  }, [activeServer?.id]);

  const activeBrain = activeServer ? brainState.byServer[activeServer.id] : null;
  const connectionState: ConnectionState = activeServer
    ? agentState.serverConnections[activeServer.id] || "offline"
    : "offline";
  const connectionIssue = activeServer
    ? agentState.serverConnectionIssues[activeServer.id] ?? null
    : null;
  const statusLabel = brainStatusLabel({
    activeServer,
    connectionState,
    activeBrain,
  });
  const hostAgent = activeBrain?.host_agent ?? null;
  const hostAdapter = activeBrain?.host_adapter ?? null;
  const brainChatScopeKey = activeBrain?.chat_thread_id
    ? `brain-thread:${activeBrain.chat_thread_id}`
    : undefined;
  const adapterLabel = brainAdapterLabel(activeBrain?.host_adapter);
  const ready = Boolean(activeServer && activeBrain?.hydrated && hostAgent?.id);
  const canUseCodexBrainInterface = Boolean(
    ready && hostAdapter?.provider === "codex",
  );
  const availableAdapters = activeBrain?.adapters ?? [];
  const subtitleLabel = [statusLabel, adapterLabel]
    .filter(Boolean)
    .join(" · ");
  const keyboardVerticalOffset = 0;

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

  const openBrainTerminal = useCallback(() => {
    if (!activeServer || !hostAgent?.id) {
      return;
    }
    router.push({
      pathname: "/terminal/[id]",
      params: {
        id: hostAgent.id,
        serverId: activeServer.id,
        cwd: hostAgent.cwd ?? "",
        command: hostAgent.command ?? "",
        name: hostAgent.name || "Brain",
      },
    });
  }, [
    activeServer,
    hostAgent?.command,
    hostAgent?.cwd,
    hostAgent?.id,
    hostAgent?.name,
    router,
  ]);

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
        <View style={styles.headerTitleBlock}>
          <Text style={styles.title}>Brain</Text>
          {activeBrain?.adapters?.length ? (
            <Pressable
              accessibilityRole="button"
              accessibilityLabel="Brain adapter"
              onPress={openAdapterSheet}
              style={({ pressed }) => [
                styles.subtitleChip,
                {
                  borderColor: colors.borderSubtle,
                  backgroundColor: colors.surfaceSubtle,
                },
                pressed ? styles.subtitleChipPressed : null,
              ]}
            >
              <Text style={styles.subtitle} numberOfLines={1}>
                {subtitleLabel}
              </Text>
              <Ionicons
                name="chevron-down"
                size={13}
                color={colors.textSecondary}
              />
            </Pressable>
          ) : (
            <Text style={styles.subtitle} numberOfLines={1}>
              {subtitleLabel}
            </Text>
          )}
        </View>
        <View style={styles.headerActions}>
          <IconButton
            icon="terminal-outline"
            size={36}
            iconSize={17}
            tone="ghost"
            color={colors.textSecondary}
            accessibilityRole="button"
            accessibilityLabel="Open Brain terminal"
            onPress={openBrainTerminal}
            disabled={!ready}
          />
          <IconButton
            icon="add-circle-outline"
            size={36}
            iconSize={17}
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
            placeholder="Message Brain"
            minimalComposer
            keyboardVerticalOffset={keyboardVerticalOffset}
            showUnavailableAction
            emptyTitle="Ready"
            emptyBody="Message Brain below."
            onSwitchToTerminal={openBrainTerminal}
          />
        ) : ready ? (
          <BrainInterfaceUnavailableState
            adapterLabel={adapterLabel}
            provider={hostAdapter?.provider}
          />
        ) : (
          <BrainLoadingState
            connected={connectionState === "connected"}
            hydrated={Boolean(activeBrain?.hydrated)}
            waitingForHost={Boolean(activeBrain?.hydrated && !hostAgent?.id)}
          />
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
            Adapters
          </AppText>
          <AppText variant="caption" tone="secondary">
            {availableAdapters.length} configured
          </AppText>
        </View>
        <View style={styles.sheetList}>
          {availableAdapters.map((adapter) => {
            const active = adapter.id === hostAdapter?.id;
            const busy = switchingAdapterId === adapter.id;
            return (
              <Pressable
                key={adapter.id}
                accessibilityRole="button"
                onPress={() => void switchBrainAdapter(adapter)}
                disabled={busy}
                style={({ pressed }) => [
                  styles.sheetRow,
                  {
                    borderColor: colors.borderSubtle,
                    backgroundColor: active
                      ? colors.surfaceActive
                      : colors.surfaceSubtle,
                  },
                  pressed && !busy ? styles.sheetRowPressed : null,
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
                  <AppText variant="caption" tone="secondary">
                    {brainAdapterDetails(adapter)}
                  </AppText>
                </View>
                {busy ? (
                  <ActivityIndicator size="small" color={colors.accent} />
                ) : null}
              </Pressable>
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
    </SafeAreaView>
  );
}

function BrainLoadingState({
  connected,
  hydrated,
  waitingForHost,
}: {
  connected: boolean;
  hydrated: boolean;
  waitingForHost?: boolean;
}) {
  const colors = useAppColors();
  return (
    <View style={loadingStyles.root}>
      {connected ? (
        <ActivityIndicator size="small" color={colors.accent} />
      ) : (
        <Ionicons name="cloud-offline-outline" size={22} color={colors.textSecondary} />
      )}
      <Text style={[loadingStyles.title, { color: colors.textPrimary }]}>
        {connected ? "Starting Brain" : "Offline"}
      </Text>
      <Text style={[loadingStyles.body, { color: colors.textSecondary }]}>
        {connected && waitingForHost
          ? "Getting your assistant ready."
          : connected && hydrated
            ? "Preparing your chat."
          : connected
            ? "Syncing Brain."
            : "Connect to a server to use Brain."}
      </Text>
    </View>
  );
}

function BrainInterfaceUnavailableState({
  adapterLabel,
  provider,
}: {
  adapterLabel: string;
  provider?: string;
}) {
  const colors = useAppColors();
  const label = adapterLabel || brainProviderLabel(provider);
  return (
    <View style={loadingStyles.root}>
      <Ionicons name="layers-outline" size={22} color={colors.textSecondary} />
      <Text style={[loadingStyles.title, { color: colors.textPrimary }]}>
        Interface unavailable
      </Text>
      <Text style={[loadingStyles.body, { color: colors.textSecondary }]}>
        {label
          ? `${label} does not expose a structured Brain interface yet.`
          : "This adapter does not expose a structured Brain interface yet."}
      </Text>
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

function brainStatusLabel({
  activeServer,
  connectionState,
  activeBrain,
}: {
  activeServer: StoredServer | null;
  connectionState: ConnectionState;
  activeBrain: BrainServerState | null;
}) {
  if (!activeServer) {
    return "Offline";
  }
  if (connectionState !== "connected") {
    return "Offline";
  }
  if (!activeBrain?.hydrated) {
    return "Syncing";
  }
  if (!activeBrain.host_agent?.id) {
    return "Starting";
  }
  return "Ready";
}

function brainAdapterLabel(adapter?: BrainAdapterRef | null) {
  if (!adapter) {
    return "";
  }
  const provider =
    adapter.provider && adapter.provider !== "custom"
      ? adapter.provider
      : adapter.name || adapter.id;
  const providerLabel = brainProviderLabel(provider);
  const runtime = adapter.runtime?.trim();
  return [providerLabel, runtime].filter(Boolean).join(" · ");
}

function brainAdapterDetails(adapter: BrainAdapterRef) {
  const provider = brainProviderLabel(
    adapter.provider && adapter.provider !== "custom"
      ? adapter.provider
      : adapter.name || adapter.id,
  );
  const runtime = adapter.runtime?.trim();
  const command = adapter.command?.trim();
  return [provider, runtime, command].filter(Boolean).join(" · ");
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
      minHeight: 58,
      paddingHorizontal: 16,
      paddingTop: 6,
      paddingBottom: 10,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    headerTitleBlock: {
      flex: 1,
      minWidth: 0,
      paddingRight: 12,
    },
    headerActions: {
      flexDirection: "row",
      alignItems: "center",
      gap: 6,
    },
    headerError: {
      paddingHorizontal: 16,
      paddingVertical: 7,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
      backgroundColor: colors.surfaceSubtle,
    },
    title: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 22,
      lineHeight: 27,
      letterSpacing: 0,
    },
    subtitle: {
      marginTop: 0,
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      lineHeight: 16,
      flexShrink: 1,
    },
    subtitleChip: {
      marginTop: 2,
      alignSelf: "flex-start",
      flexDirection: "row",
      alignItems: "center",
      gap: 4,
      paddingHorizontal: 8,
      paddingVertical: 4,
      borderRadius: 999,
      borderWidth: StyleSheet.hairlineWidth,
    },
    subtitleChipPressed: {
      opacity: 0.7,
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
      borderRadius: 14,
      borderWidth: StyleSheet.hairlineWidth,
      paddingHorizontal: 14,
      paddingVertical: 10,
      flexDirection: "row",
      alignItems: "center",
      justifyContent: "space-between",
      gap: 12,
    },
    sheetRowPressed: {
      opacity: 0.75,
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
  },
  title: {
    marginTop: 14,
    fontFamily: Typography.uiFontMedium,
    fontSize: 16,
    lineHeight: 21,
    textAlign: "center",
  },
  body: {
    marginTop: 6,
    fontFamily: Typography.uiFont,
    fontSize: 13,
    lineHeight: 19,
    textAlign: "center",
  },
});
