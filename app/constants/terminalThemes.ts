export interface TerminalThemePalette {
  background: string;
  foreground: string;
  cursor: string;
  cursorAccent: string;
  selectionBackground: string;
  selectionInactiveBackground?: string;
  black: string;
  red: string;
  green: string;
  yellow: string;
  blue: string;
  magenta: string;
  cyan: string;
  white: string;
  brightBlack: string;
  brightRed: string;
  brightGreen: string;
  brightYellow: string;
  brightBlue: string;
  brightMagenta: string;
  brightCyan: string;
  brightWhite: string;
}

export interface TerminalThemeChrome {
  appBackground: string;
  surface: string;
  surfaceMuted: string;
  surfaceActive: string;
  border: string;
  borderStrong: string;
  text: string;
  textMuted: string;
  textSubtle: string;
  accent: string;
  accentSoft: string;
  overlay: string;
}

const ANSI_COLOR_KEYS = [
  'black',
  'red',
  'green',
  'yellow',
  'blue',
  'magenta',
  'cyan',
  'white',
  'brightBlack',
  'brightRed',
  'brightGreen',
  'brightYellow',
  'brightBlue',
  'brightMagenta',
  'brightCyan',
  'brightWhite',
] as const;

const XTERM_CUBE_LEVELS = [0, 95, 135, 175, 215, 255] as const;

export type TerminalThemeName =
  | 'light'
  | 'dark';

export type TerminalSystemColorScheme =
  | 'light'
  | 'dark'
  | 'unspecified'
  | null
  | undefined;

export const TerminalThemes: Record<TerminalThemeName, TerminalThemePalette> = {
  dark: {
    background: '#0F1117',
    foreground: '#E6EAF0',
    cursor: '#5B9DFF',
    cursorAccent: '#0F1117',
    selectionBackground: 'rgba(91, 157, 255, 0.24)',
    selectionInactiveBackground: 'rgba(91, 157, 255, 0.12)',
    black: '#1B1F2A',
    red: '#F87171',
    green: '#6EE7A8',
    yellow: '#FBBF5A',
    blue: '#60A5FA',
    magenta: '#C084FC',
    cyan: '#5EEAD4',
    white: '#D1D5DB',
    brightBlack: '#6B7280',
    brightRed: '#FCA5A5',
    brightGreen: '#A7F3D0',
    brightYellow: '#FDE68A',
    brightBlue: '#93C5FD',
    brightMagenta: '#DDD6FE',
    brightCyan: '#99F6E4',
    brightWhite: '#F8FAFC',
  },
  light: {
    background: '#F8FAFC',
    foreground: '#111827',
    cursor: '#2563EB',
    cursorAccent: '#F8FAFC',
    selectionBackground: 'rgba(37, 99, 235, 0.20)',
    selectionInactiveBackground: 'rgba(37, 99, 235, 0.10)',
    black: '#1F2937',
    red: '#DC2626',
    green: '#15803D',
    yellow: '#B45309',
    blue: '#2563EB',
    magenta: '#9333EA',
    cyan: '#0F766E',
    white: '#E5E7EB',
    brightBlack: '#6B7280',
    brightRed: '#EF4444',
    brightGreen: '#16A34A',
    brightYellow: '#D97706',
    brightBlue: '#3B82F6',
    brightMagenta: '#A855F7',
    brightCyan: '#0D9488',
    brightWhite: '#FFFFFF',
  },
};

export const DefaultTerminalThemeName: TerminalThemeName = 'dark';

export function isTerminalThemeName(value: string): value is TerminalThemeName {
  return Object.prototype.hasOwnProperty.call(TerminalThemes, value);
}

export function resolveTerminalThemeName(
  colorScheme: TerminalSystemColorScheme = 'dark',
): TerminalThemeName {
  return colorScheme === 'light' ? 'light' : 'dark';
}

export function resolveTerminalTheme(
  name: TerminalThemeName = DefaultTerminalThemeName,
): TerminalThemePalette {
  return TerminalThemes[name];
}

export function buildTerminalPalette(theme: TerminalThemePalette): string[] {
  const palette = ANSI_COLOR_KEYS.map((key) => theme[key]);

  for (const red of XTERM_CUBE_LEVELS) {
    for (const green of XTERM_CUBE_LEVELS) {
      for (const blue of XTERM_CUBE_LEVELS) {
        palette.push(rgbToHex(red, green, blue));
      }
    }
  }

  for (let index = 0; index < 24; index += 1) {
    const level = 8 + index * 10;
    palette.push(rgbToHex(level, level, level));
  }

  return palette;
}

export function buildTerminalChrome(theme: TerminalThemePalette): TerminalThemeChrome {
  return {
    // Offset from the terminal background so toolbar controls sit above the surface.
    appBackground: mixHex(theme.background, theme.foreground, 0.06),
    surface: mixHex(theme.background, theme.foreground, 0.06),
    surfaceMuted: mixHex(theme.background, theme.foreground, 0.035),
    surfaceActive: mixHex(theme.background, theme.cursor, 0.14),
    border: withAlpha(theme.foreground, 0.08),
    borderStrong: withAlpha(theme.cursor, 0.22),
    text: theme.foreground,
    textMuted: mixHex(theme.foreground, theme.background, 0.38),
    textSubtle: mixHex(theme.foreground, theme.background, 0.60),
    accent: theme.cursor,
    accentSoft: withAlpha(theme.cursor, 0.14),
    overlay: withAlpha(theme.background, 0.94),
  };
}

export function isLightTerminalTheme(theme: TerminalThemePalette): boolean {
  const { red, green, blue } = parseHex(theme.background);
  const luminance = (0.299 * red + 0.587 * green + 0.114 * blue) / 255;
  return luminance > 0.62;
}

function rgbToHex(red: number, green: number, blue: number): string {
  return (
    '#' +
    red.toString(16).padStart(2, '0') +
    green.toString(16).padStart(2, '0') +
    blue.toString(16).padStart(2, '0')
  );
}

function withAlpha(hex: string, alpha: number): string {
  const { red, green, blue } = parseHex(hex);
  return `rgba(${red}, ${green}, ${blue}, ${clamp(alpha, 0, 1)})`;
}

function mixHex(from: string, to: string, weight: number): string {
  const start = parseHex(from);
  const end = parseHex(to);
  const factor = clamp(weight, 0, 1);
  return rgbToHex(
    Math.round(start.red + (end.red - start.red) * factor),
    Math.round(start.green + (end.green - start.green) * factor),
    Math.round(start.blue + (end.blue - start.blue) * factor),
  );
}

function parseHex(value: string): { red: number; green: number; blue: number } {
  const normalized = value.trim().replace('#', '');
  if (!/^[0-9a-fA-F]{6}$/.test(normalized)) {
    throw new Error(`Expected 6-digit hex color, received "${value}"`);
  }

  return {
    red: parseInt(normalized.slice(0, 2), 16),
    green: parseInt(normalized.slice(2, 4), 16),
    blue: parseInt(normalized.slice(4, 6), 16),
  };
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(Math.max(value, min), max);
}
