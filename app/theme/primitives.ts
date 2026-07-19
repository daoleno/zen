import type { AppColors } from './palette';
import type {
  ChatPalette,
  DataVisualizationPalette,
  SurfacePalette,
} from './types';

const TRANSPARENT = 'transparent';

export const ZEN_SAGE = {
  50: '#F6F8F6',
  100: '#E9EEE9',
  200: '#D2DDD4',
  300: '#B5C8B9',
  400: '#89A28D',
  500: '#6F8A74',
  600: '#56705C',
  700: '#435849',
  800: '#34443A',
  900: '#29362E',
  950: '#151C18',
} as const;

export const ZEN_BRAND_COLORS = {
  environment: '#0F0F14',
  sage: ZEN_SAGE[400],
  ivory: '#F2EEE5',
} as const;

export const ZEN_LIGHT_NEUTRALS = {
  canvas: '#F7F8F6',
  surface: '#FFFFFF',
  elevated: '#F0F2EF',
  pressed: '#E4E8E3',
  textPrimary: '#171A18',
  textSecondary: '#4F5751',
  textTertiary: '#68716A',
  borderSubtle: '#E2E6E1',
  border: '#CCD2CC',
  borderStrong: '#89938B',
} as const;

export const ZEN_DARK_NEUTRALS = {
  surface: '#17181C',
  elevated: '#202226',
  subtle: '#1B1D20',
  active: '#1F2922',
  pressed: '#282B2F',
  textPrimary: '#F2F3EF',
  textSecondary: '#BEC4BD',
  textTertiary: '#929B93',
  borderSubtle: '#282A2E',
  border: '#35383B',
  borderStrong: '#687169',
  modalSurfaceAlt: '#24272A',
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
  selection: 'rgba(86,112,92,0.18)',
  modalBackdrop: 'rgba(15,15,20,0.42)',
} as const;

export const ZEN_DARK_OVERLAYS = {
  selection: 'rgba(137,162,141,0.28)',
  modalBackdrop: 'rgba(0,0,0,0.72)',
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
