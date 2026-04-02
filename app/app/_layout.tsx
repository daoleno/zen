import { useEffect, useRef } from 'react';
import { AppState, AppStateStatus, Platform } from 'react-native';
import { Stack, useRouter, useSegments } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useFonts } from 'expo-font';
import * as Linking from 'expo-linking';
import * as Notifications from 'expo-notifications';
import * as Device from 'expo-device';
import Constants from 'expo-constants';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { Agent, AgentProvider, useAgents } from '../store/agents';
import { Colors } from '../constants/tokens';
import { wsClient } from '../services/websocket';
import { getServers, isOnboarded } from '../services/storage';
import { parseSessionKey } from '../services/sessionKeys';
import { importConnection } from '../services/importConnection';

Notifications.setNotificationHandler({
  handleNotification: async () => ({
    shouldPlaySound: true,
    shouldSetBadge: false,
    shouldShowBanner: true,
    shouldShowList: true,
  }),
});

async function registerForPushNotificationsAsync(): Promise<string | undefined> {
  if (Platform.OS === 'android') {
    await Notifications.setNotificationChannelAsync('zen-agents', {
      name: 'Agent Alerts',
      importance: Notifications.AndroidImportance.MAX,
      vibrationPattern: [0, 250, 250, 250],
    });
  }

  if (!Device.isDevice) {
    console.log('Push notifications require a physical device');
    return;
  }

  const { status: existingStatus } = await Notifications.getPermissionsAsync();
  let finalStatus = existingStatus;
  if (existingStatus !== 'granted') {
    const { status } = await Notifications.requestPermissionsAsync();
    finalStatus = status;
  }
  if (finalStatus !== 'granted') return;

  const projectId =
    Constants?.expoConfig?.extra?.eas?.projectId
    ?? Constants?.easConfig?.projectId;

  if (!projectId) {
    console.log('Push notifications disabled: Expo project ID is not configured.');
    return;
  }

  try {
    const token = (await Notifications.getExpoPushTokenAsync({ projectId })).data;
    return token;
  } catch (e) {
    console.log('Failed to get push token:', e);
    return;
  }
}

function buildNotificationContent(agent: Agent): Notifications.NotificationContentInput | null {
  const label = formatNotificationAgentLabel(agent);
  const context = [agent.project, agent.serverName].filter(Boolean).join(' · ');
  const summary = normalizeNotificationSummary(agent.summary);

  switch (agent.status) {
    case 'blocked':
      return {
        title: label ? `Input needed · ${label}` : 'Input needed',
        body: buildNotificationBody(context, summary, 'Open zen to respond.'),
        data: { agent_id: agent.id, server_id: agent.serverId, screen: 'terminal' },
        sound: 'default',
      };
    case 'failed':
      return {
        title: label ? `Task failed · ${label}` : 'Task failed',
        body: buildNotificationBody(context, summary, 'Open zen to inspect the last output.'),
        data: { agent_id: agent.id, server_id: agent.serverId, screen: 'terminal' },
        sound: 'default',
      };
    case 'done':
      return {
        title: label ? `Finished · ${label}` : 'Finished',
        body: buildNotificationBody(context, summary, 'Session finished.'),
        data: { agent_id: agent.id, server_id: agent.serverId, screen: 'terminal' },
        sound: 'default',
      };
    default:
      return null;
  }
}

function formatNotificationAgentLabel(agent: Agent): string {
  const raw = agent.project?.trim() || agent.name?.trim() || agent.id;
  if (!raw) {
    return '';
  }

  const withoutSessionSuffix = raw.replace(/\s+\([^)]+\)\s*$/, '');
  const parts = withoutSessionSuffix.split(/[\\/]/).filter(Boolean);
  return parts[parts.length - 1] || withoutSessionSuffix;
}

function normalizeNotificationSummary(summary: string | undefined): string {
  if (!summary) {
    return '';
  }

  const collapsed = summary
    .replace(/^\d{4}[/-]\d{2}[/-]\d{2}[ T]\d{2}:\d{2}:\d{2}\s*/, '')
    .replace(/\s+/g, ' ')
    .trim();

  if (collapsed.length <= 110) {
    return collapsed;
  }

  return `${collapsed.slice(0, 107)}...`;
}

function buildNotificationBody(context: string, summary: string, fallback: string): string {
  const parts = [context, summary].filter(Boolean);
  return parts.length > 0 ? parts.join(' · ') : fallback;
}

