import { buildChatChrome } from '../theme/buildChatChrome';
import type { ResolvedZenTheme } from '../theme/types';

/** @deprecated Use buildChatChrome(theme) from app/theme. */
export function buildBrainChatChrome(theme: ResolvedZenTheme) {
  return buildChatChrome(theme);
}