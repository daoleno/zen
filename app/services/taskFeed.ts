import { Colors, issueStatusColor, runStatusColor } from '../constants/tokens';
import type { Agent } from '../store/agents';
import type { Run, Task } from '../store/tasks';
import { describeDueDate } from './dueDate';

export type TaskListSectionKey = 'active' | 'backlog' | 'done';

export const TASK_STATUS_LABEL: Record<string, string> = {
  backlog: 'Backlog',
  todo: 'Todo',
  in_progress: 'In Progress',
  done: 'Done',
  cancelled: 'Cancelled',
};

export const RUN_STATUS_LABEL: Record<string, string> = {
  queued: 'Queued',
  running: 'Running',
  blocked: 'Blocked',
  done: 'Done',
  failed: 'Failed',
  cancelled: 'Stopped',
};

export function isActiveRunStatus(status?: string) {
  return status === 'queued' || status === 'running' || status === 'blocked';
}

export function normalizeCopy(text?: string) {
  if (!text) {
    return '';
  }

  return text.replace(/\s+/g, ' ').trim();
}

export function getRunMoment(run?: Run | null) {
  if (!run) {
    return 0;
  }

  return run.updatedAt || run.endedAt || run.startedAt || run.createdAt || 0;
}

export function pickCurrentRun(task: Task, runs: Run[]) {
  if (task.currentRunId) {
    const exact = runs.find(run => run.id === task.currentRunId);
    if (exact) {
      return exact;
    }
  }

  const active = runs.find(run => isActiveRunStatus(run.status));
  return active || runs[0] || null;
}

export function getTaskSectionKey(task: Task, run?: Run | null): TaskListSectionKey {
  const activeStatus = run?.status || task.lastRunStatus;

  if (activeStatus === 'blocked' || activeStatus === 'failed') {
    return 'active';
  }

  if (task.status === 'backlog') {
    return 'backlog';
  }

  if (task.status === 'done' || task.status === 'cancelled') {
    return 'done';
  }

  return 'active';
}

export function getTaskStatusPresentation(task: Task, run?: Run | null) {
  if (run) {
    switch (run.status) {
      case 'blocked':
        return { label: 'Blocked', tone: runStatusColor(run.status) };
      case 'failed':
        return { label: 'Failed', tone: runStatusColor(run.status) };
      case 'running':
        return { label: 'Running', tone: runStatusColor(run.status) };
      case 'queued':
        return { label: 'Queued', tone: runStatusColor(run.status) };
      case 'done':
        return task.status === 'done'
          ? { label: 'Done', tone: issueStatusColor('done') }
          : { label: 'Review', tone: Colors.accent };
      case 'cancelled':
        return { label: 'Stopped', tone: Colors.textSecondary };
      default:
        break;
    }
  }

  return {
    label: TASK_STATUS_LABEL[task.status] || task.status,
    tone: issueStatusColor(task.status),
  };
}

export function getTaskSecondaryText(task: Task, run?: Run | null, agent?: Agent | null) {
  const candidates = [
    run?.waitingReason,
    run?.lastError,
    agent?.summary,
    run?.summary,
    run?.status === 'queued' ? 'Ready to start on the linked session.' : undefined,
    run?.status === 'running' ? 'Agent is actively working.' : undefined,
    run?.status === 'blocked' ? 'Execution paused and needs your input.' : undefined,
    run?.status === 'failed' ? 'Execution stopped because of an error.' : undefined,
    run?.status === 'done' && task.status !== 'done' ? 'Latest run finished. Review before closing.' : undefined,
    run?.executionMode === 'attach_existing_session' ? 'Attached to an existing live session.' : undefined,
    task.dueDate ? describeDueDate(task.dueDate) : undefined,
    task.status === 'backlog' ? 'Not delegated yet.' : undefined,
    task.status === 'todo' ? 'Ready for delegation.' : undefined,
    task.status === 'in_progress' ? 'Marked active without a live run.' : undefined,
    task.status === 'done' ? 'Closed and ready for historical reference.' : undefined,
    task.status === 'cancelled' ? 'Cancelled before completion.' : undefined,
    task.description,
  ];

  for (const candidate of candidates) {
    const normalized = normalizeCopy(candidate);
    if (normalized) {
      return normalized;
    }
  }

  return 'No extra context yet.';
}

export function getTaskSortRank(sectionKey: TaskListSectionKey, task: Task, run?: Run | null) {
  switch (sectionKey) {
    case 'active':
      if (run?.status === 'blocked') return 0;
      if (run?.status === 'failed') return 1;
      if (run?.status === 'running') return 2;
      if (run?.status === 'queued') return 3;
      if (run?.status === 'done' && task.status !== 'done' && task.status !== 'cancelled') return 4;
      if (task.status === 'in_progress') return 5;
      if (task.status === 'todo') return 6;
      return 7;
    case 'backlog':
      return 0;
    case 'done':
      return task.status === 'done' ? 0 : 1;
    default:
      return 9;
  }
}
