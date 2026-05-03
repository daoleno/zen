import { useEffect, useRef } from "react";
import { Alert, AppState, AppStateStatus, Platform } from "react-native";
import { Stack, useRouter, useSegments } from "expo-router";
import { StatusBar } from "expo-status-bar";
import { useFonts } from "expo-font";
import * as Linking from "expo-linking";
import * as Notifications from "expo-notifications";
import * as Device from "expo-device";
import Constants from "expo-constants";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { SafeAreaProvider } from "react-native-safe-area-context";
import { Agent, AgentProvider, useAgents } from "../store/agents";
import { IssuesProvider, useIssues } from "../store/issues";
import { useAppTheme } from "../constants/tokens";
import { wsClient } from "../services/websocket";
import {
  getDisabledServerIds,
  getServers,
  isOnboarded,
} from "../services/storage";
import { importConnection } from "../services/importConnection";
import {
  clearNativeTerminalCrashBreadcrumb,
  getNativeTerminalCrashBreadcrumb,
} from "../services/nativeTerminalDiagnostics";
import { measureServerLatency } from "../services/serverLatency";

Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldPlaySound: true,
    shouldSetBadge: false,
    shouldShowBanner: true,
    shouldShowList: true,
  }),
});

async function registerForPushNotificationsAsync(): Promise<
  string | undefined
> {
  if (Platform.OS === "android") {
    await Notifications.setNotificationChannelAsync("zen-agents", {
      name: "Agent Alerts",
      importance: Notifications.AndroidImportance.MAX,
      vibrationPattern: [0, 250, 250, 250],
    });
  }

  if (!Device.isDevice) {
    console.log("Push notifications require a physical device");
    return;
  }

  const { status: existingStatus } = await Notifications.getPermissionsAsync();
  let finalStatus = existingStatus;
  if (existingStatus !== "granted") {
    const { status } = await Notifications.requestPermissionsAsync();
    finalStatus = status;
  }
  if (finalStatus !== "granted") return;

  const projectId =
    Constants?.expoConfig?.extra?.eas?.projectId ??
    Constants?.easConfig?.projectId;

  if (!projectId) {
    console.log(
      "Push notifications disabled: Expo project ID is not configured.",
    );
    return;
  }

  try {
    const token = (await Notifications.getExpoPushTokenAsync({ projectId }))
      .data;
    return token;
  } catch (e) {
    console.log("Failed to get push token:", e);
    return;
  }
}

function buildNotificationContent(
  agent: Agent,
): Notifications.NotificationContentInput | null {
  const label = formatNotificationAgentLabel(agent);
  const summary = normalizeNotificationSummary(agent.summary);

  switch (agent.status) {
    case "blocked":
      return {
        title: label ? `${label} needs input` : "Agent needs input",
        body: summary || "Waiting for your response.",
        data: {
          agent_id: agent.id,
          server_id: agent.serverId,
          screen: "terminal",
        },
        sound: "default",
      };
    case "failed":
      return {
        title: label ? `${label} failed` : "Agent failed",
        body: summary || "Check the terminal for details.",
        data: {
          agent_id: agent.id,
          server_id: agent.serverId,
          screen: "terminal",
        },
        sound: "default",
      };
    case "done":
      return {
        title: label ? `${label} finished` : "Agent finished",
        body: summary || "Session completed.",
        data: {
          agent_id: agent.id,
          server_id: agent.serverId,
          screen: "terminal",
        },
        sound: "default",
      };
    default:
      return null;
  }
}

function formatNotificationAgentLabel(agent: Agent): string {
  const raw = agent.project?.trim() || agent.name?.trim() || agent.id;
  if (!raw) {
    return "";
  }

  const withoutSessionSuffix = raw.replace(/\s+\([^)]+\)\s*$/, "");
  const parts = withoutSessionSuffix.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || withoutSessionSuffix;
}

function normalizeNotificationSummary(summary: string | undefined): string {
  if (!summary) {
    return "";
  }

  const collapsed = summary
    .replace(/^\d{4}[/-]\d{2}[/-]\d{2}[ T]\d{2}:\d{2}:\d{2}\s*/, "")
    .replace(/\s+/g, " ")
    .trim();

  if (collapsed.length <= 110) {
    return collapsed;
  }

  return `${collapsed.slice(0, 107)}...`;
}

