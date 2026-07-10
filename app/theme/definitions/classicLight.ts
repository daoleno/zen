import type { ZenThemeDefinition } from '../types';
import {
  ZEN_LIGHT_APP_COLORS,
  ZEN_LIGHT_CHAT_PALETTE,
  ZEN_LIGHT_DATA_VISUALIZATION,
  ZEN_LIGHT_SURFACE_PALETTE,
} from '../primitives';
import { TELEGRAM_AVATAR_COLORS } from './shared';

/** Logo-derived light theme with a crisp neutral canvas and sage accents. */
export const classicLightTheme: ZenThemeDefinition = {
  id: 'classic-light',
  name: 'Classic Day',
  colorScheme: 'light',
  colors: ZEN_LIGHT_APP_COLORS,
  chat: ZEN_LIGHT_CHAT_PALETTE,
  surfaces: ZEN_LIGHT_SURFACE_PALETTE,
  dataVisualization: ZEN_LIGHT_DATA_VISUALIZATION,
  avatarColors: TELEGRAM_AVATAR_COLORS,
};
