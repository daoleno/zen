export type PrimaryPageActionOwnerId = string;

export interface PrimaryPageActionRegistration<T> {
  content: T;
  ownerId: PrimaryPageActionOwnerId;
}

export function registerPrimaryPageAction<T>(
  _current: PrimaryPageActionRegistration<T> | null,
  ownerId: PrimaryPageActionOwnerId,
  content: T,
): PrimaryPageActionRegistration<T> {
  return { ownerId, content };
}

export function clearPrimaryPageAction<T>(
  current: PrimaryPageActionRegistration<T> | null,
  ownerId: PrimaryPageActionOwnerId,
): PrimaryPageActionRegistration<T> | null {
  if (current == null || current.ownerId !== ownerId) {
    return current;
  }
  return null;
}