function AppContent() {
  const router = useRouter();
  const segments = useSegments();
  const { state, dispatch } = useAgents();
  const notificationListener = useRef<Notifications.EventSubscription | null>(null);
  const responseListener = useRef<Notifications.EventSubscription | null>(null);
  const appStateRef = useRef<AppStateStatus>(AppState.currentState);
  const notificationsEnabledRef = useRef(false);
  const previousAgentStatesRef = useRef(new Map<string, Agent['status']>());
  const handledConnectLinksRef = useRef(new Set<string>());

  const importConnectLink = async (rawValue: string | null | undefined): Promise<boolean> => {
    const trimmed = rawValue?.trim() || '';
    if (!trimmed || handledConnectLinksRef.current.has(trimmed)) {
      return false;
    }

    handledConnectLinksRef.current.add(trimmed);

    try {
      const savedServer = await importConnection(trimmed, {
        onImported: () => {
          router.replace({
            pathname: '/settings',
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
      console.log('Failed to import connect link:', error);
      return false;
    }
  };

  // Auto-connect on app start.
  useEffect(() => {
    const onAgentList = (data: any) =>
      dispatch({
        type: 'UPSERT_SERVER_AGENTS',
        serverId: data.serverId,
        serverName: data.serverName,
        serverUrl: data.serverUrl,
        agents: data.agents || [],
      });
    const onStateChange = (data: any) =>
      dispatch({
        type: 'STATE_CHANGE',
        serverId: data.serverId,
        agent_id: data.agent_id,
        old: data.old,
        new_state: data.new,
      });
    const onOutput = (data: any) =>
      dispatch({
        type: 'UPDATE_OUTPUT',
        serverId: data.serverId,
        agent_id: data.agent_id,
        lines: data.lines || [],
      });
    const onConnecting = (data: any) =>
      dispatch({
        type: 'SET_SERVER_CONNECTION_STATE',
        serverId: data.serverId,
        connectionState: 'connecting',
      });
    const onConnected = (data: any) =>
      dispatch({
        type: 'SET_SERVER_CONNECTION_STATE',
        serverId: data.serverId,
        connectionState: 'connected',
      });
    const onDisconnected = (data: any) =>
      dispatch({
        type: 'SET_SERVER_CONNECTION_STATE',
        serverId: data.serverId,
        connectionState: 'offline',
      });
    const onConnectionIssue = (data: any) =>
      dispatch({
        type: 'SET_SERVER_CONNECTION_ISSUE',
        serverId: data.serverId,
        issue: data.issue || null,
      });

    wsClient.on('agent_list', onAgentList);
    wsClient.on('agent_state_change', onStateChange);
    wsClient.on('agent_output', onOutput);
    wsClient.on('connecting', onConnecting);
    wsClient.on('connected', onConnected);
    wsClient.on('disconnected', onDisconnected);
    wsClient.on('connection_issue', onConnectionIssue);

    (async () => {
      try {
        const initialURL = await Linking.getInitialURL();
        const imported = await importConnectLink(initialURL);
        if (imported) {
          return;
        }

        const onboarded = await isOnboarded();
        if (!onboarded && segments[0] !== 'onboarding') {
          router.replace('/onboarding');
          return;
        }

        const servers = await getServers();
        servers.forEach(server => {
          wsClient.connectServer(server);
        });
      } catch (error) {
        console.log('Failed to bootstrap app:', error);
      } finally {
        const selected = state.selectedAgentKey ? parseSessionKey(state.selectedAgentKey) : null;
        wsClient.clearActiveAgentsExcept(selected);
      }
    })();

    return () => {
      wsClient.off('agent_list', onAgentList);
      wsClient.off('agent_state_change', onStateChange);
      wsClient.off('agent_output', onOutput);
      wsClient.off('connecting', onConnecting);
      wsClient.off('connected', onConnected);
      wsClient.off('disconnected', onDisconnected);
      wsClient.off('connection_issue', onConnectionIssue);
      wsClient.disconnectAll();
    };
  }, []);

  useEffect(() => {
    const subscription = Linking.addEventListener('url', event => {
      void importConnectLink(event.url);
    });

    return () => {
      subscription.remove();
    };
  }, [router]);

  useEffect(() => {
    const syncActiveAgent = (appState: AppStateStatus) => {
      if (appState !== 'active') {
        wsClient.clearActiveAgentsExcept(null);
        return;
      }

      const selected = state.selectedAgentKey ? parseSessionKey(state.selectedAgentKey) : null;
      wsClient.clearActiveAgentsExcept(selected);
    };

    const subscription = AppState.addEventListener('change', nextState => {
      appStateRef.current = nextState;
      syncActiveAgent(nextState);
    });

    return () => {
      subscription.remove();
    };
  }, [state.selectedAgentKey]);

  useEffect(() => {
    if (appStateRef.current !== 'active') {
      wsClient.clearActiveAgentsExcept(null);
      return;
    }
    const selected = state.selectedAgentKey ? parseSessionKey(state.selectedAgentKey) : null;
    wsClient.clearActiveAgentsExcept(selected);
  }, [state.selectedAgentKey]);

  useEffect(() => {
    const nextAgentStates = new Map(state.agents.map(agent => [agent.key, agent.status]));
    const previousAgentStates = previousAgentStatesRef.current;

    if (previousAgentStates.size === 0) {
      previousAgentStatesRef.current = nextAgentStates;
      return;
    }

    if (!notificationsEnabledRef.current || appStateRef.current !== 'active') {
      previousAgentStatesRef.current = nextAgentStates;
      return;
    }

    for (const agent of state.agents) {
      const previousState = previousAgentStates.get(agent.key);
      if (!previousState || previousState === agent.status) {
        continue;
      }
      if (agent.key === state.selectedAgentKey) {
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
  }, [state.agents, state.selectedAgentKey]);

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
      notificationsEnabledRef.current = status === 'granted';

      if (!token) {
        return;
      }

      const registerPush = (serverId: string) => {
        wsClient.send(serverId, {
          type: 'register_push',
          push_token: token,
          server_ref: serverId,
        });
      };

      onConnected = (data: any) => {
        registerPush(data.serverId);
      };
      wsClient.on('connected', onConnected);

      for (const serverId of wsClient.connectedServerIds()) {
        registerPush(serverId);
      }
    })();

    notificationListener.current = Notifications.addNotificationReceivedListener(notification => {
      const content = notification.request.content;
      console.log('Notification received:', {
        title: content.title,
        body: content.body,
        data: content.data,
      });
    });

    responseListener.current = Notifications.addNotificationResponseReceivedListener(response => {
      const data = response.notification.request.content.data;
      const agentId = typeof data?.agent_id === 'string' ? data.agent_id : null;
      const serverId = typeof data?.server_id === 'string' ? data.server_id : null;

      if (agentId && serverId) {
        router.push({
          pathname: '/terminal/[id]',
          params: { id: agentId, serverId },
        });
        return;
      }
      if (data?.screen === 'inbox') {
        router.push('/');
      }
    });

    return () => {
      cancelled = true;
      if (onConnected) {
        wsClient.off('connected', onConnected);
      }
      notificationListener.current?.remove();
      responseListener.current?.remove();
    };
  }, [router]);

  return (
    <Stack
      screenOptions={{
        headerStyle: { backgroundColor: Colors.bgPrimary },
        headerTintColor: Colors.textPrimary,
        contentStyle: { backgroundColor: Colors.bgPrimary },
        animation: 'slide_from_right',
      }}
    >
      <Stack.Screen name="(tabs)" options={{ headerShown: false }} />
      <Stack.Screen name="terminal/[id]" options={{ headerShown: false, animation: 'none' }} />
      <Stack.Screen name="onboarding" options={{ headerShown: false, presentation: 'modal' }} />
    </Stack>
  );
}

export default function RootLayout() {
  const [fontsLoaded, fontError] = useFonts({
    'SourceHanSansSC-Regular': require('../assets/fonts/SourceHanSansSC-Regular.otf'),
    'SourceHanSansSC-Medium': require('../assets/fonts/SourceHanSansSC-Medium.otf'),
    'MapleMono-CN-Regular': require('../assets/fonts/MapleMono-CN-Regular.ttf'),
    'MapleMono-CN-SemiBold': require('../assets/fonts/MapleMono-CN-SemiBold.ttf'),
  });

  useEffect(() => {
    if (fontError) {
      console.log('Failed to load fonts:', fontError);
    }
  }, [fontError]);

  if (!fontsLoaded && !fontError) {
    return null;
  }

  return (
    <AgentProvider>
      <SafeAreaProvider>
        <StatusBar style="light" />
        <AppContent />
      </SafeAreaProvider>
    </AgentProvider>
  );
}
