import { StyleSheet } from 'react-native';
import { Tabs, TabList, TabTrigger, TabSlot } from 'expo-router/ui';
import { PrimaryDrawerShell } from '../../components/navigation/PrimaryDrawerShell';

export default function PrimaryLayout() {
  return (
    <Tabs style={styles.tabs} options={{ backBehavior: 'none' }}>
      <PrimaryDrawerShell>
        <TabSlot />
      </PrimaryDrawerShell>
      <TabList style={styles.hiddenTabList}>
        <TabTrigger name="brain" href="/" />
        <TabTrigger name="list" href="/list" />
      </TabList>
    </Tabs>
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
