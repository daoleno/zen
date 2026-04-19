export type TaskStateField = {
  label: string;
  value: string;
};

export type TaskStateSection = {
  body: string;
  items: string[];
};

export type TaskStateMachineStatus = {
  updated?: string;
  taskStatus?: string;
  runStatus?: string;
  runAttempt?: number;
  workspace?: string;
  session?: string;
  summary?: string;
  fields: TaskStateField[];
};

export type TaskStateSnapshot = {
  available: boolean;
  path?: string;
  title?: string;
  goal: TaskStateSection;
  machineStatus: TaskStateMachineStatus;
  completed: TaskStateSection;
  blockers: TaskStateSection;
  nextStep: TaskStateSection;
};

function normalizeSection(raw: any): TaskStateSection {
  return {
    body: typeof raw?.body === "string" ? raw.body : "",
    items: Array.isArray(raw?.items)
      ? raw.items.filter((item: unknown): item is string => typeof item === "string")
      : [],
  };
}

export function normalizeTaskStateSnapshot(raw: any): TaskStateSnapshot {
  const fields = Array.isArray(raw?.machine_status?.fields)
    ? raw.machine_status.fields
        .filter(
          (field: unknown): field is { label?: unknown; value?: unknown } =>
            !!field && typeof field === "object",
        )
        .map((field: { label?: unknown; value?: unknown }) => ({
          label: typeof field.label === "string" ? field.label : "",
          value: typeof field.value === "string" ? field.value : "",
        }))
        .filter(
          (field: TaskStateField) => !!field.label && !!field.value,
        )
    : [];

  return {
    available: !!raw?.available,
    path: typeof raw?.path === "string" ? raw.path : "",
    title: typeof raw?.title === "string" ? raw.title : "",
    goal: normalizeSection(raw?.goal),
    machineStatus: {
      updated:
        typeof raw?.machine_status?.updated === "string"
          ? raw.machine_status.updated
          : "",
      taskStatus:
        typeof raw?.machine_status?.task_status === "string"
          ? raw.machine_status.task_status
          : "",
      runStatus:
        typeof raw?.machine_status?.run_status === "string"
          ? raw.machine_status.run_status
          : "",
      runAttempt:
        typeof raw?.machine_status?.run_attempt === "number"
          ? raw.machine_status.run_attempt
          : undefined,
      workspace:
        typeof raw?.machine_status?.workspace === "string"
          ? raw.machine_status.workspace
          : "",
      session:
        typeof raw?.machine_status?.session === "string"
          ? raw.machine_status.session
          : "",
      summary:
        typeof raw?.machine_status?.summary === "string"
          ? raw.machine_status.summary
          : "",
      fields,
    },
    completed: normalizeSection(raw?.completed),
    blockers: normalizeSection(raw?.blockers),
    nextStep: normalizeSection(raw?.next_step),
  };
}

export function taskStateSectionHasContent(section?: TaskStateSection | null) {
  if (!section) {
    return false;
  }

  return section.body.trim().length > 0 || section.items.length > 0;
}
