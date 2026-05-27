import React, { useEffect, useMemo, useState } from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useFocusEffect } from "expo-router";
import { SafeAreaView } from "react-native-safe-area-context";
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
import { useBrain, type BrainServerState } from "../../store/brain";

export default function BrainScreen() {
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
  const statusLabel = brainStatusLabel({
    activeServer,
    connectionState,
    activeBrain,
  });
  const hostAgent = activeBrain?.host_agent ?? null;
  const ready = Boolean(activeServer && activeBrain?.hydrated && hostAgent?.id);
  const keyboardVerticalOffset = 0;

  return (
    <SafeAreaView style={styles.screen} edges={["top"]}>
      <View style={styles.header}>
        <View style={styles.headerTitleBlock}>
          <Text style={styles.title}>Brain</Text>
          <Text style={styles.subtitle} numberOfLines={1}>
            {statusLabel}
          </Text>
        </View>
      </View>

      <View style={styles.surface}>
        {ready ? (
          <CodexChatSurface
            key={`brain-chat:${activeServer?.id}:${hostAgent?.id}`}
            visible
            serverId={activeServer?.id ?? ""}
            agentId={hostAgent?.id ?? ""}
            agentInfo={{
              cwd: hostAgent?.cwd,
              command: hostAgent?.command,
              name: hostAgent?.name,
              startedAt: hostAgent?.updated_at
                ? Date.parse(hostAgent.updated_at)
                : undefined,
            }}
            connectionState={connectionState}
            theme={terminalTheme}
            chrome={chrome}
            screenFocused
            placeholder="Message Brain"
            minimalComposer
            keyboardVerticalOffset={keyboardVerticalOffset}
            showUnavailableAction={false}
            emptyTitle="Ready"
            emptyBody="Send a message to get started."
            onSwitchToTerminal={() => {}}
          />
        ) : (
          <BrainLoadingState
            connected={connectionState === "connected"}
            hydrated={Boolean(activeBrain?.hydrated)}
            waitingForHost={Boolean(activeBrain?.hydrated && !hostAgent?.id)}
          />
        )}
      </View>
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
    title: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 22,
      lineHeight: 27,
      letterSpacing: 0,
    },
    subtitle: {
      marginTop: 2,
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      lineHeight: 16,
    },
    surface: {
      flex: 1,
      minHeight: 0,
      backgroundColor: colors.bgPrimary,
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