function AppContent() {
  const router = useRouter();
  const segments = useSegments();
  const { state, dispatch } = useAgents();
  const { dispatch: issuesDispatch } = useIssues();
  const { colors } = useAppTheme();
  const notificationListener = useRef<Notifications.EventSubscription | null>(
    null,
  );
  const responseListener = useRef<Notifications.EventSubscription | null>(null);
  const appStateRef = useRef<AppStateStatus>(AppState.currentState);
  const notificationsEnabledRef = useRef(false);
  const previousAgentStatesRef = useRef(new Map<string, Agent["status"]>());
  const handledConnectLinksRef = useRef(new Set<string>());
  const isTerminalRouteActive = segments[0] === "terminal";

  useEffect(() => {
    const breadcrumb = getNativeTerminalCrashBreadcrumb();
    if (!breadcrumb || breadcrumb.stage !== "before") {
      return;
    }

    clearNativeTerminalCrashBreadcrumb();

    const detail = breadcrumb.detail ? `\n${breadcrumb.detail}` : "";
    const device = [breadcrumb.brand, breadcrumb.model]
      .filter(Boolean)
      .join(" ")
      .trim();
    const environment = [device, breadcrumb.abi].filter(Boolean).join(" / ");
    const footer =
      environment || breadcrumb.sdkInt
        ? `\n\n${[
            environment,
            breadcrumb.sdkInt ? `Android ${breadcrumb.sdkInt}` : "",
          ]
            .filter(Boolean)
            .join(" / ")}`
        : "";

    Alert.alert(
      "Native terminal crashed last run",
      `Last unfinished step: ${breadcrumb.operation}${detail}${footer}`,
    );
  }, []);

  const importConnectLink = async (
    rawValue: string | null | undefined,
  ): Promise<boolean> => {
    const trimmed = rawValue?.trim() || "";
    if (!trimmed || handledConnectLinksRef.current.has(trimmed)) {
      return false;
    }

    handledConnectLinksRef.current.add(trimmed);

    try {
      const savedServer = await importConnection(trimmed, {
        onImported: () => {
          router.replace({
            pathname: "/settings",
            params: { refresh: Date.now().toString() },
          });
        },
      });
      if (!savedServer) {
        handledConnectLinksRef.current.delete(trimmed);
        return false;
      }
      return true;
    } catch (error) {
      handledConnectLinksRef.current.delete(trimmed);
      console.log("Failed to import connect link:", error);
      return false;
    }
  };

  // Auto-connect on app start.
  useEffect(() => {
    const onAgentSessionList = (data: any) =>
      dispatch({
        type: "UPSERT_SERVER_AGENTS",
        serverId: data.serverId,
        serverName: data.serverName,
        serverUrl: data.serverUrl,
        agents: data.agent_sessions || [],
      });
    const onAgentSessionUpsert = (data: any) =>
      dispatch({
        type: "UPSERT_AGENT",
        serverId: data.serverId,
        serverName: data.serverName,
        serverUrl: data.serverUrl,
        agent: data.agent_session,
      });
    const onAgentSessionArchived = (data: any) =>
      dispatch({
        type: "REMOVE_AGENT",
        serverId: data.serverId,
        agent_id: data.agent_session?.id || "",
      });
    const onConnecting = (data: any) =>
      dispatch({
        type: "SET_SERVER_CONNECTION_STATE",
        serverId: data.serverId,
        connectionState: "connecting",
      });
    const onConnected = (data: any) =>
      dispatch({
        type: "SET_SERVER_CONNECTION_STATE",
        serverId: data.serverId,
        connectionState: "connected",
      });
    const onDisconnected = (data: any) =>
      {
        dispatch({
          type: "SET_SERVER_CONNECTION_STATE",
          serverId: data.serverId,
          connectionState: "offline",
        });
        issuesDispatch({
          type: "REMOVE_SERVER",
          serverId: data.serverId,
        });
      };
    const onConnectionIssue = (data: any) =>
      dispatch({
        type: "SET_SERVER_CONNECTION_ISSUE",
        serverId: data.serverId,
        issue: data.issue || null,
      });

    const onIssuesSnapshot = (data: any) =>
      issuesDispatch({
        type: "ISSUES_SNAPSHOT",
        serverId: data.serverId,
        serverName: data.serverName,
        serverUrl: data.serverUrl,
        issues: data.issues || [],
        executors: data.executors || [],
      });
    const onIssueChanged = (data: any) => {
      if (!data.issue) {
        return;
      }
      issuesDispatch({
        type: "ISSUE_CHANGED",
        serverId: data.serverId,
        serverName: data.serverName,
        serverUrl: data.serverUrl,
        issue: data.issue,
      });
    };
    const onIssueDeleted = (data: any) =>
      issuesDispatch({
        type: "ISSUE_DELETED",
        serverId: data.serverId,
        id: data.id,
        path: data.path,
      });
    const onExecutors = (data: any) =>
      issuesDispatch({
        type: "EXECUTORS_LOADED",
        serverId: data.serverId,
        executors: data.executors || [],
      });

    const onConnectedFetchIssues = (data: any) => {
      wsClient.listIssues(data.serverId);
      wsClient.listExecutors(data.serverId);
      wsClient.listAgentSessions(data.serverId);
    };

    wsClient.on("agent_session_list", onAgentSessionList);
    wsClient.on("agent_session_created", onAgentSessionUpsert);
    wsClient.on("agent_session_updated", onAgentSessionUpsert);
    wsClient.on("agent_session_archived", onAgentSessionArchived);
    wsClient.on("connecting", onConnecting);
    wsClient.on("connected", onConnected);
    wsClient.on("disconnected", onDisconnected);
    wsClient.on("connection_issue", onConnectionIssue);
    wsClient.on("issues_snapshot", onIssuesSnapshot);
    wsClient.on("issue_changed", onIssueChanged);
    wsClient.on("issue_deleted", onIssueDeleted);
    wsClient.on("executor_list", onExecutors);
    wsClient.on("connected", onConnectedFetchIssues);

    (async () => {
      try {
        const initialURL = await Linking.getInitialURL();
        const imported = await importConnectLink(initialURL);
        if (imported) {
          return;
        }

        const onboarded = await isOnboarded();
        if (!onboarded && segments[0] !== "onboarding") {
          router.replace("/onboarding");
          return;
        }

        const [servers, disabledServerIds] = await Promise.all([
          getServers(),
          getDisabledServerIds(),
        ]);
        const disabledSet = new Set(disabledServerIds);
        servers.forEach((server) => {
          if (disabledSet.has(server.id)) {
            return;
          }
          wsClient.connectServer(server);
        });
      } catch (error) {
        console.log("Failed to bootstrap app:", error);
      } finally {
        wsClient.clearActiveAgentsExcept(null);
      }
    })();

    return () => {
      // Disconnect first so the mounted listeners can drive connection state
      // back to offline during hot reloads and remounts.
      wsClient.disconnectAll();

      wsClient.off("agent_session_list", onAgentSessionList);
      wsClient.off("agent_session_created", onAgentSessionUpsert);
      wsClient.off("agent_session_updated", onAgentSessionUpsert);
      wsClient.off("agent_session_archived", onAgentSessionArchived);
      wsClient.off("connecting", onConnecting);
      wsClient.off("connected", onConnected);
      wsClient.off("disconnected", onDisconnected);
      wsClient.off("connection_issue", onConnectionIssue);
      wsClient.off("issues_snapshot", onIssuesSnapshot);
      wsClient.off("issue_changed", onIssueChanged);
      wsClient.off("issue_deleted", onIssueDeleted);
      wsClient.off("executor_list", onExecutors);
      wsClient.off("connected", onConnectedFetchIssues);
    };
  }, [dispatch, issuesDispatch, router, segments]);

  useEffect(() => {
    const subscription = Linking.addEventListener("url", (event) => {
      void importConnectLink(event.url);
    });

    return () => {
      subscription.remove();
    };
  }, [router]);

  useEffect(() => {
    const subscription = AppState.addEventListener("change", (nextState) => {
      appStateRef.current = nextState;
      if (nextState !== "active") {
        wsClient.clearActiveAgentsExcept(null);
      }
    });

    return () => {
      subscription.remove();
    };
  }, []);

  useEffect(() => {
    let cancelled = false;

    const refreshServerLatency = async () => {
      const servers = await getServers();
      if (cancelled) {
        return;
      }

      const connectedServers = servers.filter(
        (server) => state.serverConnections[server.id] === "connected",
      );
      if (connectedServers.length === 0) {
        return;
      }

      const samples = await Promise.all(
        connectedServers.map(async (server) => {
          try {
            return [
              server.id,
              await measureServerLatency({
                serverUrl: server.url,
                daemonId: server.daemonId,
              }),
            ] as const;
          } catch {
            return [server.id, null] as const;
          }
        }),
      );

      if (cancelled) {
        return;
      }

      for (const [serverId, sample] of samples) {
        if (!sample) {
          continue;
        }
        dispatch({
          type: "SET_SERVER_LATENCY",
          serverId,
          sample,
        });
      }
    };

    void refreshServerLatency();
    const interval = setInterval(() => {
      void refreshServerLatency();
    }, 15000);

    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [dispatch, state.serverConnections]);

  useEffect(() => {
    const nextAgentStates = new Map(
      state.agents.map((agent) => [agent.key, agent.status]),
    );
    const previousAgentStates = previousAgentStatesRef.current;

    if (previousAgentStates.size === 0) {
      previousAgentStatesRef.current = nextAgentStates;
      return;
    }

    if (!notificationsEnabledRef.current || appStateRef.current !== "active") {
      previousAgentStatesRef.current = nextAgentStates;
      return;
    }

    // Suppress all local notifications while user is in any terminal session.
    if (isTerminalRouteActive) {
      previousAgentStatesRef.current = nextAgentStates;
      return;
    }

    for (const agent of state.agents) {
      const previousState = previousAgentStates.get(agent.key);
      if (!previousState || previousState === agent.status) {
        continue;
      }

      const content = buildNotificationContent(agent);
      if (!content) {
        continue;
      }

      void Notifications.scheduleNotificationAsync({
        content,
        trigger: null,
      });
    }

    previousAgentStatesRef.current = nextAgentStates;
  }, [isTerminalRouteActive, state.agents]);

  // Register permissions and push token.
  useEffect(() => {
    let cancelled = false;
    let onConnected: ((data: any) => void) | null = null;

    (async () => {
      const token = await registerForPushNotificationsAsync();
      if (cancelled) {
        return;
      }

      const { status } = await Notifications.getPermissionsAsync();
      notificationsEnabledRef.current = status === "granted";

      if (!token) {
        return;
      }

      const registerPush = (serverId: string) => {
        wsClient.send(serverId, {
          type: "register_push",
          push_token: token,
          server_ref: serverId,
        });
      };

      onConnected = (data: any) => {
        registerPush(data.serverId);
      };
      wsClient.on("connected", onConnected);

      for (const serverId of wsClient.connectedServerIds()) {
        registerPush(serverId);
      }
    })();

    notificationListener.current =
      Notifications.addNotificationReceivedListener((notification) => {
        const content = notification.request.content;
        console.log("Notification received:", {
          title: content.title,
          body: content.body,
          data: content.data,
        });
      });

    responseListener.current =
      Notifications.addNotificationResponseReceivedListener((response) => {
        const data = response.notification.request.content.data;
        const agentId =
          typeof data?.agent_id === "string" ? data.agent_id : null;
        const serverId =
          typeof data?.server_id === "string" ? data.server_id : null;

        if (agentId && serverId) {
          router.push({
            pathname: "/terminal/[id]",
            params: { id: agentId, serverId },
          });
          return;
        }
        if (data?.screen === "inbox") {
          router.push("/");
        }
      });

    return () => {
      cancelled = true;
      if (onConnected) {
        wsClient.off("connected", onConnected);
      }
      notificationListener.current?.remove();
      responseListener.current?.remove();
    };
  }, [router]);

  return (
    <Stack
      screenOptions={{
        headerStyle: { backgroundColor: colors.bgPrimary },
        headerTintColor: colors.textPrimary,
        contentStyle: { backgroundColor: colors.bgPrimary },
        animation: "slide_from_right",
      }}
    >
      <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
      <Stack.Screen
        name="terminal/[id]"
        options={{ headerShown: false, animation: "none" }}
      />
      <Stack.Screen name="issue/[id]" options={{ headerShown: false }} />
      <Stack.Screen
        name="onboarding"
        options={{ headerShown: false, presentation: "modal" }}
      />
    </Stack>
  );
}

export default function RootLayout() {
  const { isLight } = useAppTheme();
  const [fontsLoaded, fontError] = useFonts({
    "SourceHanSansSC-Regular": require("../assets/fonts/SourceHanSansSC-Regular.otf"),
    "SourceHanSansSC-Medium": require("../assets/fonts/SourceHanSansSC-Medium.otf"),
    "MapleMono-CN-Regular": require("../assets/fonts/MapleMono-CN-Regular.ttf"),
    "MapleMono-CN-SemiBold": require("../assets/fonts/MapleMono-CN-SemiBold.ttf"),
  });

  useEffect(() => {
    if (fontError) {
      console.log("Failed to load fonts:", fontError);
    }
  }, [fontError]);

  if (!fontsLoaded && !fontError) {
    return null;
  }

  return (
    <GestureHandlerRootView style={{ flex: 1 }}>
      <AgentProvider>
        <IssuesProvider>
          <SafeAreaProvider>
            <StatusBar style={isLight ? "dark" : "light"} />
            <AppContent />
          </SafeAreaProvider>
        </IssuesProvider>
      </AgentProvider>
    </GestureHandlerRootView>
  );
}
