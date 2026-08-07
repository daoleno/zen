export const OPENCODE_GO_PLAN_LABEL = 'OpenCode Go';

export interface OpenCodeGoSubscriptionProjection {
  authKind: 'official';
  state: 'available';
  plan?: string;
  fetchedAt?: string;
  serverLabel?: string;
}

export function isOfficialOpenCodeGoSubscription(
  subscription: { authKind?: unknown; state?: unknown } | null | undefined,
): subscription is OpenCodeGoSubscriptionProjection {
  return subscription?.authKind === 'official' && subscription?.state === 'available';
}
