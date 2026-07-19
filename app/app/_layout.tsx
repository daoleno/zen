import { memo, useCallback, useEffect, useRef, useState } from "react";
import {
  AppState,
  Platform,
  Pressable,
} from "react-native";
import {
  Stack,
  useGlobalSearchParams,
  useNavigation,
  useRouter,
  useSegments,
} from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { StatusBar } from "expo-status-bar";
import { useFonts } from "expo-font";
import * as Linking from "expo-linking";
import * as Notifications from "expo-notifications";
import * as Device from "expo-device";
import Constants from "expo-constants";
import { GestureHandlerRootView } from "react-native-gesture-handler";
import { KeyboardProvider } from "react-native-keyboard-controller";
import { SafeAreaProvider } from "react-native-safe-area-context";
import {
  AgentProvider,
  useAgentDispatch,
  useAgentServerConnections,
} from "../store/agents";
import { BrainProvider, useBrainDispatch } from "../store/brain";
import { WorkProvider, useWorkDispatch } from "../store/work";
import { CalendarProvider, useCalendarDispatch } from "../store/calendar";
import { syncCalendarNotifications } from "../services/calendarNotifications";
import { useAppTheme } from "../constants/tokens";
import { ThemeProvider } from "../theme";
import { wsClient } from "../services/websocket";
import {
  createConnectedReadRefreshHandler,
  decideDisconnectLifecycle,
} from "../services/connectionLifecycle";
import {
  getDisabledServerIds,
  getServers,
  isOnboarded,
  markOnboarded,
} from "../services/storage";
import { importConnection } from "../services/importConnection";
import {
  clearNativeTerminalCrashBreadcrumb,
  getNativeTerminalCrashBreadcrumb,
} from "../services/nativeTerminalDiagnostics";
import { consumeUnfinishedNativeTerminalBreadcrumb } from "../services/nativeTerminalDiagnosticsObserver";
import { measureServerLatency } from "../services/serverLatency";
import {
  foregroundNotificationPresentation,
  resolveNotificationDestination,
} from "../services/notificationRouting";
import {
  screenshotDemoEnabled,
  shouldUseScreenshotDemoRuntime,
} from "../services/screenshotDemo";

