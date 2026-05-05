import { StyleSheet, Text as RNText } from 'react-native';
import { Tabs } from 'expo-router';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { useAppColors } from '../../constants/tokens';

export default function TabLayout() {
  const colors = useAppColors();
  const insets = useSafeAreaInsets();
  const tabBarBottom = Math.max(insets.bottom, 8);

  return (
    <Tabs
      screenOptions={{
        tabBarStyle: {
          backgroundColor: colors.bgPrimary,
          borderTopColor: colors.borderSubtle,
          borderTopWidth: 0.5,
          height: 52 + tabBarBottom,
          paddingBottom: tabBarBottom,
          paddingTop: 4,
        },
        tabBarActiveTintColor: colors.accent,
        tabBarInactiveTintColor: colors.disabledText,
        tabBarShowLabel: false,
        headerShown: false,
      }}
    >
      <Tabs.Screen
        name="index"
        options={{
          title: 'Agents',
          tabBarIcon: ({ color, focused }) => (
            <TabSymbol glyph={focused ? '●' : '○'} color={color} focused={focused} fontSize={14} />
          ),
        }}
      />
      <Tabs.Screen
        name="issues"
        options={{
          title: 'Issues',
          tabBarIcon: ({ color, focused }) => (
            <TabSymbol glyph="◇" color={color} focused={focused} fontSize={16} />
          ),
        }}
      />
      <Tabs.Screen
        name="stats"
        options={{
          title: 'Stats',
          tabBarIcon: ({ color, focused }) => (
            <TabSymbol glyph="∷" color={color} focused={focused} fontSize={20} />
          ),
        }}
      />
      <Tabs.Screen
        name="settings"
        options={{
          title: 'Settings',
          tabBarIcon: ({ color, focused }) => (
            <TabSymbol glyph="///" color={color} focused={focused} fontSize={16} />
          ),
        }}
      />
    </Tabs>
  );
}

function TabSymbol({
  glyph,
  color,
  focused,
  fontSize,
}: {
  glyph: string;
  color: string;
  focused: boolean;
  fontSize: number;
}) {
  return (
    <RNText
      style={[
        styles.tabSymbol,
        {
          color,
          fontSize,
          opacity: focused ? 1 : 0.52,
        },
      ]}
    >
      {glyph}
    </RNText>
  );
}

const styles = StyleSheet.create({
  tabSymbol: {
    minWidth: 28,
    lineHeight: 22,
    textAlign: 'center',
  },
});
