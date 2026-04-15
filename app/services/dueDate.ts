export type DueDateOption = {
  value: string;
  label: string;
  meta: string;
};

function pad(value: number) {
  return value.toString().padStart(2, '0');
}

function startOfDay(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

function addDays(date: Date, days: number) {
  const next = new Date(date);
  next.setDate(next.getDate() + days);
  return next;
}

export function toDueDateString(date: Date) {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

export function parseDueDate(value?: string | null) {
  if (!value) {
    return null;
  }

  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value.trim());
  if (!match) {
    return null;
  }

  const year = Number(match[1]);
  const month = Number(match[2]);
  const day = Number(match[3]);
  const date = new Date(year, month - 1, day);

  if (
    date.getFullYear() !== year
    || date.getMonth() !== month - 1
    || date.getDate() !== day
  ) {
    return null;
  }

  return date;
}

export function getDueDateState(value?: string | null, now = new Date()) {
  const date = parseDueDate(value);
  if (!date) {
    return null;
  }

  const due = startOfDay(date);
  const today = startOfDay(now);
  const diffMs = due.getTime() - today.getTime();
  const dayDiff = Math.round(diffMs / 86_400_000);

  return {
    date: due,
    dayDiff,
    isOverdue: dayDiff < 0,
    isToday: dayDiff === 0,
    isTomorrow: dayDiff === 1,
  };
}

export function formatDueDateShort(value?: string | null, now = new Date()) {
  const state = getDueDateState(value, now);
  if (!state) {
    return 'Not set';
  }

  if (state.isToday) {
    return 'Today';
  }
  if (state.isTomorrow) {
    return 'Tomorrow';
  }
  if (state.dayDiff === -1) {
    return 'Yesterday';
  }

  return state.date.toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
  });
}

export function formatDueDateLong(value?: string | null, now = new Date()) {
  const state = getDueDateState(value, now);
  if (!state) {
    return 'Not set';
  }

  if (state.isToday) {
    return 'Today';
  }
  if (state.isTomorrow) {
    return 'Tomorrow';
  }

  return state.date.toLocaleDateString(undefined, {
    weekday: 'short',
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  });
}

export function describeDueDate(value?: string | null, now = new Date()) {
  const state = getDueDateState(value, now);
  if (!state) {
    return '';
  }

  if (state.isOverdue) {
    return `Overdue since ${formatDueDateLong(value, now)}`;
  }
  if (state.isToday) {
    return 'Due today';
  }
  if (state.isTomorrow) {
    return 'Due tomorrow';
  }

  return `Due ${formatDueDateLong(value, now)}`;
}

export function buildDueDateOptions(days = 21, now = new Date()): DueDateOption[] {
  const today = startOfDay(now);

  return Array.from({ length: days }, (_, index) => {
    const date = addDays(today, index);
    const value = toDueDateString(date);

    let label = date.toLocaleDateString(undefined, { weekday: 'short' });
    if (index === 0) {
      label = 'Today';
    } else if (index === 1) {
      label = 'Tomorrow';
    }

    return {
      value,
      label,
      meta: date.toLocaleDateString(undefined, {
        month: 'short',
        day: 'numeric',
      }),
    };
  });
}
