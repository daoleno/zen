import type { AppColors } from './palette';

export type ThemeColorScheme = 'light' | 'dark';

/** Shell + list UI palette (tabs, settings, inbox). */
export type AppPalette = AppColors;

export type ChatLayout = 'chatgpt' | 'telegram' | 'classic';

/** Chat-specific palette — layout + colors for future theme picker support. */
export interface ChatPalette {
  layout: ChatLayout;
  showWallpaper: boolean;
  showTimestamps: boolean;
  showDateDividers: boolean;
  background: string;
  sentBubble: string;
  receivedBubble: string;
  sentText: string;
  receivedText: string;
  sentTimestamp: string;
  receivedTimestamp: string;
  /**
   * Outbound transport-status clock (sending). Paints in the timeline inset
   * outside the bubble — never as outline chrome. Distinct from timestamps and
   * any future sent/read marks.
   */
  outboundSentClock: string;
  composerBackground: string;
  composerBorder: string;
  composerDock: string;
  link: string;
  patternIcon: string;
}

export interface SurfacePalette {
  card: string;
  cardStrong: string;
  subtle: string;
  border: string;
  sectionLabel: string;
}

/**
 * Layered materials for chrome that floats over content: bars, sheets,
 * menus, floating controls. Fills are translucent so the canvas reads through;
 * `highlight` is the lit top edge and `stroke` the outer hairline.
 */
export interface MaterialPalette {
  /** Navigation bars and pinned chrome over scrolling content. */
  chrome: string;
  /** Sheets, menus and floating controls. */
  regular: string;
  /** Popovers and toasts that must stay legible over any content. */
  thick: string;
  /** Chips and capsules resting directly on the canvas. */
  thin: string;
  highlight: string;
  stroke: string;
  separator: string;
  /** Accent-tinted fill for selected or tinted controls. */
  tint: string;
}

export interface DataVisualizationPalette {
  activityRamp: readonly [string, string, string, string];
}

export interface ZenThemeDefinition {
  id: string;
  name: string;
  colorScheme: ThemeColorScheme;
  colors: AppPalette;
  chat: ChatPalette;
  surfaces: SurfacePalette;
  materials: MaterialPalette;
  dataVisualization: DataVisualizationPalette;
  avatarColors: readonly string[];
}

export interface ResolvedZenTheme extends ZenThemeDefinition {
  isLight: boolean;
}

export type ThemePreference = 'system' | string;
