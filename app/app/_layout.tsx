import { useEffect, useRef } from 'react';
import { Platform } from 'react-native';
import { Stack, useRouter, useSegments } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import * as Notifications from 'expo-notifications';
import * as Device from 'expo-device';
import Constants from 'expo-constants';
import { AgentProvider, useAgents } from '../store/agents';
import { Colors } from '../constants/tokens';
import { wsClient } from '../services/websocket';
import { getServerUrl, isOnboarded } from '../services/storage';

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

function AppContent() {
  const router = useRouter();
  const segments = useSegments();
  const { dispatch } = useAgents();
  const notificationListener = useRef<Notifications.EventSubscription>();
  const responseListener = useRef<Notifications.EventSubscription>();

  // Auto-connect on app start.
  useEffect(() => {
    (async () => {
      const onboarded = await isOnboarded();
      if (!onboarded && segments[0] !== 'onboarding') {
        router.replace('/onboarding');
        return;
      }

      const url = await getServerUrl();
      if (url) {
        wsClient.connect(url);

        const onAgentList = (data: any) => dispatch({ type: 'SET_AGENTS', agents: data.agents || [] });
        const onStateChange = (data: any) => dispatch({ type: 'STATE_CHANGE', agent_id: data.agent_id, old: data.old, new_state: data.new });
        const onOutput = (data: any) => dispatch({ type: 'UPDATE_OUTPUT', agent_id: data.agent_id, lines: data.lines || [] });
        const onConnected = () => dispatch({ type: 'SET_CONNECTED', connected: true });
        const onDisconnected = () => dispatch({ type: 'SET_CONNECTED', connected: false });

        wsClient.on('agent_list', onAgentList);
        wsClient.on('agent_state_change', onStateChange);
        wsClient.on('agent_output', onOutput);
        wsClient.on('connected', onConnected);
        wsClient.on('disconnected', onDisconnected);
      }
    })();
  }, []);

  // Register push token and send to daemon.
  useEffect(() => {
    registerForPushNotificationsAsync().then(token => {
      if (token) {
        wsClient.on('connected', () => {
          wsClient.send({ type: 'register_push', push_token: token });
        });
      }
    });

    notificationListener.current = Notifications.addNotificationReceivedListener(notification => {
      console.log('Notification received:', notification);
    });

    responseListener.current = Notifications.addNotificationResponseReceivedListener(response => {
      const data = response.notification.request.content.data;
      if (data?.agent_id && data?.screen === 'terminal') {
        router.push(`/terminal/${data.agent_id}`);
      }
    });

    return () => {
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
      <Stack.Screen name="terminal/[id]" options={{ headerShown: false }} />
      <Stack.Screen name="onboarding" options={{ headerShown: false, presentation: 'modal' }} />
    </Stack>
  );
}

export default function RootLayout() {
  return (
    <AgentProvider>
      <StatusBar style="light" />
      <AppContent />
    </AgentProvider>
  );
}
