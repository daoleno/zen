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
        tabBarLabelStyle: {
          fontFamily: Typography.uiFont,
          fontSize: 11,
          letterSpacing: 0.3,
        },
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
          tabBarIcon: ({ color }) => <RNText style={{ fontSize: 10, color, opacity: 0.8 }}>{'∷'}</RNText>,
        }}
      />
      <Tabs.Screen
        name="settings"
        options={{
          title: 'Settings',
          tabBarIcon: ({ color }) => <RNText style={{ fontSize: 18, color, opacity: 0.8 }}>{'///'}</RNText>,
        }}
      />
    </Tabs>
  );
}

function TabDot({ color }: { color: string }) {
  return (
    <RNText style={{ fontSize: 8, color }}>{'●'}</RNText>
  );
}
