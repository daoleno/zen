import React, { useState, useEffect } from 'react';
import {
  View,
  Text,
  TextInput,
  Switch,
  StyleSheet,
  SafeAreaView,
  ScrollView,
  TouchableOpacity,
} from 'react-native';
import { Colors, Spacing } from '../../constants/tokens';
import { wsClient } from '../../services/websocket';
import { useAgents } from '../../store/agents';
import * as Storage from '../../services/storage';

export default function SettingsScreen() {
  const { state } = useAgents();
  const [serverUrl, setServerUrl] = useState('');
  const [nightMode, setNightMode] = useState(false);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    (async () => {
      const url = await Storage.getServerUrl();
      const nm = await Storage.getNightMode();
      setServerUrl(url || 'ws://localhost:9876/ws');
      setNightMode(nm);
      setLoaded(true);
    })();
  }, []);

  const handleConnect = async () => {
    await Storage.setServerUrl(serverUrl);
    wsClient.disconnect();
    wsClient.connect(serverUrl);
  };

  const handleNightMode = async (value: boolean) => {
    setNightMode(value);
    await Storage.setNightMode(value);
  };

  if (!loaded) return null;

  return (
    <SafeAreaView style={styles.container}>
      <ScrollView contentContainerStyle={styles.content}>
        <Text style={styles.sectionTitle}>Server Connection</Text>
        <View style={styles.card}>
          <Text style={styles.label}>Server URL</Text>
          <TextInput
            style={styles.input}
            value={serverUrl}
            onChangeText={setServerUrl}
            placeholder="ws://your-server:9876/ws"
            placeholderTextColor={Colors.textSecondary}
            autoCapitalize="none"
            autoCorrect={false}
          />
          <TouchableOpacity style={styles.connectBtn} onPress={handleConnect}>
            <Text style={styles.connectBtnText}>
              {state.connected ? 'Reconnect' : 'Connect'}
            </Text>
          </TouchableOpacity>
          <View style={styles.statusRow}>
            <View style={[styles.statusDot, { backgroundColor: state.connected ? Colors.statusRunning : Colors.statusFailed }]} />
            <Text style={styles.statusLabel}>
              {state.connected ? 'Connected' : 'Disconnected'}
            </Text>
          </View>
        </View>

        <Text style={styles.sectionTitle}>Notifications</Text>
        <View style={styles.card}>
          <SettingRow label="Agent blocked" sublabel="Always push" value={true} />
          <SettingRow label="Agent failed" sublabel="Always push" value={true} />
          <SettingRow label="Agent done" sublabel="During work hours" value={true} />
          <SettingRow label="Rest reminders" sublabel="Every 90 minutes" value={true} />
        </View>

        <Text style={styles.sectionTitle}>Wellness</Text>
        <View style={styles.card}>
          <View style={styles.settingRow}>
            <View>
              <Text style={styles.settingLabel}>Deep night mode</Text>
              <Text style={styles.settingSublabel}>Mute non-critical after midnight</Text>
            </View>
            <Switch
              value={nightMode}
              onValueChange={handleNightMode}
              trackColor={{ false: Colors.bgSurface, true: Colors.zenGreen }}
              thumbColor={Colors.textPrimary}
            />
          </View>
        </View>

        <Text style={styles.version}>zen v0.1.0</Text>
      </ScrollView>
    </SafeAreaView>
  );
}

function SettingRow({ label, sublabel, value }: { label: string; sublabel: string; value: boolean }) {
  const [enabled, setEnabled] = useState(value);
  return (
    <View style={styles.settingRow}>
      <View>
        <Text style={styles.settingLabel}>{label}</Text>
        <Text style={styles.settingSublabel}>{sublabel}</Text>
      </View>
      <Switch
        value={enabled}
        onValueChange={setEnabled}
        trackColor={{ false: Colors.bgSurface, true: Colors.accent }}
        thumbColor={Colors.textPrimary}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.bgPrimary },
  content: { padding: Spacing.screenMargin },
  sectionTitle: {
    color: Colors.textSecondary,
    fontSize: 13,
    fontWeight: '600',
    textTransform: 'uppercase',
    letterSpacing: 1,
    marginTop: 24,
    marginBottom: 8,
  },
  card: { backgroundColor: Colors.bgSurface, borderRadius: 12, padding: 16 },
  label: { color: Colors.textSecondary, fontSize: 13, marginBottom: 8 },
  input: {
    backgroundColor: Colors.bgElevated,
    borderRadius: 10,
    paddingHorizontal: 14,
    paddingVertical: 10,
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: 'monospace',
    borderWidth: 1,
    borderColor: Colors.bgElevated,
  },
  connectBtn: {
    backgroundColor: Colors.accent,
    borderRadius: 10,
    paddingVertical: 12,
    alignItems: 'center',
    marginTop: 12,
    minHeight: 44,
    justifyContent: 'center',
  },
  connectBtnText: { color: Colors.bgPrimary, fontWeight: '600', fontSize: 15 },
  statusRow: { flexDirection: 'row', alignItems: 'center', marginTop: 12 },
  statusDot: { width: 8, height: 8, borderRadius: 4, marginRight: 8 },
  statusLabel: { color: Colors.textSecondary, fontSize: 13 },
  settingRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingVertical: 8,
  },
  settingLabel: { color: Colors.textPrimary, fontSize: 15 },
  settingSublabel: { color: Colors.textSecondary, fontSize: 12, marginTop: 2 },
  version: { color: Colors.textSecondary, fontSize: 12, textAlign: 'center', marginTop: 32 },
});
