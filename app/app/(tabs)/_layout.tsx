import { StyleSheet, View } from 'react-native';
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
          position: 'absolute',
          left: 0,
          right: 0,
          bottom: 0,
          height: 56 + tabBarBottom,
          paddingBottom: tabBarBottom,
          paddingTop: 6,
          paddingHorizontal: 4,
          backgroundColor: colors.bgElevated,
          borderTopColor: colors.borderSubtle,
          borderTopWidth: StyleSheet.hairlineWidth,
          elevation: 0,
          shadowOpacity: 0,
        },
        tabBarActiveTintColor: colors.accent,
        tabBarInactiveTintColor: colors.textTertiary,
        tabBarShowLabel: true,
        tabBarLabelStyle: {
          fontFamily: Typography.uiFontMedium,
          fontSize: 10,
          lineHeight: 12,
          marginTop: 2,
        },
        tabBarItemStyle: {
          borderRadius: 12,
          minHeight: 44,
          paddingVertical: 2,
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
            <TabSymbol icon={focused ? 'chatbubbles' : 'chatbubbles-outline'} color={color} focused={focused} />
          ),
        }}
      />
      <Tabs.Screen
        name="work"
        options={{
          title: 'Brain',
          tabBarIcon: ({ color, focused }) => (
            <TabSymbol icon={focused ? 'hardware-chip' : 'hardware-chip-outline'} color={color} focused={focused} />
          ),
        }}
      />
      <Tabs.Screen
        name="stats"
        options={{
          title: 'Stats',
          tabBarIcon: ({ color, focused }) => (
            <TabSymbol icon={focused ? 'bar-chart' : 'bar-chart-outline'} color={color} focused={focused} />
          ),
        }}
      />
      <Tabs.Screen
        name="settings"
        options={{
          title: 'Settings',
          tabBarIcon: ({ color, focused }) => (
            <TabSymbol icon={focused ? 'settings' : 'settings-outline'} color={color} focused={focused} />
          ),
        }}
      />
    </Tabs>
  );
}

function TabSymbol({
  icon,
  color,
  focused,
}: {
  icon: ComponentProps<typeof Ionicons>['name'];
  color: ColorValue;
  focused: boolean;
}) {
  const colors = useAppColors();
  return (
    <View
      style={[
        styles.tabIconWrap,
        {
          backgroundColor: focused ? colors.accentSoft : 'transparent',
        },
      ]}
    >
      <Ionicons name={icon} size={20} color={color} />
    </View>
  );
}

const styles = StyleSheet.create({
  tabIconWrap: {
    minWidth: 52,
    height: 30,
    borderRadius: 15,
    alignItems: 'center',
    justifyContent: 'center',
  },
});