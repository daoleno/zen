import type { AppColors } from './palette';
import type {
  ChatPalette,
  DataVisualizationPalette,
  MaterialPalette,
  SurfacePalette,
} from './types';

const TRANSPARENT = 'transparent';

export const ZEN_SAGE = {
  50: '#F4F8F4',
  100: '#E4EDE5',
  200: '#CADCCD',
  300: '#A9C6AF',
  400: '#82A68A',
  500: '#628A6B',
  600: '#4A7154',
  700: '#3A5A43',
  800: '#2D4535',
  900: '#23362A',
  950: '#121C16',
} as const;

export const ZEN_BRAND_COLORS = {
  environment: '#0F0F14',
  sage: ZEN_SAGE[400],
  ivory: '#F2EEE5',
} as const;

// Grouped canvas with white content cards, the Apple layering model.
export const ZEN_LIGHT_NEUTRALS = {
  canvas: '#F2F3F0',
  surface: '#FFFFFF',
  elevated: '#EBEDE9',
  pressed: '#E1E4DF',
  textPrimary: '#121513',
  textSecondary: '#4B524D',
  textTertiary: '#646C66',
  borderSubtle: '#E3E6E1',
  border: '#D2D7D1',
  borderStrong: '#8A938C',
} as const;

export const ZEN_DARK_NEUTRALS = {
  surface: '#18191E',
  elevated: '#212329',
  subtle: '#15161B',
  active: '#1C2A22',
  pressed: '#2A2C32',
  textPrimary: '#F3F4F1',
  textSecondary: '#BCC2BD',
  textTertiary: '#8F9791',
  borderSubtle: '#25272C',
  border: '#33363C',
  borderStrong: '#666E68',
  modalSurfaceAlt: '#23252B',
} as const;

export const ZEN_LIGHT_STATUS = {
  danger: '#B42318',
  dangerSoft: '#FCE8E6',
  warning: '#8A4B00',
  warningSoft: '#F8ECD6',
  success: '#246B3D',
  successSoft: '#E3F2E7',
} as const;

export const ZEN_DARK_STATUS = {
  danger: '#FF8A80',
  dangerSoft: '#321B1B',
  warning: '#F5C26B',
  warningSoft: '#2C2618',
  success: '#75D39A',
  successSoft: '#162A1E',
} as const;

export const ZEN_LIGHT_OVERLAYS = {
  selection: 'rgba(74,113,84,0.18)',
  modalBackdrop: 'rgba(15,15,20,0.32)',
} as const;

export const ZEN_DARK_OVERLAYS = {
  selection: 'rgba(130,166,138,0.28)',
  modalBackdrop: 'rgba(0,0,0,0.6)',
} as const;

export const ZEN_LIGHT_APP_COLORS: AppColors = {
  bgPrimary: ZEN_LIGHT_NEUTRALS.canvas,
  bgSurface: ZEN_LIGHT_NEUTRALS.surface,
  bgElevated: ZEN_LIGHT_NEUTRALS.elevated,
  textPrimary: ZEN_LIGHT_NEUTRALS.textPrimary,
  textSecondary: ZEN_LIGHT_NEUTRALS.textSecondary,
  textTertiary: ZEN_LIGHT_NEUTRALS.textTertiary,
  accent: ZEN_SAGE[600],
  accentSoft: ZEN_SAGE[100],
  accentStrong: ZEN_SAGE[700],
  logoDetail: ZEN_SAGE[900],
  statusFailed: ZEN_LIGHT_STATUS.danger,
  statusBlocked: ZEN_LIGHT_STATUS.warning,
  statusUnknown: ZEN_LIGHT_NEUTRALS.textTertiary,
  statusRunning: ZEN_SAGE[600],
  statusDone: ZEN_LIGHT_STATUS.success,
  zenGreen: ZEN_LIGHT_STATUS.success,
  priorityUrgent: ZEN_LIGHT_STATUS.danger,
  priorityHigh: ZEN_LIGHT_STATUS.warning,
  priorityMedium: ZEN_SAGE[600],
  priorityLow: ZEN_LIGHT_NEUTRALS.textSecondary,
  border: ZEN_LIGHT_NEUTRALS.border,
  borderSubtle: ZEN_LIGHT_NEUTRALS.borderSubtle,
  borderStrong: ZEN_LIGHT_NEUTRALS.borderStrong,
  surfaceSubtle: ZEN_LIGHT_NEUTRALS.elevated,
  surfacePressed: ZEN_LIGHT_NEUTRALS.pressed,
  surfaceActive: ZEN_SAGE[100],
  inputBackground: ZEN_LIGHT_NEUTRALS.surface,
  disabledSurface: ZEN_LIGHT_NEUTRALS.pressed,
  modalBackdrop: ZEN_LIGHT_OVERLAYS.modalBackdrop,
  modalSurface: ZEN_LIGHT_NEUTRALS.surface,
  modalSurfaceAlt: ZEN_LIGHT_NEUTRALS.elevated,
  textOnAccent: ZEN_LIGHT_NEUTRALS.surface,
  focusRing: ZEN_SAGE[700],
  selectionBackground: ZEN_LIGHT_OVERLAYS.selection,
  promptGreen: ZEN_LIGHT_STATUS.success,
  promptYellow: ZEN_LIGHT_STATUS.warning,
  warning: ZEN_LIGHT_STATUS.warning,
  dangerText: ZEN_LIGHT_STATUS.danger,
  success: ZEN_LIGHT_STATUS.success,
  disabledText: ZEN_LIGHT_NEUTRALS.textTertiary,
  dangerSoft: ZEN_LIGHT_STATUS.dangerSoft,
  warningSoft: ZEN_LIGHT_STATUS.warningSoft,
  successSoft: ZEN_LIGHT_STATUS.successSoft,
  shadowColor: ZEN_BRAND_COLORS.environment,
};

