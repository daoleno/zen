import React, { useMemo, useState } from 'react';
import {
  Alert,
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from 'react-native';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { Ionicons } from '@expo/vector-icons';
import { SafeAreaView } from 'react-native-safe-area-context';
import { Colors, Typography, issueStatusColor, runStatusColor } from '../../constants/tokens';
import type { IssuePriority, IssueStatus } from '../../constants/tokens';
import { useTasks } from '../../store/tasks';
import type { Run, Task } from '../../store/tasks';
import { useAgents } from '../../store/agents';
import type { Agent } from '../../store/agents';
import { wsClient } from '../../services/websocket';
import { IssueStatusIcon } from '../../components/issue/IssueStatusIcon';
import { StatusPicker } from '../../components/issue/StatusPicker';
import { PriorityPicker } from '../../components/issue/PriorityPicker';
import { DelegateRunSheet } from '../../components/issue/DelegateRunSheet';
import { TerminalPreview } from '../../components/terminal/TerminalPreview';

const PRIORITY_LABEL: Record<number, string> = {
  0: 'None',
  1: 'Urgent',
  2: 'High',
  3: 'Medium',
  4: 'Low',
};

const STATUS_LABEL: Record<string, string> = {
  backlog: 'Backlog',
  todo: 'Todo',
  in_progress: 'In Progress',
  done: 'Done',
  cancelled: 'Cancelled',
};

const RUN_STATUS_LABEL: Record<string, string> = {
  queued: 'Queued',
  running: 'Running',
  blocked: 'Blocked',
  done: 'Done',
  failed: 'Failed',
  cancelled: 'Cancelled',
};

const EXECUTION_MODE_LABEL: Record<string, string> = {
  spawn_new_session: 'New session',
  attach_existing_session: 'Existing session',
};

type TimelineItem = {
  key: string;
  timestamp: number;
  eyebrow: string;
  title: string;
  body?: string;
  meta?: string;
  note?: string;
  tone: string;
  sessionId?: string;
  sessionLabel?: string;
  sessionPreviewLines?: string[];
  sessionIsLive?: boolean;
};

function isActiveRunStatus(status?: string) {
  return status === 'queued' || status === 'running' || status === 'blocked';
}

function formatTimestamp(timestamp?: number) {
  if (!timestamp) {
    return 'Unknown time';
  }

  return new Date(timestamp).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });
}

function buildRunTitle(run: Run) {
  switch (run.status) {
    case 'queued':
      return 'Run queued';
    case 'running':
      return 'Run in progress';
    case 'blocked':
      return 'Run waiting on input';
    case 'done':
      return 'Run completed';
    case 'failed':
      return 'Run failed';
    case 'cancelled':
      return 'Run cancelled';
    default:
      return 'Run updated';
  }
}

function buildRunFallbackBody(run: Run) {
  const linkage =
    run.executionMode === 'attach_existing_session'
      ? 'This task is attached to a live session that started elsewhere.'
      : 'This task started a fresh live session for execution.';

  switch (run.status) {
    case 'queued':
      return `${linkage} The run is waiting to start.`;
    case 'running':
      return `${linkage} The agent is actively working.`;
    case 'blocked':
      return `${linkage} The agent paused and needs input before it can continue.`;
    case 'done':
      return `${linkage} Execution finished and is ready for review.`;
    case 'failed':
      return `${linkage} Execution stopped because of an error.`;
    case 'cancelled':
      return `${linkage} Execution was cancelled before completion.`;
    default:
      return linkage;
  }
}

function buildRunMeta(run: Run) {
  const parts = [`Attempt ${run.attemptNumber || 1}`];
  const modeLabel = EXECUTION_MODE_LABEL[run.executionMode] || '';
  if (modeLabel) {
    parts.push(modeLabel);
  }
  if (run.executorLabel) {
    parts.push(run.executorLabel);
  }
  return parts.join(' · ');
}

