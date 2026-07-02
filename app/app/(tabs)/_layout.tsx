import { useMemo } from 'react';
import { StyleSheet } from 'react-native';
import { Tabs, TabList, TabTrigger, TabSlot } from 'expo-router/ui';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { getFloatingTabBarInset } from '../../components/navigation/floatingTabBarMetrics';
import { TabScreenInsetProvider } from '../../components/navigation/TabScreenInsetContext';
import { ZenFloatingTabList } from '../../components/navigation/ZenFloatingTabList';
import { ZenTabTrigger } from '../../components/navigation/ZenTabTrigger';

export default function TabLayout() {
  const insets = useSafeAreaInsets();
  const tabScreenBottomInset = useMemo(
    () => getFloatingTabBarInset(insets.bottom),
    [insets.bottom],
  );

  return (
    <TabScreenInsetProvider inset={tabScreenBottomInset}>
      <Tabs style={styles.tabs}>
        <TabSlot />
        <ZenFloatingTabList>
          <ZenTabTrigger
            name="agents"
            label="Agents"
            icon="chatbubbles-outline"
            iconFocused="chatbubbles"
          />
          <ZenTabTrigger
            name="brain"
            label="Brain"
            icon="hardware-chip-outline"
            iconFocused="hardware-chip"
          />
          <ZenTabTrigger
            name="stats"
            label="Stats"
            icon="bar-chart-outline"
            iconFocused="bar-chart"
          />
          <ZenTabTrigger
            name="settings"
            label="Settings"
            icon="settings-outline"
            iconFocused="settings"
          />
        </ZenFloatingTabList>
        <TabList style={styles.hiddenTabList}>
          <TabTrigger name="agents" href="/" />
          <TabTrigger name="brain" href="/work" />
          <TabTrigger name="stats" href="/stats" />
          <TabTrigger name="settings" href="/settings" />
        </TabList>
      </Tabs>
    </TabScreenInsetProvider>
  );
}

const styles = StyleSheet.create({
  tabs: {
    flex: 1,
  },
  hiddenTabList: {
    display: 'none',
  },
});