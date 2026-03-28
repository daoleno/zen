import React, { useEffect, useState } from 'react';
import {
  View,
  Text,
  FlatList,
  TouchableOpacity,
  StyleSheet,
  SafeAreaView,
} from 'react-native';
import { useRouter } from 'expo-router';
import { Colors, Spacing, Typography, statusColor, AgentStatus } from '../../constants/tokens';
import { useAgents, Agent } from '../../store/agents';
import { wsClient } from '../../services/websocket';

const STATUS_PRIORITY: Record<AgentStatus, number> = {
  failed: 0,
  blocked: 1,
  unknown: 2,
  running: 3,
  done: 4,
};

export default function InboxScreen() {
  const { state, dispatch } = useAgents();
  const router = useRouter();
  const [filter, setFilter] = useState<'active' | 'all'>('active');

  useEffect(() => {
    const onAgentList = (data: any) => {
      dispatch({ type: 'SET_AGENTS', agents: data.agents || [] });
    };
    const onStateChange = (data: any) => {
      dispatch({ type: 'STATE_CHANGE', agent_id: data.agent_id, old: data.old, new_state: data.new });
    };
    const onOutput = (data: any) => {
      dispatch({ type: 'UPDATE_OUTPUT', agent_id: data.agent_id, lines: data.lines || [] });
    };
    const onConnected = () => dispatch({ type: 'SET_CONNECTED', connected: true });
    const onDisconnected = () => dispatch({ type: 'SET_CONNECTED', connected: false });

    wsClient.on('agent_list', onAgentList);
    wsClient.on('agent_state_change', onStateChange);
    wsClient.on('agent_output', onOutput);
    wsClient.on('connected', onConnected);
    wsClient.on('disconnected', onDisconnected);

    return () => {
      wsClient.off('agent_list', onAgentList);
      wsClient.off('agent_state_change', onStateChange);
      wsClient.off('agent_output', onOutput);
      wsClient.off('connected', onConnected);
      wsClient.off('disconnected', onDisconnected);
    };
  }, [dispatch]);

  const sortedAgents = [...state.agents]
    .filter(a => filter === 'all' || (a.status !== 'done' && a.status !== 'failed'))
    .sort((a, b) => {
      const pa = STATUS_PRIORITY[a.status] ?? 5;
      const pb = STATUS_PRIORITY[b.status] ?? 5;
      if (pa !== pb) return pa - pb;
      return (b.updated_at || 0) - (a.updated_at || 0);
    });

  const needsAttention = state.agents.some(a => a.status === 'blocked' || a.status === 'failed');
  const showZen = state.connected && !needsAttention && state.agents.length > 0;

  const zenBanner = showZen && filter === 'active';

  const renderAgent = ({ item }: { item: Agent }) => {
    return (
      <TouchableOpacity
        style={styles.row}
        onPress={() => router.push(`/terminal/${item.id}`)}
        activeOpacity={0.7}
      >
        <View style={[styles.dot, { backgroundColor: statusColor(item.status) }]} />
        <View style={styles.rowContent}>
          <Text style={styles.agentName} numberOfLines={1}>{item.name}</Text>
          <Text style={styles.summary} numberOfLines={1}>
            {item.project ? `${item.project} · ` : ''}{item.summary}
          </Text>
        </View>
        <Text style={styles.statusText}>{item.status}</Text>
      </TouchableOpacity>
    );
  };

  return (
    <SafeAreaView style={styles.container}>
      {!state.connected && (
        <View style={styles.banner}>
          <Text style={styles.bannerText}>Connecting...</Text>
        </View>
      )}

      <View style={styles.filterRow}>
        <TouchableOpacity
          style={[styles.filterBtn, filter === 'active' && styles.filterBtnActive]}
          onPress={() => setFilter('active')}
        >
          <Text style={[styles.filterText, filter === 'active' && styles.filterTextActive]}>Active</Text>
        </TouchableOpacity>
        <TouchableOpacity
          style={[styles.filterBtn, filter === 'all' && styles.filterBtnActive]}
          onPress={() => setFilter('all')}
        >
          <Text style={[styles.filterText, filter === 'all' && styles.filterTextActive]}>All</Text>
        </TouchableOpacity>
      </View>

      {zenBanner && (
        <View style={styles.zenBanner}>
          <Text style={styles.zenBannerText}>
            ☯ All clear · {state.agents.filter(a => a.status === 'running').length} running
          </Text>
        </View>
      )}

      {sortedAgents.length === 0 ? (
        <View style={styles.emptyContainer}>
          <Text style={styles.emptyIcon}>◉</Text>
          <Text style={styles.emptyText}>No agents running</Text>
          <Text style={styles.emptySubtext}>Start an agent on your homelab</Text>
        </View>
      ) : (
        <FlatList
          data={sortedAgents}
          keyExtractor={item => item.id}
          renderItem={renderAgent}
          ItemSeparatorComponent={() => <View style={styles.separator} />}
        />
      )}
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.bgPrimary },
  banner: { backgroundColor: Colors.statusUnknown, padding: 8, alignItems: 'center' },
  bannerText: { color: Colors.bgPrimary, fontWeight: '600', fontSize: 13 },
  filterRow: { flexDirection: 'row', padding: Spacing.screenMargin, gap: 8 },
  filterBtn: { paddingHorizontal: 16, paddingVertical: 6, borderRadius: 16, backgroundColor: Colors.bgSurface },
  filterBtnActive: { backgroundColor: Colors.bgElevated },
  filterText: { color: Colors.textSecondary, fontSize: 13 },
  filterTextActive: { color: Colors.textPrimary },
  row: {
    flexDirection: 'row',
    alignItems: 'center',
    height: Spacing.rowHeight,
    paddingHorizontal: Spacing.rowPaddingH,
    paddingVertical: Spacing.rowPaddingV,
  },
  dot: { width: 8, height: 8, borderRadius: 4, marginRight: 12 },
  rowContent: { flex: 1, marginRight: 8 },
  agentName: { color: Colors.textPrimary, fontSize: Typography.agentNameSize, fontWeight: '600' },
  summary: { color: Colors.textSecondary, fontSize: Typography.metadataSize, marginTop: 2 },
  statusText: { color: Colors.textSecondary, fontSize: Typography.metadataSize },
  separator: { height: 1, backgroundColor: Colors.bgSurface, marginLeft: Spacing.rowPaddingH + 20 },
  emptyContainer: { flex: 1, justifyContent: 'center', alignItems: 'center' },
  emptyIcon: { fontSize: 48, color: Colors.textSecondary, marginBottom: 16 },
  emptyText: { color: Colors.textPrimary, fontSize: 18, fontWeight: '600' },
  emptySubtext: { color: Colors.textSecondary, fontSize: 14, marginTop: 8 },
  zenBanner: { backgroundColor: Colors.bgSurface, paddingVertical: 10, alignItems: 'center', marginHorizontal: Spacing.screenMargin, borderRadius: 8, marginBottom: 8 },
  zenBannerText: { color: Colors.statusRunning, fontSize: 13, fontWeight: '500' },
});
