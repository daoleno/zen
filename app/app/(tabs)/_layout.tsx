import { Text as RNText } from 'react-native';
import { Tabs } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { Colors, Typography } from '../../constants/tokens';

export default function TabLayout() {
  const insets = useSafeAreaInsets();
  const tabBarBottom = Math.max(insets.bottom, 8);

  return (
    <Tabs
      screenOptions={{
        tabBarStyle: {
          backgroundColor: Colors.bgPrimary,
          borderTopColor: 'rgba(255,255,255,0.04)',
          borderTopWidth: 0.5,
          height: 52 + tabBarBottom,
          paddingBottom: tabBarBottom,
          paddingTop: 4,
        },
        tabBarActiveTintColor: Colors.textPrimary,
        tabBarInactiveTintColor: 'rgba(255,255,255,0.25)',
        tabBarShowLabel: false,
        headerShown: false,
      }}
    >
      <Tabs.Screen
        name="index"
        options={{
          title: 'Agents',
          tabBarIcon: ({ color }) => <TabDot color={color} />,
        }}
      />
      <Tabs.Screen
        name="stats"
        options={{
          title: 'Stats',
          tabBarIcon: ({ color }) => <RNText style={{ fontSize: 20, color, opacity: 0.8, lineHeight: 22 }}>{'∷'}</RNText>,
        }}
      />
      <Tabs.Screen
        name="settings"
        options={{
          title: 'Settings',
          tabBarIcon: ({ color }) => <RNText style={{ fontSize: 16, color, opacity: 0.8, lineHeight: 22 }}>{'///'}</RNText>,
        }}
      />
    </Tabs>
  );
}

function TabDot({ color }: { color: string }) {
  return (
    <RNText style={{ fontSize: 14, color, lineHeight: 22 }}>{'●'}</RNText>
  );
}
