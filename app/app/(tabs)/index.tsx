import React, { useEffect, useMemo, useState } from 'react';
import {
  FlatList,
  SafeAreaView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import { useRouter } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { Agent, useAgents } from '../../store/agents';
import { AgentStatus, Colors, Spacing, Typography, statusColor } from '../../constants/tokens';
import {
  getInboxViewMode,
  getRecentAgentOpens,
  markAgentOpened,
  setInboxViewMode,
  StoredInboxViewMode,
  StoredRecentAgentOpens,
} from '../../services/storage';
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
  const [viewMode, setViewModeState] = useState<StoredInboxViewMode>('list');
  const [recentAgentOpens, setRecentAgentOpens] = useState<StoredRecentAgentOpens>({});

  useEffect(() => {
    let cancelled = false;

    (async () => {
      const [storedViewMode, storedRecentOpens] = await Promise.all([
        getInboxViewMode(),
        getRecentAgentOpens(),
      ]);
      if (!cancelled) {
        setViewModeState(storedViewMode);
        setRecentAgentOpens(storedRecentOpens);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, []);

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

  const sortedAgents = useMemo(() => {
    return [...state.agents].sort((left, right) => {
      const leftOpenedAt = recentAgentOpens[left.id] ?? 0;
      const rightOpenedAt = recentAgentOpens[right.id] ?? 0;
      if (leftOpenedAt !== rightOpenedAt) return rightOpenedAt - leftOpenedAt;

      const leftPriority = STATUS_PRIORITY[left.status] ?? 5;
      const rightPriority = STATUS_PRIORITY[right.status] ?? 5;
      if (leftPriority !== rightPriority) return leftPriority - rightPriority;
      return (right.updated_at || 0) - (left.updated_at || 0);
    });
  }, [recentAgentOpens, state.agents]);

  const runningCount = state.agents.filter(agent => agent.status === 'running').length;

  const setViewMode = async (mode: StoredInboxViewMode) => {
    setViewModeState(mode);
    await setInboxViewMode(mode);
  };

  const openAgent = (agentId: string) => {
    const openedAt = Date.now();
    setRecentAgentOpens(previous => ({
      ...previous,
      [agentId]: openedAt,
    }));
    void markAgentOpened(agentId, openedAt);
    router.push(`/terminal/${agentId}`);
  };

  const renderListAgent = ({ item }: { item: Agent }) => (
    <TouchableOpacity
      style={styles.listCard}
      onPress={() => openAgent(item.id)}
      activeOpacity={0.82}
    >
      <View style={styles.listHeader}>
        <View style={styles.listTitleBlock}>
          <View style={styles.inlineStatus}>
            <View style={[styles.statusDot, { backgroundColor: statusColor(item.status) }]} />
            <Text style={styles.inlineStatusText}>{getStatusLabel(item.status)}</Text>
          </View>
          <Text style={styles.listTitle} numberOfLines={1}>{item.name}</Text>
        </View>
      </View>

      {item.project ? <Text style={styles.listProject} numberOfLines={1}>{item.project}</Text> : null}

      <Text style={styles.listPreview} numberOfLines={2}>
        {buildCompactPreview(item)}
      </Text>
    </TouchableOpacity>
  );

  const renderGridAgent = ({ item, index }: { item: Agent; index: number }) => (
    <TouchableOpacity
      style={[
        styles.previewCard,
        index % 2 === 0 ? styles.previewCardLeft : styles.previewCardRight,
      ]}
      onPress={() => openAgent(item.id)}
      activeOpacity={0.84}
    >
      <View style={styles.previewTopRow}>
        <View style={styles.previewTitleWrap}>
          <Text style={styles.previewTitle} numberOfLines={1}>{item.name}</Text>
          {item.project ? <Text style={styles.previewProject} numberOfLines={1}>{item.project}</Text> : null}
        </View>
        <View style={[styles.previewStatusPill, { borderColor: statusColor(item.status) + '55' }]}>
          <View style={[styles.previewStatusDot, { backgroundColor: statusColor(item.status) }]} />
          <Text style={styles.previewStatusText}>{getStatusLabel(item.status)}</Text>
        </View>
      </View>

      <Text style={styles.previewBody} numberOfLines={4}>
        {buildPreviewBody(item)}
      </Text>
    </TouchableOpacity>
  );

  return (
    <SafeAreaView style={styles.container}>
      {!state.connected && (
        <View style={styles.banner}>
          <Text style={styles.bannerText}>Connecting...</Text>
        </View>
      )}

      <View style={styles.header}>
        <View style={styles.headerCopy}>
          <Text style={styles.title}>Agents</Text>
          <Text style={styles.subtitle}>{runningCount} running</Text>
        </View>

        <View style={styles.viewToggle}>
          <IconToggleButton
            icon="reorder-three-outline"
            selected={viewMode === 'list'}
            onPress={() => setViewMode('list')}
          />
          <IconToggleButton
            icon="grid-outline"
            selected={viewMode === 'grid'}
            onPress={() => setViewMode('grid')}
          />
        </View>
      </View>

      {sortedAgents.length === 0 ? (
        <View style={styles.emptyContainer}>
          <Text style={styles.emptyIcon}>◉</Text>
          <Text style={styles.emptyText}>No agents running</Text>
          <Text style={styles.emptySubtext}>Start an agent on your homelab</Text>
        </View>
      ) : viewMode === 'list' ? (
        <FlatList
          data={sortedAgents}
          key="list"
          keyExtractor={item => item.id}
          renderItem={renderListAgent}
          contentContainerStyle={styles.listContent}
          ItemSeparatorComponent={() => <View style={styles.listGap} />}
        />
      ) : (
        <FlatList
          data={sortedAgents}
          key="grid"
          keyExtractor={item => item.id}
          renderItem={renderGridAgent}
          numColumns={2}
          columnWrapperStyle={styles.gridRow}
          contentContainerStyle={styles.gridContent}
        />
      )}
    </SafeAreaView>
  );
}

function IconToggleButton({
  icon,
  selected,
  onPress,
}: {
  icon: React.ComponentProps<typeof Ionicons>['name'];
  selected: boolean;
  onPress: () => void;
}) {
  return (
    <TouchableOpacity
      style={[styles.viewBtn, selected && styles.viewBtnActive]}
      onPress={onPress}
      activeOpacity={0.82}
    >
      <Ionicons
        name={icon}
        size={18}
        color={selected ? Colors.textPrimary : Colors.textSecondary}
      />
    </TouchableOpacity>
  );
}

function buildCompactPreview(agent: Agent): string {
  const preview = extractPreviewLines(agent).join('  ·  ');
  return preview || agent.summary || 'No recent output';
}

function buildPreviewBody(agent: Agent): string {
  const lines = extractPreviewLines(agent);
  if (lines.length > 0) return lines.join('\n');
  return agent.summary || 'No recent output';
}

function extractPreviewLines(agent: Agent): string[] {
  const cleaned = agent.last_output_lines
    .map(line => stripAnsi(line).replace(/\s+/g, ' ').trim())
    .filter(Boolean);

  const collected: string[] = [];
  for (let index = cleaned.length - 1; index >= 0; index -= 1) {
    const line = cleaned[index];
    if (!isMeaningfulPreviewLine(line, agent)) continue;
    collected.push(line);
    if (collected.length === 3) break;
  }

  return collected;
}

function isMeaningfulPreviewLine(line: string, agent: Agent): boolean {
  if (!line) return false;

  if (line === agent.summary.trim()) return false;
  if (line.includes('background terminal running')) return false;
  if (line.includes('/ps to vie')) return false;
  if (line.includes('gpt-') && line.includes('left')) return false;
  if (line.includes('~/workspace/')) return false;
  if (/^\[[^\]]+\]/.test(line) && line.includes('node') && line.includes('c')) return false;
  if (/^[>$#]\s/.test(line)) return false;
  if (/^[A-Za-z0-9._-]+@[A-Za-z0-9._-]+:/.test(line)) return false;
  if (/^tmux\s*\(/i.test(line)) return false;
  if (/@filename\b/.test(line)) return false;

  return true;
}

function stripAnsi(value: string): string {
  return value.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '');
}

function getStatusLabel(status: AgentStatus): string {
  switch (status) {
    case 'running':
      return 'Running';
    case 'blocked':
      return 'Needs Input';
    case 'failed':
      return 'Error';
    case 'done':
      return 'Done';
    case 'unknown':
      return 'Waiting';
  }
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: '#0B1118',
  },
  banner: {
    backgroundColor: Colors.statusUnknown,
    padding: 8,
    alignItems: 'center',
  },
  bannerText: {
    color: Colors.bgPrimary,
    fontFamily: Typography.uiFontMedium,
    fontSize: 13,
  },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    paddingHorizontal: Spacing.screenMargin,
    paddingTop: 16,
    paddingBottom: 14,
  },
  headerCopy: {
    flex: 1,
    paddingRight: 12,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 24,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: -0.3,
  },
  subtitle: {
    marginTop: 4,
    color: '#7C8A9B',
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  viewToggle: {
    flexDirection: 'row',
    padding: 4,
    borderRadius: 18,
    backgroundColor: '#111923',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.06)',
    gap: 4,
  },
  viewBtn: {
    width: 34,
    height: 34,
    borderRadius: 14,
    alignItems: 'center',
    justifyContent: 'center',
  },
  viewBtnActive: {
    backgroundColor: '#1C2734',
  },
  listContent: {
    paddingHorizontal: Spacing.screenMargin,
    paddingBottom: 28,
  },
  listGap: {
    height: 10,
  },
  listCard: {
    borderRadius: 18,
    paddingHorizontal: 14,
    paddingVertical: 14,
    backgroundColor: '#121B25',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.05)',
  },
  listHeader: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    justifyContent: 'space-between',
    gap: 10,
  },
  listTitleBlock: {
    flex: 1,
  },
  inlineStatus: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    marginBottom: 8,
  },
  statusDot: {
    width: 7,
    height: 7,
    borderRadius: 4,
  },
  inlineStatusText: {
    color: '#8A98AA',
    fontSize: 10,
    fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase',
    letterSpacing: 0.6,
  },
  listTitle: {
    color: Colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
  },
  listProject: {
    marginTop: 6,
    color: '#5B9DFF',
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  listPreview: {
    marginTop: 10,
    color: '#C4CFDB',
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
  },
  gridContent: {
    paddingHorizontal: Spacing.screenMargin,
    paddingBottom: 28,
  },
  gridRow: {
    gap: 12,
    marginBottom: 12,
  },
  previewCard: {
    flex: 1,
    minHeight: 152,
    borderRadius: 18,
    paddingHorizontal: 14,
    paddingVertical: 14,
    backgroundColor: '#121B25',
    borderWidth: 1,
    borderColor: 'rgba(255,255,255,0.05)',
  },
  previewCardLeft: {
    marginRight: 0,
  },
  previewCardRight: {
    marginLeft: 0,
  },
  previewTopRow: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'flex-start',
    gap: 8,
    marginBottom: 12,
  },
  previewTitleWrap: {
    flex: 1,
  },
  previewTitle: {
    color: Colors.textPrimary,
    fontSize: 15,
    fontFamily: Typography.uiFontMedium,
    marginBottom: 4,
  },
  previewProject: {
    color: '#5B9DFF',
    fontSize: 11,
    fontFamily: Typography.uiFont,
  },
  previewStatusPill: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 5,
    height: 24,
    paddingHorizontal: 8,
    borderRadius: 999,
    backgroundColor: 'rgba(255,255,255,0.03)',
    borderWidth: 1,
  },
  previewStatusDot: {
    width: 6,
    height: 6,
    borderRadius: 3,
  },
  previewStatusText: {
    color: '#D7E1EB',
    fontSize: 10,
    fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase',
  },
  previewBody: {
    flex: 1,
    color: '#C8D3DE',
    fontSize: 11.5,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
  emptyContainer: {
    flex: 1,
    justifyContent: 'center',
    alignItems: 'center',
  },
  emptyIcon: {
    fontSize: 48,
    color: Colors.textSecondary,
    marginBottom: 16,
  },
  emptyText: {
    color: Colors.textPrimary,
    fontSize: 18,
    fontFamily: Typography.uiFontMedium,
  },
  emptySubtext: {
    color: Colors.textSecondary,
    fontSize: 14,
    fontFamily: Typography.uiFont,
    marginTop: 8,
  },
});