Notifications.setNotificationHandler({
  handleNotification: foregroundNotificationPresentation,
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

function AppRuntime() {
  const segments = useSegments();
  const params = useGlobalSearchParams<{
    demo?: string | string[];
  }>();
  if (
    shouldUseScreenshotDemoRuntime({
      demo: params.demo,
      enabled: screenshotDemoEnabled(),
      rootSegment: segments[0],
    })
  ) {
    return <ScreenshotDemoRuntime />;
  }

  return <LiveAppRuntime />;
}

function LiveAppRuntime() {
  const [bootstrapResolved, setBootstrapResolved] = useState(false);
  const handleBootstrapResolved = useCallback((resolved: boolean) => {
    setBootstrapResolved(resolved);
  }, []);

  return (
    <>
      <NativeTerminalDiagnosticsObserver />
      <ConnectionLifecycle onBootstrapResolved={handleBootstrapResolved} />
      <LatencySampler />
      <NotificationObserver />
      <AppNavigator bootstrapResolved={bootstrapResolved} />
    </>
  );
}

function ScreenshotDemoRuntime() {
  return <AppNavigator bootstrapResolved />;
}

const NativeTerminalDiagnosticsObserver = memo(
  function NativeTerminalDiagnosticsObserver() {
    useEffect(() => {
      consumeUnfinishedNativeTerminalBreadcrumb({
        breadcrumb: getNativeTerminalCrashBreadcrumb(),
        clearBreadcrumb: clearNativeTerminalCrashBreadcrumb,
        log: (message, diagnostic) => {
          console.log(message, diagnostic);
        },
      });
    }, []);

    return null;
  },
);

interface ConnectionLifecycleProps {
  onBootstrapResolved(resolved: boolean): void;
}

const ConnectionLifecycle = memo(function ConnectionLifecycle({
  onBootstrapResolved,
}: ConnectionLifecycleProps) {
  const router = useRouter();
  const segments = useSegments();
  const dispatch = useAgentDispatch();
  const brainDispatch = useBrainDispatch();
  const workDispatch = useWorkDispatch();
  const calendarDispatch = useCalendarDispatch();
  const routerRef = useRef(router);
  const handledConnectLinksRef = useRef(new Set<string>());
  const rootSegment = segments[0];
  const rootSegmentRef = useRef(rootSegment);

  useEffect(() => {
    rootSegmentRef.current = rootSegment;
  }, [rootSegment]);

  useEffect(() => {
    routerRef.current = router;
  }, [router]);

  const importConnectLink = useCallback(async (
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
          routerRef.current.replace({
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
  }, []);

  // Auto-connect on app start.
  useEffect(() => {
    onBootstrapResolved(false);

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
    const onDisconnected = (data: any) => {
      const decision = decideDisconnectLifecycle(data?.reason);
      dispatch({
        type: "SET_SERVER_CONNECTION_STATE",
        serverId: data.serverId,
        connectionState: decision.connectionState,
      });
      // Transient transport close (background suspension) must retain Brain/Work
      // caches so foreground resume does not flash offline empty states.
      if (!decision.clearServerCaches) {
        return;
      }
      workDispatch({
        type: "REMOVE_SERVER",
        serverId: data.serverId,
      });
      brainDispatch({
        type: "REMOVE_SERVER",
        serverId: data.serverId,
      });
      calendarDispatch({ type: "REMOVE_SERVER", serverId: data.serverId });
    };
    const onConnectionIssue = (data: any) =>
      dispatch({
        type: "SET_SERVER_CONNECTION_ISSUE",
        serverId: data.serverId,
        issue: data.issue || null,
      });

    const onWorkItemsSnapshot = (data: any) =>
      workDispatch({
        type: "WORK_ITEMS_SNAPSHOT",
        serverId: data.serverId,
        serverName: data.serverName,
        serverUrl: data.serverUrl,
        workItems: data.work_items || [],
      });
    const onWorkItemChanged = (data: any) => {
      if (!data.work_item) {
        return;
      }
      workDispatch({
        type: "WORK_ITEM_CHANGED",
        serverId: data.serverId,
        serverName: data.serverName,
        serverUrl: data.serverUrl,
        workItem: data.work_item,
      });
    };
    const onWorkItemDeleted = (data: any) =>
      workDispatch({
        type: "WORK_ITEM_DELETED",
        serverId: data.serverId,
        id: data.id,
        path: data.path,
      });
    const onBrainSnapshot = (data: any) =>
      brainDispatch({
        type: "BRAIN_SNAPSHOT",
        serverId: data.serverId,
        serverName: data.serverName,
        serverUrl: data.serverUrl,
        brain: data.brain || {},
      });
    const onCalendarSnapshot = (data: any) => {
      const items = data.calendar_items || [];
      calendarDispatch({
        type: "CALENDAR_SNAPSHOT",
        serverId: data.serverId,
        serverName: data.serverName,
        serverUrl: data.serverUrl,
        items,
      });
      void syncCalendarNotifications(data.serverId, items).catch((error) => {
        console.log("Failed to sync Calendar notifications:", error);
      });
    };
    const onCalendarChanged = (data: any) => {
      if (!data.calendar_item) return;
      calendarDispatch({
        type: "CALENDAR_CHANGED",
        serverId: data.serverId,
        serverName: data.serverName,
        serverUrl: data.serverUrl,
        item: data.calendar_item,
      });
      wsClient.listCalendarItems(data.serverId);
      wsClient.requestBrainSnapshot(data.serverId);
    };
    const onConnectedFetchWork = createConnectedReadRefreshHandler(wsClient);

    wsClient.on("agent_session_list", onAgentSessionList);
    wsClient.on("agent_session_created", onAgentSessionUpsert);
    wsClient.on("agent_session_updated", onAgentSessionUpsert);
    wsClient.on("agent_session_archived", onAgentSessionArchived);
    wsClient.on("connecting", onConnecting);
    wsClient.on("connected", onConnected);
    wsClient.on("disconnected", onDisconnected);
    wsClient.on("connection_issue", onConnectionIssue);
    wsClient.on("work_items_snapshot", onWorkItemsSnapshot);
    wsClient.on("work_item_changed", onWorkItemChanged);
    wsClient.on("work_item_deleted", onWorkItemDeleted);
    wsClient.on("brain_snapshot", onBrainSnapshot);
    wsClient.on("calendar_items_snapshot", onCalendarSnapshot);
    wsClient.on("calendar_item_changed", onCalendarChanged);
    wsClient.on("connected", onConnectedFetchWork);

    (async () => {
      try {
        const initialURL = await Linking.getInitialURL();
        const imported = await importConnectLink(initialURL);
        if (imported) {
          return;
        }

        const [onboarded, servers, disabledServerIds] = await Promise.all([
          isOnboarded(),
          getServers(),
          getDisabledServerIds(),
        ]);
        if (!onboarded && servers.length === 0) {
          routerRef.current.replace("/onboarding");
          return;
        }
        if (!onboarded) {
          await markOnboarded();
        }
        if (
          servers.length > 0 &&
          rootSegmentRef.current === "onboarding"
        ) {
          routerRef.current.replace("/");
        }

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
        onBootstrapResolved(true);
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
      wsClient.off("work_items_snapshot", onWorkItemsSnapshot);
      wsClient.off("work_item_changed", onWorkItemChanged);
      wsClient.off("work_item_deleted", onWorkItemDeleted);
      wsClient.off("brain_snapshot", onBrainSnapshot);
      wsClient.off("calendar_items_snapshot", onCalendarSnapshot);
      wsClient.off("calendar_item_changed", onCalendarChanged);
      wsClient.off("connected", onConnectedFetchWork);
    };
  }, [
    brainDispatch,
    calendarDispatch,
    dispatch,
    importConnectLink,
    onBootstrapResolved,
    workDispatch,
  ]);

  useEffect(() => {
    const subscription = Linking.addEventListener("url", (event) => {
      void importConnectLink(event.url);
    });

    return () => {
      subscription.remove();
    };
  }, [importConnectLink]);

  return null;
});

const LatencySampler = memo(function LatencySampler() {
  const dispatch = useAgentDispatch();
  const serverConnections = useAgentServerConnections();

  useEffect(() => {
    let cancelled = false;

    const refreshServerLatency = async () => {
      const servers = await getServers();
      if (cancelled) {
        return;
      }

      const connectedServers = servers.filter(
        (server) => serverConnections[server.id] === "connected",
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
  }, [dispatch, serverConnections]);

  return null;
});

const NotificationObserver = memo(function NotificationObserver() {
  const router = useRouter();
  const routerRef = useRef(router);
  const notificationListener = useRef<Notifications.EventSubscription | null>(
    null,
  );
  const responseListener = useRef<Notifications.EventSubscription | null>(null);
  const appStateRef = useRef(AppState.currentState);

  useEffect(() => {
    routerRef.current = router;
  }, [router]);

  useEffect(() => {
    const subscription = AppState.addEventListener("change", (nextState) => {
      const previous = appStateRef.current;
      appStateRef.current = nextState;
      if (nextState !== "active") {
        wsClient.clearActiveAgentsExcept(null);
        return;
      }
      // Foreground resume: skip reconnect backoff and silently restore transport.
      if (previous !== "active") {
        wsClient.resumeReconnects();
      }
    });

    return () => {
      subscription.remove();
    };
  }, []);

  // Register permissions and push token.
  useEffect(() => {
    let cancelled = false;
    let onConnected: ((data: any) => void) | null = null;

    (async () => {
      const token = await registerForPushNotificationsAsync();
      if (cancelled) {
        return;
      }

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
        const destination = resolveNotificationDestination(data);
        switch (destination?.kind) {
          case "terminal":
            routerRef.current.push({
              pathname: "/terminal/[id]",
              params: {
                id: destination.agentId,
                serverId: destination.serverId,
              },
            });
            break;
          case "inbox":
            routerRef.current.push("/list");
            break;
          case "calendar":
            routerRef.current.push({
              pathname: "/calendar",
              params: {
                id: destination.calendarId,
                serverId: destination.serverId,
              },
            });
            break;
          case "brain":
            routerRef.current.push({
              pathname: "/",
              params: {
                brainThreadId: destination.brainThreadId,
                brainMessageId: destination.brainMessageId,
                serverId: destination.serverId,
              },
            });
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
  }, []);

  return null;
});

interface AppNavigatorProps {
  bootstrapResolved: boolean;
}

const AppNavigator = memo(function AppNavigator({
  bootstrapResolved,
}: AppNavigatorProps) {
  const { colors } = useAppTheme();

  if (!bootstrapResolved) {
    return null;
  }

  return (
    <Stack
      screenOptions={{
        headerStyle: { backgroundColor: colors.bgPrimary },
        headerTintColor: colors.textPrimary,
        contentStyle: { backgroundColor: colors.bgPrimary },
        animation: "slide_from_right",
      }}
    >
      <Stack.Screen name="(primary)" options={{ headerShown: false }} />
      <Stack.Screen
        name="calendar"
        options={{
          title: "Calendar",
          headerLeft: () => <SecondaryBackButton />,
        }}
      />
      <Stack.Screen
        name="skills"
        options={{
          title: "Skills",
          headerLeft: () => <SecondaryBackButton />,
        }}
      />
      <Stack.Screen
        name="stats"
        options={{
          title: "Stats",
          headerLeft: () => <SecondaryBackButton />,
        }}
      />
      <Stack.Screen
        name="settings"
        options={{
          title: "Settings",
          headerLeft: () => <SecondaryBackButton />,
        }}
      />
      <Stack.Screen
        name="terminal/[id]"
        options={{ headerShown: false, animation: "none" }}
      />
      <Stack.Screen name="work/[id]" options={{ headerShown: false }} />
      <Stack.Screen
        name="onboarding"
        options={{ headerShown: false, presentation: "modal" }}
      />
      <Stack.Screen name="screenshot-demo" options={{ headerShown: false }} />
    </Stack>
  );
});

AppNavigator.displayName = "AppNavigator";

function SecondaryBackButton() {
  const router = useRouter();
  const navigation = useNavigation();
  const { colors } = useAppTheme();
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel="Back"
      hitSlop={8}
      onPress={() => {
        if (navigation.canGoBack()) {
          router.back();
          return;
        }
        router.replace("/");
      }}
      style={{
        width: 44,
        minHeight: 44,
        alignItems: "flex-start",
        justifyContent: "center",
      }}
    >
      <Ionicons name="chevron-back" size={23} color={colors.textPrimary} />
    </Pressable>
  );
}

function ThemedStatusBar() {
  const { isLight } = useAppTheme();
  return <StatusBar style={isLight ? "dark" : "light"} />;
}

export default function RootLayout() {
  const [fontsLoaded, fontError] = useFonts({
    "SourceHanSansSC-Regular": require("../assets/fonts/SourceHanSansSC-Regular.otf"),
    "SourceHanSansSC-Medium": require("../assets/fonts/SourceHanSansSC-Medium.otf"),
    "MapleMono-CN-Regular": require("../assets/fonts/MapleMono-CN-Regular.ttf"),
    "MapleMono-CN-SemiBold": require("../assets/fonts/MapleMono-CN-SemiBold.ttf"),
    "SarasaGothicSC-Regular": require("../assets/fonts/SarasaGothicSC-Regular.ttf"),
    "SarasaGothicSC-Bold": require("../assets/fonts/SarasaGothicSC-Bold.ttf"),
    "SarasaTermSC-Regular": require("../assets/fonts/SarasaTermSC-Regular.ttf"),
    "SarasaTermSC-Bold": require("../assets/fonts/SarasaTermSC-Bold.ttf"),
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
      <ThemeProvider>
        <KeyboardProvider statusBarTranslucent navigationBarTranslucent>
          <AgentProvider>
            <BrainProvider>
              <WorkProvider>
                <CalendarProvider>
                  <SafeAreaProvider>
                    <ThemedStatusBar />
                    <AppRuntime />
                  </SafeAreaProvider>
                </CalendarProvider>
              </WorkProvider>
            </BrainProvider>
          </AgentProvider>
        </KeyboardProvider>
      </ThemeProvider>
    </GestureHandlerRootView>
  );
}
