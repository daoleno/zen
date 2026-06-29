import type { ColorValue } from 'react-native';
import type { ComponentProps } from 'react';
import { Tabs } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import {
  ZenFloatingTabBar,
  type ZenFloatingTabBarProps,
} from '../../components/navigation/ZenFloatingTabBar';

export default function TabLayout() {
  return (
    <Tabs
      tabBar={(props) => (
        <ZenFloatingTabBar {...(props as unknown as ZenFloatingTabBarProps)} />
      )}
      screenOptions={{
        headerShown: false,
        tabBarShowLabel: false,
      }}
    >
      <Tabs.Screen
        name="index"
        options={{
          title: 'Agents',
          tabBarIcon: ({ color, focused }) => (
            <TabIcon
              icon={focused ? 'chatbubbles' : 'chatbubbles-outline'}
              color={color}
            />
          ),
        }}
      />
      <Tabs.Screen
        name="work"
        options={{
          title: 'Brain',
          tabBarIcon: ({ color, focused }) => (
            <TabIcon
              icon={focused ? 'hardware-chip' : 'hardware-chip-outline'}
              color={color}
            />
          ),
        }}
      />
      <Tabs.Screen
        name="stats"
        options={{
          title: 'Stats',
          tabBarIcon: ({ color, focused }) => (
            <TabIcon
              icon={focused ? 'bar-chart' : 'bar-chart-outline'}
              color={color}
            />
          ),
        }}
      />
      <Tabs.Screen
        name="settings"
        options={{
          title: 'Settings',
          tabBarIcon: ({ color, focused }) => (
            <TabIcon
              icon={focused ? 'settings' : 'settings-outline'}
              color={color}
            />
          ),
        }}
      />
    </Tabs>
  );
}

function TabIcon({
  icon,
  color,
}: {
  icon: ComponentProps<typeof Ionicons>['name'];
  color: ColorValue;
}) {
  return <Ionicons name={icon} size={22} color={color} />;
}