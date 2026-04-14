import React, { useMemo, useState } from 'react';
import {
  Alert,
  FlatList,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import { useRouter } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Colors, Typography } from '../../constants/tokens';
import { useTasks, Task } from '../../store/tasks';
import { useAgents } from '../../store/agents';
import { IssueRow } from '../../components/issue/IssueRow';
import { StatusFilterBar, IssueFilter } from '../../components/issue/StatusFilterBar';
import { CreateIssueSheet } from '../../components/issue/CreateIssueSheet';
import { getServers, StoredServer } from '../../services/storage';
import { useFocusEffect } from 'expo-router';

const STATUS_ORDER: Record<string, number> = {
  in_progress: 0,
  todo: 1,
  backlog: 2,
  done: 3,
  cancelled: 4,
};

function matchesFilter(task: Task, filter: IssueFilter): boolean {
  switch (filter) {
    case 'active': return task.status === 'todo' || task.status === 'in_progress';
    case 'backlog': return task.status === 'backlog';
    case 'done': return task.status === 'done' || task.status === 'cancelled';
    case 'all': return true;
  }
}

export default function IssuesScreen() {
  const router = useRouter();
  const { state: taskState } = useTasks();
  const { state: agentState } = useAgents();
  const [filter, setFilter] = useState<IssueFilter>('active');
  const [createVisible, setCreateVisible] = useState(false);
  const [servers, setServers] = useState<StoredServer[]>([]);
  const [selectedServerId, setSelectedServerId] = useState<string | null>(null);

  useFocusEffect(
    React.useCallback(() => {
      let cancelled = false;
      (async () => {
        const s = await getServers();
        if (!cancelled) setServers(s);
      })();
      return () => { cancelled = true; };
    }, []),
  );

  const connectedServers = useMemo(
    () => servers.filter(s => agentState.serverConnections[s.id] === 'connected'),
    [servers, agentState.serverConnections],
  );
  const serverOptions = useMemo(
    () => connectedServers.map(s => ({ id: s.id, name: s.name })),
    [connectedServers],
  );

  const sortedIssues = useMemo(() => {
    return taskState.tasks
      .filter(t => matchesFilter(t, filter))
      .sort((a, b) => {
        // Sort by status group first
        const sa = STATUS_ORDER[a.status] ?? 5;
        const sb = STATUS_ORDER[b.status] ?? 5;
        if (sa !== sb) return sa - sb;

        // Then by priority (urgent=1 first, none=0 last)
        const pa = a.priority === 0 ? 5 : a.priority;
        const pb = b.priority === 0 ? 5 : b.priority;
        if (pa !== pb) return pa - pb;

        // Then by updatedAt desc
        return b.updatedAt - a.updatedAt;
      });
  }, [taskState.tasks, filter]);
  const runById = useMemo(
    () => Object.fromEntries(taskState.runs.map(run => [`${run.serverId}:${run.id}`, run])),
    [taskState.runs],
  );

  const openCreateSheet = () => {
    if (connectedServers.length === 0) {
      Alert.alert('No server connected', 'Connect to a daemon first.');
      return;
    }
    setSelectedServerId(prev =>
      prev && connectedServers.some(s => s.id === prev)
        ? prev
        : connectedServers[0].id,
    );
    setCreateVisible(true);
  };

  const filterCounts = useMemo(() => ({
    active: taskState.tasks.filter(t => t.status === 'todo' || t.status === 'in_progress').length,
    backlog: taskState.tasks.filter(t => t.status === 'backlog').length,
    done: taskState.tasks.filter(t => t.status === 'done' || t.status === 'cancelled').length,
    all: taskState.tasks.length,
  }), [taskState.tasks]);

  return (
    <SafeAreaView style={styles.container} edges={['top']}>
      <View style={styles.header}>
        <Text style={styles.title}>Issues</Text>
        <TouchableOpacity style={styles.addBtn} onPress={openCreateSheet} activeOpacity={0.82}>
          <Ionicons name="add" size={19} color={Colors.textPrimary} />
        </TouchableOpacity>
      </View>

      <StatusFilterBar selected={filter} onSelect={setFilter} />

      {sortedIssues.length === 0 ? (
        <View style={styles.emptyContainer}>
          <Text style={styles.emptyIcon}>◇</Text>
          <Text style={styles.emptyText}>
            {filter === 'active' ? 'No active issues' :
             filter === 'backlog' ? 'Backlog is empty' :
             filter === 'done' ? 'No completed issues' :
             'No issues yet'}
          </Text>
          <Text style={styles.emptySubtext}>
            {taskState.tasks.length === 0
              ? 'Create an issue to start tracking your work.'
              : 'Try a different filter.'}
          </Text>
          {taskState.tasks.length === 0 && connectedServers.length > 0 && (
            <TouchableOpacity
              style={styles.emptyActionBtn}
              onPress={openCreateSheet}
              activeOpacity={0.82}
            >
              <Text style={styles.emptyActionText}>New Issue</Text>
            </TouchableOpacity>
          )}
        </View>
      ) : (
        <FlatList
          data={sortedIssues}
          keyExtractor={item => `${item.serverId}:${item.id}`}
          renderItem={({ item }) => (
            <IssueRow
              task={item}
              run={item.currentRunId ? runById[`${item.serverId}:${item.currentRunId}`] : null}
              onPress={() => router.push({
                pathname: '/issue/[id]',
                params: { id: item.id, serverId: item.serverId },
              })}
            />
          )}
          contentContainerStyle={styles.listContent}
        />
      )}

      <CreateIssueSheet
        visible={createVisible}
        serverOptions={serverOptions}
        selectedServerId={selectedServerId}
        onSelectServer={setSelectedServerId}
        onClose={() => setCreateVisible(false)}
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: Colors.bgPrimary },
  header: {
    flexDirection: 'row',
    justifyContent: 'space-between',
    alignItems: 'center',
    paddingHorizontal: 20,
    paddingTop: 8,
    paddingBottom: 12,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 22,
    lineHeight: 28,
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 1,
    opacity: 0.9,
  },
  addBtn: {
    width: 32, height: 32, borderRadius: 10,
    alignItems: 'center', justifyContent: 'center',
  },
  listContent: { paddingHorizontal: 20, paddingBottom: 32 },
  emptyContainer: {
    flex: 1, justifyContent: 'center', alignItems: 'center', paddingHorizontal: 24,
  },
  emptyIcon: {
    fontSize: 44, color: Colors.textSecondary, marginBottom: 16, opacity: 0.6,
  },
  emptyText: {
    color: Colors.textPrimary, fontSize: 17,
    fontFamily: Typography.uiFontMedium, opacity: 0.8,
  },
  emptySubtext: {
    color: Colors.textSecondary, fontSize: 13, fontFamily: Typography.uiFont,
    marginTop: 6, maxWidth: 280, textAlign: 'center', opacity: 0.6,
  },
  emptyActionBtn: {
    marginTop: 22, paddingHorizontal: 20, minHeight: 40, borderRadius: 12,
    alignItems: 'center', justifyContent: 'center',
    backgroundColor: Colors.accent,
  },
  emptyActionText: {
    color: Colors.bgPrimary, fontSize: 13, fontFamily: Typography.uiFontMedium,
  },
});