export const ZEN_DARK_APP_COLORS: AppColors = {
  bgPrimary: ZEN_BRAND_COLORS.environment,
  bgSurface: ZEN_DARK_NEUTRALS.surface,
  bgElevated: ZEN_DARK_NEUTRALS.elevated,
  textPrimary: ZEN_DARK_NEUTRALS.textPrimary,
  textSecondary: ZEN_DARK_NEUTRALS.textSecondary,
  textTertiary: ZEN_DARK_NEUTRALS.textTertiary,
  accent: ZEN_SAGE[400],
  accentSoft: ZEN_DARK_NEUTRALS.active,
  accentStrong: ZEN_SAGE[300],
  logoDetail: ZEN_BRAND_COLORS.ivory,
  statusFailed: ZEN_DARK_STATUS.danger,
  statusBlocked: ZEN_DARK_STATUS.warning,
  statusUnknown: ZEN_DARK_NEUTRALS.textTertiary,
  statusRunning: ZEN_SAGE[400],
  statusDone: ZEN_DARK_STATUS.success,
  zenGreen: ZEN_DARK_STATUS.success,
  priorityUrgent: ZEN_DARK_STATUS.danger,
  priorityHigh: ZEN_DARK_STATUS.warning,
  priorityMedium: ZEN_SAGE[400],
  priorityLow: ZEN_DARK_NEUTRALS.textSecondary,
  border: ZEN_DARK_NEUTRALS.border,
  borderSubtle: ZEN_DARK_NEUTRALS.borderSubtle,
  borderStrong: ZEN_DARK_NEUTRALS.borderStrong,
  surfaceSubtle: ZEN_DARK_NEUTRALS.subtle,
  surfacePressed: ZEN_DARK_NEUTRALS.pressed,
  surfaceActive: ZEN_DARK_NEUTRALS.active,
  inputBackground: ZEN_DARK_NEUTRALS.subtle,
  disabledSurface: ZEN_DARK_NEUTRALS.elevated,
  modalBackdrop: ZEN_DARK_OVERLAYS.modalBackdrop,
  modalSurface: ZEN_DARK_NEUTRALS.subtle,
  modalSurfaceAlt: ZEN_DARK_NEUTRALS.modalSurfaceAlt,
  textOnAccent: ZEN_BRAND_COLORS.environment,
  focusRing: ZEN_SAGE[300],
  selectionBackground: ZEN_DARK_OVERLAYS.selection,
  promptGreen: ZEN_DARK_STATUS.success,
  promptYellow: ZEN_DARK_STATUS.warning,
  warning: ZEN_DARK_STATUS.warning,
  dangerText: ZEN_DARK_STATUS.danger,
  success: ZEN_DARK_STATUS.success,
  disabledText: ZEN_DARK_NEUTRALS.textTertiary,
  dangerSoft: ZEN_DARK_STATUS.dangerSoft,
  warningSoft: ZEN_DARK_STATUS.warningSoft,
  successSoft: ZEN_DARK_STATUS.successSoft,
  shadowColor: '#000000',
};

