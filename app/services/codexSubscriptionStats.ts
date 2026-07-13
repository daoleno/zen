export function normalizeCodexUsedPercent(value: number): number {
  if (!Number.isFinite(value)) return 0;
  return Math.min(100, Math.max(0, value));
}

export function codexRemainingPercent(usedPercent: number): number {
  return 100 - normalizeCodexUsedPercent(usedPercent);
}

export function codexWindowLabel(windowMinutes?: number): string {
  if (!windowMinutes) return 'Usage window';
  if (windowMinutes % (7 * 24 * 60) === 0) return unitLabel(windowMinutes / (7 * 24 * 60), 'week');
  if (windowMinutes % (24 * 60) === 0) return unitLabel(windowMinutes / (24 * 60), 'day');
  if (windowMinutes % 60 === 0) return unitLabel(windowMinutes / 60, 'hour');
  return `${windowMinutes} min`;
}

function unitLabel(value: number, unit: string): string {
  return `${value} ${unit}${value === 1 ? '' : 's'}`;
}

export function codexPlanLabel(plan?: string): string {
  if (!plan) return 'ChatGPT plan';
  const known: Record<string, string> = {
    free: 'ChatGPT Free', plus: 'ChatGPT Plus', pro: 'ChatGPT Pro',
    team: 'ChatGPT Team', business: 'ChatGPT Business', enterprise: 'ChatGPT Enterprise',
    edu: 'ChatGPT Edu', education: 'ChatGPT Edu', go: 'ChatGPT Go',
  };
  return known[plan.toLowerCase()] ?? 'ChatGPT plan';
}

export function codexAuthSummary(authKind: string): string {
  switch (authKind) {
    case 'official': return 'ChatGPT plan';
    case 'api_key': return 'API key authentication';
    case 'absent': return 'Not signed in';
    default: return 'Authentication unavailable';
  }
}

export function isOfficialCodexSubscription(
  subscription: { authKind?: unknown } | null | undefined,
): subscription is { authKind: 'official' } {
  return subscription?.authKind === 'official';
}
