import type { ZenThemeDefinition } from '../types';
import {
  ZEN_DARK_APP_COLORS,
  ZEN_DARK_CHAT_PALETTE,
  ZEN_DARK_DATA_VISUALIZATION,
  ZEN_DARK_SURFACE_PALETTE,
} from '../primitives';
import { TELEGRAM_AVATAR_COLORS } from './shared';

/** Logo-derived dark theme with solid graphite surfaces and sage accents. */
export const classicDarkTheme: ZenThemeDefinition = {
  id: 'classic-dark',
  name: 'Classic Night',
  colorScheme: 'dark',
  colors: ZEN_DARK_APP_COLORS,
  chat: ZEN_DARK_CHAT_PALETTE,
  surfaces: ZEN_DARK_SURFACE_PALETTE,
  dataVisualization: ZEN_DARK_DATA_VISUALIZATION,
  avatarColors: TELEGRAM_AVATAR_COLORS,
};
