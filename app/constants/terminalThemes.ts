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

export type TerminalThemeName = 'zen-midnight' | 'zen-amber';

export const TerminalThemes: Record<TerminalThemeName, TerminalThemePalette> = {
  'zen-midnight': {
    background: '#0C1117',
    foreground: '#D9E1EA',
    cursor: '#8BC2FF',
    cursorAccent: '#0C1117',
    selectionBackground: 'rgba(117, 160, 211, 0.24)',
    selectionInactiveBackground: 'rgba(117, 160, 211, 0.14)',
    black: '#1B2430',
    red: '#E67E80',
    green: '#A7C080',
    yellow: '#DBBC7F',
    blue: '#7FBBB3',
    magenta: '#D699B6',
    cyan: '#83C092',
    white: '#D9E1EA',
    brightBlack: '#5C6773',
    brightRed: '#F29C9C',
    brightGreen: '#C0D9A2',
    brightYellow: '#E8CC8F',
    brightBlue: '#97D0C8',
    brightMagenta: '#E7B1CB',
    brightCyan: '#A6D9B0',
    brightWhite: '#F6F8FB',
  },
  'zen-amber': {
    background: '#28221D',
    foreground: '#E2D6C4',
    cursor: '#D8A657',
    cursorAccent: '#28221D',
    selectionBackground: 'rgba(216, 166, 87, 0.22)',
    selectionInactiveBackground: 'rgba(216, 166, 87, 0.12)',
    black: '#3C332E',
    red: '#EA6962',
    green: '#A9B665',
    yellow: '#D8A657',
    blue: '#7DAEA3',
    magenta: '#D3869B',
    cyan: '#89B482',
    white: '#D5C4A1',
    brightBlack: '#7C6F64',
    brightRed: '#FF8F84',
    brightGreen: '#C0D38C',
    brightYellow: '#EBCB8B',
    brightBlue: '#9FC6BC',
    brightMagenta: '#E0A4B5',
    brightCyan: '#A8D6C0',
    brightWhite: '#FBF1C7',
  },
};

export const TerminalThemeLabels: Record<TerminalThemeName, string> = {
  'zen-midnight': 'Midnight',
  'zen-amber': 'Amber Quiet',
};

export const TerminalThemeDescriptions: Record<TerminalThemeName, string> = {
  'zen-midnight': 'Cooler ANSI contrast with a clean blue cursor.',
  'zen-amber': 'Warm, low-glare ANSI palette tuned for long sessions.',
};

export const DefaultTerminalThemeName: TerminalThemeName = 'zen-amber';

export function resolveTerminalTheme(
  name: TerminalThemeName = DefaultTerminalThemeName,
  overrides?: Partial<TerminalThemePalette>,
): TerminalThemePalette {
  return {
    ...TerminalThemes[name],
    ...overrides,
  };
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
    appBackground: mixHex(theme.background, '#000000', 0.22),
    surface: mixHex(theme.background, theme.foreground, 0.07),
    surfaceMuted: mixHex(theme.background, theme.foreground, 0.04),
    surfaceActive: mixHex(theme.background, theme.cursor, 0.16),
    border: withAlpha(theme.foreground, 0.1),
    borderStrong: withAlpha(theme.cursor, 0.26),
    text: theme.foreground,
    textMuted: mixHex(theme.foreground, theme.background, 0.34),
    textSubtle: mixHex(theme.foreground, theme.background, 0.56),
    accent: theme.cursor,
    accentSoft: withAlpha(theme.cursor, 0.18),
    overlay: withAlpha(theme.background, 0.92),
  };
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
