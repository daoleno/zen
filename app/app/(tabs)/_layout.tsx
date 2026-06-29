import { StyleSheet } from 'react-native';
import type { ColorValue } from 'react-native';
import type { ComponentProps } from 'react';
import { Tabs } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Typography, useAppColors } from '../../constants/tokens';

export default function TabLayout() {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  const tabBarBottom = Math.max(insets.bottom, 0);

  return (
    <Tabs
      screenOptions={{
        tabBarStyle: {
          backgroundColor: colors.bgPrimary,
          borderTopColor: colors.borderSubtle,
          borderTopWidth: StyleSheet.hairlineWidth,
          height: 50 + tabBarBottom,
          paddingBottom: tabBarBottom,
          paddingTop: 4,
          elevation: 0,
          shadowOpacity: 0,
        },
        tabBarActiveTintColor: colors.accent,
        tabBarInactiveTintColor: colors.textTertiary,
        tabBarShowLabel: true,
        tabBarLabelStyle: {
          fontFamily: Typography.uiFont,
          fontSize: 11,
          lineHeight: 13,
          marginTop: 1,
        },
        tabBarItemStyle: {
          paddingVertical: 0,
        },
        tabBarHideOnKeyboard: true,
        headerShown: false,
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
  return <Ionicons name={icon} size={24} color={color} />;
}