function describeTaskState(task: Task, currentRun?: Run, currentAgent?: Agent) {
  if (currentRun) {
    switch (currentRun.status) {
      case 'queued':
        return {
          title: 'Queued for execution',
          body:
            currentRun.summary
            || currentAgent?.summary
            || 'This task has been delegated and is waiting for the live session to start.',
          tone: runStatusColor(currentRun.status),
        };
      case 'running':
        return {
          title: 'Agent is working',
          body:
            currentRun.summary
            || currentAgent?.summary
            || 'Execution is active. Use the session view for runtime control and this page for status and review.',
          tone: runStatusColor(currentRun.status),
        };
      case 'blocked':
        return {
          title: 'Waiting on your input',
          body:
            currentRun.waitingReason
            || currentRun.summary
            || currentAgent?.summary
            || 'The agent paused and needs a decision before it can continue.',
          tone: runStatusColor(currentRun.status),
        };
      case 'done':
        return {
          title: 'Latest run completed',
          body:
            currentRun.summary
            || 'Execution finished. Review the activity below before you close the task.',
          tone: runStatusColor(currentRun.status),
        };
      case 'failed':
        return {
          title: 'Latest run failed',
          body:
            currentRun.lastError
            || currentRun.summary
            || 'Execution stopped because of an error. Review the failure details before retrying.',
          tone: runStatusColor(currentRun.status),
        };
      case 'cancelled':
        return {
          title: 'Latest run cancelled',
          body:
            currentRun.summary
            || 'Execution was cancelled. You can start another run when you are ready.',
          tone: runStatusColor(currentRun.status),
        };
      default:
        break;
    }
  }

  switch (task.status) {
    case 'done':
      return {
        title: 'Task completed',
        body: 'The task is marked done and has no active execution attached right now.',
        tone: issueStatusColor(task.status),
      };
    case 'cancelled':
      return {
        title: 'Task cancelled',
        body: 'The task is cancelled and has no active execution attached right now.',
        tone: issueStatusColor(task.status),
      };
    case 'in_progress':
      return {
        title: 'In progress without a live run',
        body: 'The task is marked in progress, but there is no active session attached at the moment.',
        tone: issueStatusColor(task.status),
      };
    default:
      return {
        title: 'Ready to delegate',
        body: 'This task can stay planning-only until you delegate a new run or attach an existing live session.',
        tone: issueStatusColor(task.status),
      };
  }
}

function buildTimelineItems(
  task: Task,
  runs: Run[],
  liveSessionById: Record<string, Agent>,
): TimelineItem[] {
  const items: TimelineItem[] = [
    {
      key: `task-${task.id}`,
      timestamp: task.createdAt,
      eyebrow: 'Task',
      title: 'Issue created',
      body: task.description
        ? 'The work item is defined and ready for execution.'
        : 'This task exists as planning-only work until you attach execution.',
      meta: [
        task.serverName,
        PRIORITY_LABEL[task.priority] !== 'None' ? `${PRIORITY_LABEL[task.priority]} priority` : undefined,
        STATUS_LABEL[task.status] || task.status,
      ].filter(Boolean).join(' · '),
      tone: issueStatusColor(task.status),
    },
  ];

  for (const run of runs) {
    const liveSession = run.agentSessionId ? liveSessionById[run.agentSessionId] : undefined;
    items.push({
      key: `run-${run.id}`,
      timestamp: run.endedAt || run.updatedAt || run.startedAt || run.createdAt,
      eyebrow: RUN_STATUS_LABEL[run.status] || run.status,
      title: buildRunTitle(run),
      body: run.summary || liveSession?.summary || buildRunFallbackBody(run),
      meta: buildRunMeta(run),
      note: run.waitingReason || run.lastError,
      tone: runStatusColor(run.status),
      sessionId: run.agentSessionId,
      sessionLabel: liveSession?.project?.trim() || liveSession?.name || run.agentSessionId,
      sessionPreviewLines: liveSession?.last_output_lines?.length ? liveSession.last_output_lines : undefined,
      sessionIsLive: !!liveSession,
    });
  }

  return items.sort((left, right) => right.timestamp - left.timestamp);
}

