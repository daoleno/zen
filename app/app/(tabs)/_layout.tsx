import { useMemo } from 'react';
import type { ColorValue } from 'react-native';
import type { ComponentProps } from 'react';
import { Tabs } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { getFloatingTabBarInset } from '../../components/navigation/floatingTabBarMetrics';
import { TabScreenInsetProvider } from '../../components/navigation/TabScreenInsetContext';
import {
  ZenFloatingTabBar,
  type ZenFloatingTabBarProps,
} from '../../components/navigation/ZenFloatingTabBar';

export default function TabLayout() {
  const insets = useSafeAreaInsets();
  const tabScreenBottomInset = useMemo(
    () => getFloatingTabBarInset(insets.bottom),
    [insets.bottom],
  );

  return (
    <TabScreenInsetProvider inset={tabScreenBottomInset}>
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
    </TabScreenInsetProvider>
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