export type PrimarySelectionBarOwnerId = string;

export interface PrimarySelectionBarRegistration<T> {
  content: T;
  ownerId: PrimarySelectionBarOwnerId;
}

/**
 * Owner-scoped registration for the full-width app-bar replacement shown
 * during list selection mode (Cancel + count + Terminate). Mirrors the
 * PrimaryPageAction registration rules: the most recently registered owner
 * wins, and clear only removes the matching owner.
 */
export function registerPrimarySelectionBar<T>(
  _current: PrimarySelectionBarRegistration<T> | null,
  ownerId: PrimarySelectionBarOwnerId,
  content: T,
): PrimarySelectionBarRegistration<T> {
  return { ownerId, content };
}

export function clearPrimarySelectionBar<T>(
  current: PrimarySelectionBarRegistration<T> | null,
  ownerId: PrimarySelectionBarOwnerId,
): PrimarySelectionBarRegistration<T> | null {
  if (current == null || current.ownerId !== ownerId) {
    return current;
  }
  return null;
}
