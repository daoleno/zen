import { Tabs } from 'expo-router';
import { Colors } from '../../constants/tokens';

export default function TabLayout() {
  return (
    <Tabs
      screenOptions={{
        tabBarStyle: {
          backgroundColor: Colors.bgPrimary,
          borderTopColor: Colors.bgSurface,
          height: 70,
          paddingBottom: 10,
        },
        tabBarActiveTintColor: Colors.textPrimary,
        tabBarInactiveTintColor: Colors.textSecondary,
        headerShown: false,
      }}
    >
      <Tabs.Screen
        name="index"
        options={{
          title: 'Inbox',
          tabBarIcon: ({ color }) => <TabIcon name="●" color={color} />,
        }}
      />
      <Tabs.Screen
        name="settings"
        options={{
          title: 'Settings',
          tabBarIcon: ({ color }) => <TabIcon name="⚙" color={color} />,
        }}
      />
    </Tabs>
  );
}

function TabIcon({ name, color }: { name: string; color: string }) {
  return (
    <RNText style={{ fontSize: 22, color }}>{name}</RNText>
  );
}

import { Text as RNText } from 'react-native';
