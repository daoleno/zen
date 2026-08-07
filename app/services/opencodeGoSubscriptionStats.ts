export const OPENCODE_GO_PLAN_LABEL = 'OpenCode Go';

export interface OpenCodeGoSubscriptionProjection {
  authKind: 'official';
  state: 'available';
  plan?: string;
  fetchedAt?: string;
  usageAvailable?: boolean;
  windows?: OpenCodeGoWindowProjection[];
  serverLabel?: string;
}

export interface OpenCodeGoWindowProjection {
  name: string;
  usedPercent: number;
  limitUsd?: number;
  resetInSeconds?: number;
  resetsAt?: string;
}

export function isOfficialOpenCodeGoSubscription(
  subscription: { authKind?: unknown; state?: unknown } | null | undefined,
): subscription is OpenCodeGoSubscriptionProjection {
  return subscription?.authKind === 'official' && subscription?.state === 'available';
}

export function openCodeGoWindowLabel(name: string): string {
  switch (name) {
    case 'rolling': return '5 hours';
    case 'weekly': return 'Weekly';
    case 'monthly': return 'Monthly';
    default: return 'Usage window';
  }
}

export function openCodeGoLimitLabel(limitUsd?: number): string {
  if (!Number.isFinite(limitUsd)) return '';
  return `$${limitUsd}`;
}
