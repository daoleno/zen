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

export type TerminalThemeName = 'zen-midnight' | 'zen-amber';

export const TerminalThemes: Record<TerminalThemeName, TerminalThemePalette> = {
  'zen-midnight': {
    background: '#08131B',
    foreground: '#D7E3EE',
    cursor: '#8FD3FF',
    cursorAccent: '#08131B',
    selectionBackground: 'rgba(115, 169, 255, 0.24)',
    selectionInactiveBackground: 'rgba(115, 169, 255, 0.14)',
    black: '#12202C',
    red: '#F7768E',
    green: '#7BD88F',
    yellow: '#F6C177',
    blue: '#73A9FF',
    magenta: '#C099FF',
    cyan: '#78DCE8',
    white: '#D7E3EE',
    brightBlack: '#5B7083',
    brightRed: '#FF9AAE',
    brightGreen: '#A8E6B0',
    brightYellow: '#FFD89A',
    brightBlue: '#9DC1FF',
    brightMagenta: '#D8B8FF',
    brightCyan: '#A0ECF5',
    brightWhite: '#F5FAFF',
  },
  'zen-amber': {
    background: '#14110F',
    foreground: '#F2E6D0',
    cursor: '#F6C177',
    cursorAccent: '#14110F',
    selectionBackground: 'rgba(246, 193, 119, 0.20)',
    selectionInactiveBackground: 'rgba(246, 193, 119, 0.12)',
    black: '#221D18',
    red: '#F38BA8',
    green: '#A6D189',
    yellow: '#F6C177',
    blue: '#8CAAEE',
    magenta: '#CA9EE6',
    cyan: '#81C8BE',
    white: '#EADFCC',
    brightBlack: '#6C6257',
    brightRed: '#FFB4C6',
    brightGreen: '#BFE6A8',
    brightYellow: '#FFD899',
    brightBlue: '#B1C8FF',
    brightMagenta: '#E0BCF8',
    brightCyan: '#A6E3D7',
    brightWhite: '#FFF7EA',
  },
};

export const DefaultTerminalThemeName: TerminalThemeName = 'zen-midnight';

export function resolveTerminalTheme(
  name: TerminalThemeName = DefaultTerminalThemeName,
  overrides?: Partial<TerminalThemePalette>,
): TerminalThemePalette {
  return {
    ...TerminalThemes[name],
    ...overrides,
  };
}