export const ZEN_LIGHT_CHAT_PALETTE: ChatPalette = {
  layout: 'telegram',
  showWallpaper: false,
  showTimestamps: false,
  showDateDividers: true,
  background: ZEN_LIGHT_NEUTRALS.canvas,
  sentBubble: ZEN_SAGE[200],
  receivedBubble: ZEN_LIGHT_NEUTRALS.surface,
  sentText: ZEN_LIGHT_NEUTRALS.textPrimary,
  receivedText: ZEN_LIGHT_NEUTRALS.textPrimary,
  sentTimestamp: ZEN_LIGHT_NEUTRALS.textSecondary,
  receivedTimestamp: ZEN_LIGHT_NEUTRALS.textTertiary,
  // Outside the bubble on chat.background — high-contrast sage, not outline chrome.
  outboundSentClock: ZEN_SAGE[700],
  composerBackground: ZEN_LIGHT_NEUTRALS.surface,
  composerBorder: ZEN_LIGHT_NEUTRALS.border,
  composerDock: TRANSPARENT,
  link: ZEN_SAGE[700],
  patternIcon: TRANSPARENT,
};

export const ZEN_DARK_CHAT_PALETTE: ChatPalette = {
  layout: 'telegram',
  showWallpaper: false,
  showTimestamps: false,
  showDateDividers: true,
  background: ZEN_BRAND_COLORS.environment,
  sentBubble: ZEN_SAGE[700],
  receivedBubble: ZEN_DARK_NEUTRALS.subtle,
  sentText: ZEN_DARK_NEUTRALS.textPrimary,
  receivedText: ZEN_DARK_NEUTRALS.textPrimary,
  sentTimestamp: ZEN_SAGE[200],
  receivedTimestamp: ZEN_DARK_NEUTRALS.textTertiary,
  // Outside the bubble on near-black canvas — bright sage for status readability.
  outboundSentClock: ZEN_SAGE[200],
  composerBackground: ZEN_DARK_NEUTRALS.subtle,
  composerBorder: ZEN_DARK_NEUTRALS.border,
  composerDock: TRANSPARENT,
  link: ZEN_SAGE[300],
  patternIcon: TRANSPARENT,
};

export const ZEN_LIGHT_SURFACE_PALETTE: SurfacePalette = {
  card: ZEN_LIGHT_APP_COLORS.bgSurface,
  cardStrong: ZEN_LIGHT_APP_COLORS.bgElevated,
  subtle: ZEN_LIGHT_APP_COLORS.surfaceSubtle,
  border: ZEN_LIGHT_APP_COLORS.border,
  sectionLabel: ZEN_LIGHT_APP_COLORS.textTertiary,
};

export const ZEN_DARK_SURFACE_PALETTE: SurfacePalette = {
  card: ZEN_DARK_APP_COLORS.bgSurface,
  cardStrong: ZEN_DARK_APP_COLORS.bgElevated,
  subtle: ZEN_DARK_APP_COLORS.surfaceSubtle,
  border: ZEN_DARK_APP_COLORS.border,
  sectionLabel: ZEN_DARK_APP_COLORS.textTertiary,
};

export const ZEN_LIGHT_MATERIALS: MaterialPalette = {
  chrome: 'rgba(242,243,240,0.88)',
  regular: 'rgba(255,255,255,0.92)',
  thick: 'rgba(255,255,255,0.97)',
  thin: 'rgba(255,255,255,0.68)',
  highlight: 'rgba(255,255,255,0.95)',
  stroke: 'rgba(18,24,20,0.08)',
  separator: 'rgba(18,24,20,0.10)',
  tint: 'rgba(74,113,84,0.12)',
};

export const ZEN_DARK_MATERIALS: MaterialPalette = {
  chrome: 'rgba(15,15,20,0.86)',
  regular: 'rgba(30,31,37,0.92)',
  thick: 'rgba(36,38,44,0.97)',
  thin: 'rgba(44,46,53,0.64)',
  highlight: 'rgba(255,255,255,0.10)',
  stroke: 'rgba(255,255,255,0.08)',
  separator: 'rgba(255,255,255,0.09)',
  tint: 'rgba(130,166,138,0.18)',
};

export const ZEN_LIGHT_DATA_VISUALIZATION: DataVisualizationPalette = {
  activityRamp: [ZEN_SAGE[100], ZEN_SAGE[200], ZEN_SAGE[400], ZEN_SAGE[700]],
};

export const ZEN_DARK_DATA_VISUALIZATION: DataVisualizationPalette = {
  activityRamp: [
    ZEN_DARK_NEUTRALS.active,
    ZEN_SAGE[800],
    ZEN_SAGE[500],
    ZEN_SAGE[300],
  ],
};