export default function IssueDetailScreen() {
  const { id, serverId } = useLocalSearchParams<{ id: string; serverId: string }>();
  const router = useRouter();
  const { state: taskState } = useTasks();
  const { state: agentState } = useAgents();
  const [statusPickerVisible, setStatusPickerVisible] = useState(false);
  const [delegating, setDelegating] = useState(false);
  const [delegateSheetVisible, setDelegateSheetVisible] = useState(false);

  const task = useMemo(
    () => taskState.tasks.find(t => t.id === id && t.serverId === serverId),
    [taskState.tasks, id, serverId],
  );

  const runsForTask = useMemo(() => {
    return taskState.runs
      .filter(run => run.serverId === serverId && run.taskId === id)
      .slice()
      .sort((left, right) => {
        const leftTs = left.endedAt || left.updatedAt || left.startedAt || left.createdAt;
        const rightTs = right.endedAt || right.updatedAt || right.startedAt || right.createdAt;
        return rightTs - leftTs;
      });
  }, [taskState.runs, id, serverId]);

  const liveSessionById = useMemo(() => {
    const entries = agentState.agents
      .filter(agent => agent.serverId === serverId)
      .map(agent => [agent.id, agent] as const);
    return Object.fromEntries(entries) as Record<string, Agent>;
  }, [agentState.agents, serverId]);

  const currentRun = useMemo(() => {
    if (!task) {
      return undefined;
    }

    if (task.currentRunId) {
      return runsForTask.find(run => run.id === task.currentRunId) || runsForTask[0];
    }

    return runsForTask[0];
  }, [task, runsForTask]);

  const currentAgent = useMemo(() => {
    if (!currentRun?.agentSessionId) {
      return undefined;
    }
    return liveSessionById[currentRun.agentSessionId];
  }, [currentRun, liveSessionById]);

  const activeRunBySessionId = useMemo(() => {
    const entries = taskState.runs
      .filter(run =>
        run.serverId === serverId
        && run.agentSessionId
        && isActiveRunStatus(run.status),
      )
      .map(run => [run.agentSessionId!, run] as const);
    return Object.fromEntries(entries) as Record<string, Run>;
  }, [serverId, taskState.runs]);

  const delegateSessionOptions = useMemo(() => {
    return agentState.agents
      .filter(agent => agent.serverId === serverId)
      .filter(agent => {
        const activeRun = activeRunBySessionId[agent.id];
        return !activeRun || activeRun.taskId === task?.id;
      })
      .map(agent => ({
        id: agent.id,
        title: agent.project?.trim() || agent.name,
        subtitle: agent.summary || agent.cwd || '',
        status: agent.status,
      }));
  }, [activeRunBySessionId, agentState.agents, serverId, task?.id]);

  const timelineItems = useMemo(
    () => (task ? buildTimelineItems(task, runsForTask, liveSessionById) : []),
    [task, runsForTask, liveSessionById],
  );

  if (!task) {
    return (
      <SafeAreaView style={styles.container}>
        <View style={styles.header}>
          <TouchableOpacity onPress={() => router.back()} activeOpacity={0.82}>
            <Ionicons name="arrow-back" size={22} color={Colors.textPrimary} />
          </TouchableOpacity>
          <Text style={styles.headerTitle}>Issue not found</Text>
        </View>
      </SafeAreaView>
    );
  }

  const latestRun = runsForTask[0];
  const hasActiveRun = isActiveRunStatus(currentRun?.status);
  const canDelegate = !hasActiveRun && task.status !== 'done' && task.status !== 'cancelled';
  const canMarkDone = task.status === 'in_progress' || task.status === 'todo';
  const summary = describeTaskState(task, currentRun, currentAgent);
  const runCountLabel = `${runsForTask.length} run${runsForTask.length === 1 ? '' : 's'}`;
  const delegateActionLabel = runsForTask.length === 0 ? 'Delegate to Agent' : 'Start Another Run';

  const openSession = (sessionId: string) => {
    router.push({
      pathname: '/terminal/[id]',
      params: { id: sessionId, serverId: task.serverId },
    });
  };

  const handleStatusChange = (status: IssueStatus) => {
    wsClient.updateTask(task.serverId, task.id, { status });
  };

  const handlePriorityChange = (priority: IssuePriority) => {
    wsClient.updateTask(task.serverId, task.id, { priority });
  };

  const handleStartNewRun = async () => {
    setDelegating(true);
    try {
      const result = await wsClient.createRun(task.serverId, {
        taskId: task.id,
        executionMode: 'spawn_new_session',
      });
      if (result.run?.agent_session_id) {
        openSession(result.run.agent_session_id);
      }
    } catch (error: any) {
      Alert.alert('Delegation failed', error?.message || 'Could not delegate.');
    } finally {
      setDelegating(false);
      setDelegateSheetVisible(false);
    }
  };

  const handleAttachSession = async (agentSessionId: string) => {
    setDelegating(true);
    try {
      const result = await wsClient.createRun(task.serverId, {
        taskId: task.id,
        executionMode: 'attach_existing_session',
        agentSessionId,
      });
      const targetSessionId = result.run?.agent_session_id || agentSessionId;
      if (targetSessionId) {
        openSession(targetSessionId);
      }
    } catch (error: any) {
      Alert.alert('Could not attach session', error?.message || 'Try another live session.');
    } finally {
      setDelegating(false);
      setDelegateSheetVisible(false);
    }
  };

  const handleDelete = () => {
    Alert.alert('Delete issue?', `ZEN-${task.number}: ${task.title}`, [
      { text: 'Cancel', style: 'cancel' },
      {
        text: 'Delete',
        style: 'destructive',
        onPress: () => {
          wsClient.deleteTask(task.serverId, task.id);
          router.back();
        },
      },
    ]);
  };

  return (
    <SafeAreaView style={styles.container}>
      <View style={styles.header}>
        <TouchableOpacity onPress={() => router.back()} activeOpacity={0.82}>
          <Ionicons name="arrow-back" size={22} color={Colors.textPrimary} />
        </TouchableOpacity>
        <Text style={styles.headerTitle}>ZEN-{task.number}</Text>
        <View style={{ flex: 1 }} />
      </View>

      <ScrollView style={styles.content} contentContainerStyle={styles.contentInner}>
        <View style={styles.heroCard}>
          <View style={styles.heroMetaRow}>
            <TouchableOpacity
              style={[
                styles.statusBadge,
                {
                  backgroundColor: `${issueStatusColor(task.status)}22`,
                  borderColor: issueStatusColor(task.status),
                },
              ]}
              onPress={() => setStatusPickerVisible(true)}
              activeOpacity={0.82}
            >
              <IssueStatusIcon status={task.status} size={14} />
              <Text style={[styles.statusBadgeText, { color: issueStatusColor(task.status) }]}>
                {STATUS_LABEL[task.status] || task.status}
              </Text>
              <Ionicons name="chevron-down" size={12} color={issueStatusColor(task.status)} />
            </TouchableOpacity>

            {currentRun ? (
              <View style={styles.metaChip}>
                <View style={[styles.metaChipDot, { backgroundColor: runStatusColor(currentRun.status) }]} />
                <Text style={styles.metaChipText}>
                  Attempt {currentRun.attemptNumber || 1}
                </Text>
              </View>
            ) : null}

            <View style={styles.metaChip}>
              <Text style={styles.metaChipText}>{task.serverName}</Text>
            </View>
          </View>

          <Text style={styles.title}>{task.title}</Text>

          {task.description ? (
            <Text style={styles.description}>{task.description}</Text>
          ) : (
            <Text style={styles.descriptionPlaceholder}>
              Add more context here if the task needs a stronger brief for delegation.
            </Text>
          )}

          <View style={styles.stateCard}>
            <Text style={styles.stateLabel}>Current state</Text>
            <Text style={[styles.stateTitle, { color: summary.tone }]}>{summary.title}</Text>
            <Text style={styles.stateBody}>{summary.body}</Text>

            {currentRun ? (
              <Text style={styles.stateMeta}>
                {buildRunMeta(currentRun)}
                {currentRun.agentSessionId ? ` · ${currentRun.agentSessionId}` : ''}
              </Text>
            ) : null}

            {currentRun?.agentSessionId ? (
              <View style={styles.linkedSessionRow}>
                <Ionicons
                  name="terminal-outline"
                  size={15}
                  color={currentAgent ? Colors.accent : Colors.textSecondary}
                />
                <Text style={styles.linkedSessionText} numberOfLines={1}>
                  {currentAgent
                    ? `Live session · ${currentAgent.project?.trim() || currentAgent.name}`
                    : `Linked session · ${currentRun.agentSessionId}`}
                </Text>
                {currentAgent ? (
                  <TouchableOpacity
                    style={styles.inlineAction}
                    onPress={() => openSession(currentAgent.id)}
                    activeOpacity={0.82}
                  >
                    <Text style={styles.inlineActionText}>Open</Text>
                  </TouchableOpacity>
                ) : null}
              </View>
            ) : null}
          </View>

          {currentAgent?.last_output_lines?.length ? (
            <View style={styles.previewBlock}>
              <View style={styles.previewHeader}>
                <Text style={styles.previewLabel}>Live preview</Text>
                <Text style={styles.previewMeta}>{RUN_STATUS_LABEL[currentRun?.status || ''] || 'Live'}</Text>
              </View>
              <View style={styles.previewFrame}>
                <TerminalPreview lines={currentAgent.last_output_lines} />
              </View>
            </View>
          ) : null}

          <View style={styles.heroActions}>
            {canDelegate ? (
              <TouchableOpacity
                style={[styles.heroActionBtn, styles.heroActionBtnPrimary, delegating && styles.disabled]}
                onPress={() => setDelegateSheetVisible(true)}
                disabled={delegating}
                activeOpacity={0.82}
              >
                <Ionicons name="play" size={16} color={Colors.bgPrimary} />
                <Text style={[styles.heroActionText, styles.heroActionTextPrimary]}>
                  {delegating ? 'Delegating...' : delegateActionLabel}
                </Text>
              </TouchableOpacity>
            ) : null}

            {currentAgent ? (
              <TouchableOpacity
                style={styles.heroActionBtn}
                onPress={() => openSession(currentAgent.id)}
                activeOpacity={0.82}
              >
                <Ionicons name="terminal-outline" size={16} color={Colors.textPrimary} />
                <Text style={styles.heroActionText}>Open Live Session</Text>
              </TouchableOpacity>
            ) : null}
          </View>

          <Text style={styles.heroHint}>
            Use this page to understand work state and delegation history. Use the session view when you need runtime control.
          </Text>
        </View>

        <View style={styles.section}>
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionTitle}>Activity</Text>
            <Text style={styles.sectionMeta}>{runsForTask.length === 0 ? 'Planning only' : runCountLabel}</Text>
          </View>

          {runsForTask.length === 0 ? (
            <View style={styles.emptyCard}>
              <Text style={styles.emptyTitle}>No runs yet</Text>
              <Text style={styles.emptyBody}>
                Tasks do not need to create a session immediately. Delegate when you are ready, or attach work to an existing live session.
              </Text>
              {canDelegate ? (
                <TouchableOpacity
                  style={styles.emptyActionBtn}
                  onPress={() => setDelegateSheetVisible(true)}
                  activeOpacity={0.82}
                >
                  <Text style={styles.emptyActionText}>Delegate this task</Text>
                </TouchableOpacity>
              ) : null}
            </View>
          ) : null}

          <View style={styles.timeline}>
            {timelineItems.map((item, index) => (
              <View key={item.key} style={styles.timelineRow}>
                <View style={styles.timelineRail}>
                  <View style={[styles.timelineDot, { backgroundColor: item.tone }]} />
                  {index < timelineItems.length - 1 ? <View style={styles.timelineLine} /> : null}
                </View>

                <View style={styles.timelineCard}>
                  <View style={styles.timelineTopRow}>
                    <Text style={styles.timelineEyebrow}>{item.eyebrow}</Text>
                    <Text style={styles.timelineTime}>{formatTimestamp(item.timestamp)}</Text>
                  </View>

                  <Text style={styles.timelineTitle}>{item.title}</Text>

                  {item.meta ? <Text style={styles.timelineMeta}>{item.meta}</Text> : null}
                  {item.body ? <Text style={styles.timelineBody}>{item.body}</Text> : null}

                  {item.note ? (
                    <View style={styles.timelineNote}>
                      <Ionicons name="information-circle-outline" size={15} color={item.tone} />
                      <Text style={styles.timelineNoteText}>{item.note}</Text>
                    </View>
                  ) : null}

                  {item.sessionId ? (
                    <View style={styles.timelineSessionRow}>
                      <Ionicons
                        name="terminal-outline"
                        size={14}
                        color={item.sessionIsLive ? Colors.accent : Colors.textSecondary}
                      />
                      <Text
                        style={[
                          styles.timelineSessionText,
                          !item.sessionIsLive && styles.timelineSessionTextMuted,
                        ]}
                        numberOfLines={1}
                      >
                        {item.sessionIsLive
                          ? `Live session · ${item.sessionLabel}`
                          : `Linked session · ${item.sessionLabel}`}
                      </Text>
                      {item.sessionIsLive ? (
                        <TouchableOpacity
                          style={styles.inlineAction}
                          onPress={() => openSession(item.sessionId!)}
                          activeOpacity={0.82}
                        >
                          <Text style={styles.inlineActionText}>Open</Text>
                        </TouchableOpacity>
                      ) : null}
                    </View>
                  ) : null}

                  {item.sessionPreviewLines?.length ? (
                    <View style={styles.timelinePreview}>
                      <TerminalPreview lines={item.sessionPreviewLines} />
                    </View>
                  ) : null}
                </View>
              </View>
            ))}
          </View>
        </View>

        <View style={styles.section}>
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionTitle}>Details</Text>
            <Text style={styles.sectionMeta}>Secondary properties</Text>
          </View>

          <View style={styles.detailsCard}>
            <View style={styles.detailRow}>
              <Text style={styles.detailLabel}>Priority</Text>
              <PriorityPicker value={task.priority} onChange={handlePriorityChange} />
            </View>

            <View style={styles.detailDivider} />

            <View style={styles.detailRow}>
              <Text style={styles.detailLabel}>Server</Text>
              <Text style={styles.detailValue}>{task.serverName}</Text>
            </View>

            <View style={styles.detailDivider} />

            <View style={styles.detailRow}>
              <Text style={styles.detailLabel}>Created</Text>
              <Text style={styles.detailValue}>{formatTimestamp(task.createdAt)}</Text>
            </View>

            <View style={styles.detailDivider} />

            <View style={styles.detailRow}>
              <Text style={styles.detailLabel}>Updated</Text>
              <Text style={styles.detailValue}>{formatTimestamp(task.updatedAt)}</Text>
            </View>

            {latestRun ? (
              <>
                <View style={styles.detailDivider} />
                <View style={styles.detailRow}>
                  <Text style={styles.detailLabel}>Latest run</Text>
                  <Text style={styles.detailValue}>
                    Attempt {latestRun.attemptNumber || 1} · {RUN_STATUS_LABEL[latestRun.status] || latestRun.status}
                  </Text>
                </View>
              </>
            ) : null}

            {currentRun?.agentSessionId && !currentAgent ? (
              <>
                <View style={styles.detailDivider} />
                <View style={styles.detailRow}>
                  <Text style={styles.detailLabel}>Linked session</Text>
                  <Text style={styles.detailValueMono}>{currentRun.agentSessionId}</Text>
                </View>
              </>
            ) : null}

            {currentAgent?.cwd ? (
              <>
                <View style={styles.detailDivider} />
                <View style={styles.detailRow}>
                  <Text style={styles.detailLabel}>Working dir</Text>
                  <Text style={styles.detailValueMono} numberOfLines={2}>{currentAgent.cwd}</Text>
                </View>
              </>
            ) : null}

            {task.labels.length > 0 ? (
              <>
                <View style={styles.detailDivider} />
                <View style={[styles.detailRow, styles.detailRowTop]}>
                  <Text style={styles.detailLabel}>Labels</Text>
                  <View style={styles.labelsRow}>
                    {task.labels.map(label => (
                      <View key={label} style={styles.labelChip}>
                        <Text style={styles.labelText}>{label}</Text>
                      </View>
                    ))}
                  </View>
                </View>
              </>
            ) : null}
          </View>
        </View>

        <View style={styles.section}>
          <View style={styles.sectionHeader}>
            <Text style={styles.sectionTitle}>Workflow</Text>
            <Text style={styles.sectionMeta}>Quick changes</Text>
          </View>

          <View style={styles.actions}>
            {canMarkDone ? (
              <TouchableOpacity
                style={[styles.actionBtn, styles.actionBtnPrimary]}
                onPress={() => handleStatusChange('done')}
                activeOpacity={0.82}
              >
                <Ionicons name="checkmark" size={16} color={Colors.bgPrimary} />
                <Text style={[styles.actionText, styles.actionTextPrimary]}>Mark Done</Text>
              </TouchableOpacity>
            ) : null}

            {task.status !== 'cancelled' && task.status !== 'done' ? (
              <TouchableOpacity
                style={styles.actionBtn}
                onPress={() => handleStatusChange('cancelled')}
                activeOpacity={0.82}
              >
                <Ionicons name="close" size={16} color={Colors.textSecondary} />
                <Text style={[styles.actionText, { color: Colors.textSecondary }]}>Cancel</Text>
              </TouchableOpacity>
            ) : null}

            <TouchableOpacity style={styles.actionBtn} onPress={handleDelete} activeOpacity={0.82}>
              <Ionicons name="trash-outline" size={16} color="#F09999" />
              <Text style={[styles.actionText, { color: '#F09999' }]}>Delete</Text>
            </TouchableOpacity>
          </View>
        </View>
      </ScrollView>

      <StatusPicker
        visible={statusPickerVisible}
        current={task.status}
        onSelect={handleStatusChange}
        onClose={() => setStatusPickerVisible(false)}
      />

      <DelegateRunSheet
        visible={delegateSheetVisible}
        busy={delegating}
        sessions={delegateSessionOptions}
        onClose={() => setDelegateSheetVisible(false)}
        onStartNew={() => { void handleStartNewRun(); }}
        onAttach={(sessionId) => { void handleAttachSession(sessionId); }}
      />
    </SafeAreaView>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: Colors.bgPrimary,
  },
  header: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 12,
    paddingHorizontal: 16,
    paddingVertical: 12,
  },
  headerTitle: {
    color: Colors.textSecondary,
    fontSize: 14,
    fontFamily: Typography.terminalFont,
  },
  content: {
    flex: 1,
  },
  contentInner: {
    paddingHorizontal: 20,
    paddingBottom: 40,
    gap: 22,
  },
  heroCard: {
    borderRadius: 22,
    backgroundColor: Colors.bgSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    paddingHorizontal: 18,
    paddingVertical: 18,
    gap: 16,
  },
  heroMetaRow: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    alignItems: 'center',
    gap: 8,
  },
  statusBadge: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    alignSelf: 'flex-start',
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
  },
  statusBadgeText: {
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  metaChip: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    minHeight: 30,
    paddingHorizontal: 10,
    borderRadius: 999,
    backgroundColor: 'rgba(255,255,255,0.05)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
  },
  metaChipDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  metaChipText: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  title: {
    color: Colors.textPrimary,
    fontSize: 22,
    lineHeight: 30,
    fontFamily: Typography.uiFontMedium,
  },
  description: {
    color: Colors.textSecondary,
    fontSize: 14,
    lineHeight: 21,
    fontFamily: Typography.uiFont,
  },
  descriptionPlaceholder: {
    color: Colors.textSecondary,
    fontSize: 14,
    lineHeight: 21,
    fontFamily: Typography.uiFont,
    opacity: 0.55,
  },
  stateCard: {
    borderRadius: 18,
    backgroundColor: 'rgba(255,255,255,0.04)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    paddingHorizontal: 14,
    paddingVertical: 14,
    gap: 8,
  },
  stateLabel: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase',
    letterSpacing: 0.6,
    opacity: 0.7,
  },
  stateTitle: {
    fontSize: 18,
    lineHeight: 24,
    fontFamily: Typography.uiFontMedium,
  },
  stateBody: {
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
  },
  stateMeta: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
    opacity: 0.85,
  },
  linkedSessionRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    marginTop: 4,
  },
  linkedSessionText: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  inlineAction: {
    paddingHorizontal: 10,
    paddingVertical: 6,
    borderRadius: 999,
    backgroundColor: 'rgba(91,157,255,0.12)',
  },
  inlineActionText: {
    color: Colors.accent,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  previewBlock: {
    gap: 8,
  },
  previewHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
  },
  previewLabel: {
    color: Colors.textPrimary,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  previewMeta: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  previewFrame: {
    height: 120,
  },
  heroActions: {
    flexDirection: 'row',
    flexWrap: 'wrap',
    gap: 10,
  },
  heroActionBtn: {
    minHeight: 44,
    paddingHorizontal: 14,
    borderRadius: 14,
    backgroundColor: 'rgba(255,255,255,0.06)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.1)',
    alignItems: 'center',
    justifyContent: 'center',
    flexDirection: 'row',
    gap: 8,
  },
  heroActionBtnPrimary: {
    backgroundColor: Colors.accent,
    borderColor: Colors.accent,
  },
  heroActionText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  heroActionTextPrimary: {
    color: Colors.bgPrimary,
  },
  heroHint: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
    opacity: 0.72,
  },
  section: {
    gap: 12,
  },
  sectionHeader: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 10,
  },
  sectionTitle: {
    color: Colors.textPrimary,
    fontSize: 16,
    fontFamily: Typography.uiFontMedium,
  },
  sectionMeta: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  emptyCard: {
    borderRadius: 18,
    backgroundColor: Colors.bgSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    paddingHorizontal: 16,
    paddingVertical: 16,
    gap: 8,
  },
  emptyTitle: {
    color: Colors.textPrimary,
    fontSize: 15,
    fontFamily: Typography.uiFontMedium,
  },
  emptyBody: {
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
  },
  emptyActionBtn: {
    alignSelf: 'flex-start',
    marginTop: 4,
    paddingHorizontal: 14,
    minHeight: 38,
    borderRadius: 12,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: 'rgba(91,157,255,0.12)',
  },
  emptyActionText: {
    color: Colors.accent,
    fontSize: 13,
    fontFamily: Typography.uiFontMedium,
  },
  timeline: {
    gap: 0,
  },
  timelineRow: {
    flexDirection: 'row',
    gap: 12,
  },
  timelineRail: {
    width: 16,
    alignItems: 'center',
  },
  timelineDot: {
    width: 10,
    height: 10,
    borderRadius: 5,
    marginTop: 8,
  },
  timelineLine: {
    width: 1.5,
    flex: 1,
    backgroundColor: 'rgba(255,255,255,0.12)',
    marginTop: 6,
  },
  timelineCard: {
    flex: 1,
    marginBottom: 14,
    borderRadius: 18,
    backgroundColor: Colors.bgSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    paddingHorizontal: 14,
    paddingVertical: 14,
    gap: 8,
  },
  timelineTopRow: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 12,
  },
  timelineEyebrow: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase',
    letterSpacing: 0.6,
  },
  timelineTime: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.uiFont,
  },
  timelineTitle: {
    color: Colors.textPrimary,
    fontSize: 16,
    lineHeight: 22,
    fontFamily: Typography.uiFontMedium,
  },
  timelineMeta: {
    color: Colors.textSecondary,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
    opacity: 0.85,
  },
  timelineBody: {
    color: Colors.textSecondary,
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
  },
  timelineNote: {
    flexDirection: 'row',
    alignItems: 'flex-start',
    gap: 8,
    paddingHorizontal: 10,
    paddingVertical: 10,
    borderRadius: 12,
    backgroundColor: 'rgba(255,255,255,0.04)',
  },
  timelineNoteText: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.uiFont,
  },
  timelineSessionRow: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
  },
  timelineSessionText: {
    flex: 1,
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  timelineSessionTextMuted: {
    color: Colors.textSecondary,
    fontFamily: Typography.terminalFont,
  },
  timelinePreview: {
    height: 96,
  },
  detailsCard: {
    borderRadius: 18,
    backgroundColor: Colors.bgSurface,
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
    overflow: 'hidden',
  },
  detailRow: {
    minHeight: 56,
    paddingHorizontal: 14,
    paddingVertical: 12,
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'space-between',
    gap: 14,
  },
  detailRowTop: {
    alignItems: 'flex-start',
  },
  detailDivider: {
    height: StyleSheet.hairlineWidth,
    backgroundColor: 'rgba(255,255,255,0.08)',
  },
  detailLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
    textTransform: 'uppercase',
    letterSpacing: 0.5,
    opacity: 0.8,
  },
  detailValue: {
    flex: 1,
    textAlign: 'right',
    color: Colors.textPrimary,
    fontSize: 14,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  detailValueMono: {
    flex: 1,
    textAlign: 'right',
    color: Colors.textPrimary,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.terminalFont,
  },
  labelsRow: {
    flex: 1,
    flexDirection: 'row',
    flexWrap: 'wrap',
    justifyContent: 'flex-end',
    gap: 6,
  },
  labelChip: {
    paddingHorizontal: 8,
    paddingVertical: 4,
    borderRadius: 999,
    backgroundColor: 'rgba(255,255,255,0.06)',
  },
  labelText: {
    color: Colors.textPrimary,
    fontSize: 12,
    fontFamily: Typography.uiFont,
  },
  actions: {
    gap: 10,
  },
  actionBtn: {
    flexDirection: 'row',
    alignItems: 'center',
    justifyContent: 'center',
    gap: 8,
    minHeight: 44,
    borderRadius: 14,
    backgroundColor: 'rgba(255,255,255,0.06)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.1)',
  },
  actionBtnPrimary: {
    backgroundColor: Colors.accent,
    borderColor: Colors.accent,
  },
  actionText: {
    color: Colors.textPrimary,
    fontSize: 14,
    fontFamily: Typography.uiFontMedium,
  },
  actionTextPrimary: {
    color: Colors.bgPrimary,
  },
  disabled: {
    opacity: 0.45,
  },
});
