import { useEffect, useRef } from 'react';
import { AppState, AppStateStatus, Platform } from 'react-native';
import { Stack, useRouter, useSegments } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useFonts } from 'expo-font';
import * as Notifications from 'expo-notifications';
import * as Device from 'expo-device';
import Constants from 'expo-constants';
import { SafeAreaProvider } from 'react-native-safe-area-context';
import { Agent, AgentProvider, useAgents } from '../store/agents';
import { Colors } from '../constants/tokens';
import { wsClient } from '../services/websocket';
import { getServers, isOnboarded } from '../services/storage';
import { parseSessionKey } from '../services/sessionKeys';

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

  try {
    const projectId =
      Constants?.expoConfig?.extra?.eas?.projectId ?? Constants?.easConfig?.projectId;
    if (!projectId) {
      throw new Error('Project ID not found');
    }
    const token = (await Notifications.getExpoPushTokenAsync({ projectId })).data;
    return token;
  } catch (e) {
    console.log('Failed to get push token:', e);
    return;
  }
}

function buildNotificationContent(agent: Agent): Notifications.NotificationContentInput | null {
  const bodyPrefix = agent.serverName ? `${agent.serverName} · ` : '';

  switch (agent.status) {
    case 'blocked':
      return {
        title: `${agent.name} needs you`,
        body: `${bodyPrefix}${agent.summary || 'Waiting for input'}`,
        data: { agent_id: agent.id, server_id: agent.serverId, screen: 'terminal' },
        sound: 'default',
      };
    case 'failed':
      return {
        title: `${agent.name} failed`,
        body: `${bodyPrefix}${agent.summary || 'The session hit an error'}`,
        data: { agent_id: agent.id, server_id: agent.serverId, screen: 'terminal' },
        sound: 'default',
      };
    case 'done':
      return {
        title: `${agent.name} done`,
        body: `${bodyPrefix}${agent.summary || 'Task completed successfully'}`,
        data: { agent_id: agent.id, server_id: agent.serverId, screen: 'terminal' },
        sound: 'default',
      };
    default:
      return null;
  }
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

    wsClient.on('agent_list', onAgentList);
    wsClient.on('agent_state_change', onStateChange);
    wsClient.on('agent_output', onOutput);
    wsClient.on('connecting', onConnecting);
    wsClient.on('connected', onConnected);
    wsClient.on('disconnected', onDisconnected);

    (async () => {
      const onboarded = await isOnboarded();
      if (!onboarded && segments[0] !== 'onboarding') {
        router.replace('/onboarding');
        return;
      }

      const servers = await getServers();
      servers.forEach(server => {
        wsClient.connectServer(server);
      });

      const selected = state.selectedAgentKey ? parseSessionKey(state.selectedAgentKey) : null;
      wsClient.clearActiveAgentsExcept(selected);
    })();

    return () => {
      wsClient.off('agent_list', onAgentList);
      wsClient.off('agent_state_change', onStateChange);
      wsClient.off('agent_output', onOutput);
      wsClient.off('connecting', onConnecting);
      wsClient.off('connected', onConnected);
      wsClient.off('disconnected', onDisconnected);
      wsClient.disconnectAll();
    };
  }, []);

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
      console.log('Notification received:', notification);
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
  const [fontsLoaded] = useFonts({
    'SourceHanSansSC-Regular': require('../assets/fonts/SourceHanSansSC-Regular.otf'),
    'SourceHanSansSC-Medium': require('../assets/fonts/SourceHanSansSC-Medium.otf'),
    'MapleMono-CN-Regular': require('../assets/fonts/MapleMono-CN-Regular.ttf'),
    'MapleMono-CN-SemiBold': require('../assets/fonts/MapleMono-CN-SemiBold.ttf'),
  });

  if (!fontsLoaded) {
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
