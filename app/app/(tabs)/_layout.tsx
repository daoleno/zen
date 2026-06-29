import { StyleSheet, View } from 'react-native';
import type { ColorValue } from 'react-native';
import type { ComponentProps } from 'react';
import { Tabs } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Radii, Typography, shadow, useAppColors } from '../../constants/tokens';

const TAB_BAR_HEIGHT = 58;
const TAB_BAR_HORIZONTAL_INSET = 14;
const TAB_BAR_BOTTOM_GAP = 10;

export default function TabLayout() {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  const bottom = Math.max(insets.bottom, TAB_BAR_BOTTOM_GAP);

  return (
    <Tabs
      screenOptions={{
        tabBarBackground: () => (
          <View
            style={{
              flex: 1,
              borderRadius: Radii.pill,
              backgroundColor: colors.bgElevated,
              borderWidth: StyleSheet.hairlineWidth,
              borderColor: colors.borderSubtle,
            }}
          />
        ),
        tabBarStyle: {
          position: 'absolute',
          left: TAB_BAR_HORIZONTAL_INSET,
          right: TAB_BAR_HORIZONTAL_INSET,
          bottom,
          height: TAB_BAR_HEIGHT,
          borderRadius: Radii.pill,
          backgroundColor: 'transparent',
          borderTopWidth: 0,
          paddingTop: 6,
          paddingBottom: 6,
          ...shadow('float', colors.shadowColor),
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