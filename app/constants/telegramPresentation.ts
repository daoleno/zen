import { TELEGRAM_AVATAR_COLORS } from '../theme/definitions/shared';

export function avatarColorForSeed(
  seed: string,
  palette: readonly string[] = TELEGRAM_AVATAR_COLORS,
): string {
  if (palette.length === 0) {
    return '#65AADD';
  }
  let hash = 0;
  for (let i = 0; i < seed.length; i += 1) {
    hash = seed.charCodeAt(i) + ((hash << 5) - hash);
  }
  return palette[Math.abs(hash) % palette.length];
}

export function initialsFromLabel(label: string): string {
  const trimmed = label.trim();
  if (!trimmed) {
    return '?';
  }
  const words = trimmed.split(/\s+/).filter(Boolean);
  if (words.length >= 2) {
    return `${words[0][0] ?? ''}${words[1][0] ?? ''}`.toUpperCase();
  }
  if (trimmed.length >= 2) {
    return trimmed.slice(0, 2).toUpperCase();
  }
  return trimmed[0]?.toUpperCase() ?? '?';
}

/** Compact bubble time — HH:mm today, short label otherwise. */
export function formatChatBubbleTime(timestamp?: string): string {
  if (!timestamp?.trim()) {
    return '';
  }
  const date = new Date(timestamp);
  if (!Number.isFinite(date.getTime())) {
    return '';
  }
  return formatTelegramListTime(date.getTime());
}

export function formatTelegramListTime(timestamp?: number): string {
  if (!timestamp) {
    return '';
  }
  const date = new Date(timestamp);
  if (!Number.isFinite(date.getTime())) {
    return '';
  }

  const now = new Date();
  const sameDay = date.toDateString() === now.toDateString();
  if (sameDay) {
    return date.toLocaleTimeString([], {
      hour: '2-digit',
      minute: '2-digit',
      hour12: false,
    });
  }

  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString()) {
    return 'Yesterday';
  }

  const diffMs = now.getTime() - date.getTime();
  if (diffMs < 7 * 86_400_000) {
    return date.toLocaleDateString([], { weekday: 'short' });
  }

  const sameYear = date.getFullYear() === now.getFullYear();
  return date.toLocaleDateString(
    [],
    sameYear
      ? { month: 'short', day: '2-digit' }
      : { month: 'short', day: '2-digit', year: '2-digit' },
  );
}

export function formatDateDividerLabel(timestamp?: string): string | null {
  if (!timestamp) {
    return null;
  }
  const date = new Date(timestamp);
  if (!Number.isFinite(date.getTime())) {
    return null;
  }

  const now = new Date();
  if (date.toDateString() === now.toDateString()) {
    return 'Today';
  }

  const yesterday = new Date(now);
  yesterday.setDate(yesterday.getDate() - 1);
  if (date.toDateString() === yesterday.toDateString()) {
    return 'Yesterday';
  }

  const sameYear = date.getFullYear() === now.getFullYear();
  return date.toLocaleDateString(
    [],
    sameYear
      ? { month: 'long', day: 'numeric' }
      : { month: 'long', day: 'numeric', year: 'numeric' },
  );
}

export function dayKeyFromTimestamp(timestamp?: string): string | null {
  if (!timestamp) {
    return null;
  }
  const date = new Date(timestamp);
  if (!Number.isFinite(date.getTime())) {
    return null;
  }
  return date.toDateString();
}