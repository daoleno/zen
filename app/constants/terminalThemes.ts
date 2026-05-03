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
  | 'kanagawa'
  | 'rose-pine'
  | 'everforest'
  | 'zen-midnight'
  | 'zen-amber'
  | 'zen-paper';

export type TerminalThemePreference = TerminalThemeName | 'system';

export type TerminalSystemColorScheme =
  | 'light'
  | 'dark'
  | 'unspecified'
  | null
  | undefined;

export const TerminalThemes: Record<TerminalThemeName, TerminalThemePalette> = {
  // Kanagawa Dragon — deep ink, Japanese brushwork. The most zen by name and spirit.
  'kanagawa': {
    background: '#0D0C0C',
    foreground: '#C5C9C5',
    cursor: '#C8C093',
    cursorAccent: '#0D0C0C',
    selectionBackground: 'rgba(200, 192, 147, 0.20)',
    selectionInactiveBackground: 'rgba(200, 192, 147, 0.10)',
    black: '#16161D',
    red: '#C34043',
    green: '#76946A',
    yellow: '#C0A36E',
    blue: '#7E9CD8',
    magenta: '#957FB8',
    cyan: '#6A9589',
    white: '#C8C093',
    brightBlack: '#717C7C',
    brightRed: '#E82424',
    brightGreen: '#98BB6C',
    brightYellow: '#E6C384',
    brightBlue: '#7FB4CA',
    brightMagenta: '#938AA9',
    brightCyan: '#7AA89F',
    brightWhite: '#DCD7BA',
  },
  // Rosé Pine Moon — botanical moonlight, soft purples and warm gold.
  'rose-pine': {
    background: '#232136',
    foreground: '#E0DEF4',
    cursor: '#EA9A97',
    cursorAccent: '#232136',
    selectionBackground: 'rgba(196, 167, 231, 0.20)',
    selectionInactiveBackground: 'rgba(196, 167, 231, 0.10)',
    black: '#393552',
    red: '#EB6F92',
    green: '#3E8FB0',
    yellow: '#F6C177',
    blue: '#9CCFD8',
    magenta: '#C4A7E7',
    cyan: '#EA9A97',
    white: '#E0DEF4',
    brightBlack: '#6E6A86',
    brightRed: '#EB6F92',
    brightGreen: '#9CCFD8',
    brightYellow: '#F6C177',
    brightBlue: '#9CCFD8',
    brightMagenta: '#C4A7E7',
    brightCyan: '#EA9A97',
    brightWhite: '#E0DEF4',
  },
  // Everforest Dark — forest earth tones, low glare, long sessions.
  'everforest': {
    background: '#272E33',
    foreground: '#D3C6AA',
    cursor: '#A7C080',
    cursorAccent: '#272E33',
    selectionBackground: 'rgba(167, 192, 128, 0.22)',
    selectionInactiveBackground: 'rgba(167, 192, 128, 0.12)',
    black: '#374145',
    red: '#E67E80',
    green: '#A7C080',
    yellow: '#DBBC7F',
    blue: '#7FBBB3',
    magenta: '#D699B6',
    cyan: '#83C092',
    white: '#D3C6AA',
    brightBlack: '#475258',
    brightRed: '#E67E80',
    brightGreen: '#A7C080',
    brightYellow: '#DBBC7F',
    brightBlue: '#7FBBB3',
    brightMagenta: '#D699B6',
    brightCyan: '#83C092',
    brightWhite: '#D3C6AA',
  },
  // Zen Midnight — cool ANSI contrast, blue cursor.
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
  // Zen Paper — warm parchment light theme, ink on paper.
  'zen-paper': {
    background: '#F3EDE0',
    foreground: '#2B2921',
    cursor: '#8B7355',
    cursorAccent: '#F3EDE0',
    selectionBackground: 'rgba(139, 115, 85, 0.22)',
    selectionInactiveBackground: 'rgba(139, 115, 85, 0.12)',
    black: '#1C1918',
    red: '#C65A52',
    green: '#5C8F50',
    yellow: '#9F7D38',
    blue: '#4D77A8',
    magenta: '#8A6097',
    cyan: '#4F8782',
    white: '#DDD8CE',
    brightBlack: '#7A7060',
    brightRed: '#D8736B',
    brightGreen: '#73A766',
    brightYellow: '#B8924B',
    brightBlue: '#668DB7',
    brightMagenta: '#A077AB',
    brightCyan: '#68A09A',
    brightWhite: '#F3EDE0',
  },
  // Zen Amber — warm, low-glare ANSI tuned for long sessions.
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
  'kanagawa': 'Kanagawa',
  'rose-pine': 'Rosé Pine',
  'everforest': 'Everforest',
  'zen-midnight': 'Midnight',
  'zen-amber': 'Amber',
  'zen-paper': 'Paper',
};

export const TerminalThemeDescriptions: Record<TerminalThemeName, string> = {
  'kanagawa': 'Deep ink and warm sand — Japanese woodblock in terminal form.',
  'rose-pine': 'Botanical moonlight, soft purples and warm gold.',
  'everforest': 'Forest earth tones, low glare, easy on long sessions.',
  'zen-midnight': 'Cool blue contrast, clean and bright.',
  'zen-amber': 'Warm, low-glare palette tuned for long sessions.',
  'zen-paper': 'Warm parchment light theme, ink on paper.',
};

export const DefaultTerminalThemeName: TerminalThemeName = 'kanagawa';
export const SystemLightTerminalThemeName: TerminalThemeName = 'zen-paper';
export const SystemDarkTerminalThemeName: TerminalThemeName = DefaultTerminalThemeName;
export const DefaultTerminalThemePreference: TerminalThemePreference = 'system';

export const TerminalThemePreferenceLabels: Record<TerminalThemePreference, string> = {
  system: 'System',
  ...TerminalThemeLabels,
};

export function isTerminalThemeName(value: string): value is TerminalThemeName {
  return Object.prototype.hasOwnProperty.call(TerminalThemes, value);
}

export function isTerminalThemePreference(
  value: string,
): value is TerminalThemePreference {
  return value === 'system' || isTerminalThemeName(value);
}

export function resolveTerminalThemePreference(
  preference: TerminalThemePreference = DefaultTerminalThemePreference,
  colorScheme: TerminalSystemColorScheme = 'dark',
): TerminalThemeName {
  if (preference !== 'system') {
    return preference;
  }

  return colorScheme === 'light'
    ? SystemLightTerminalThemeName
    : SystemDarkTerminalThemeName;
}

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
    // Slightly lighter than the terminal background so the toolbar is visually above,
    // not merged with the dark system keyboard behind it.
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
