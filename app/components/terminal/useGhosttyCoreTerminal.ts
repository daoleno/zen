import { useCallback, useEffect, useMemo, useRef } from 'react';
import {
  type TerminalThemePalette,
} from '../../constants/terminalThemes';
import {
  createTerminal,
  destroyTerminal,
  encodeMouseEvent,
  getRenderSnapshot,
  getVisibleText,
  resize as resizeTerminal,
  setTheme as setNativeTheme,
  writeData,
  type MouseEventPayload,
  type RenderSnapshot,
} from '../../modules/zen-terminal-vt/src';

export interface GhosttyGridSize {
  cols: number;
  rows: number;
  cellWidth: number;
  cellHeight: number;
}

/**
 * Thin lifecycle wrapper around the libghostty-vt native module.
 *
 * This owns the terminal handle, keeps its grid in sync with the renderer,
 * and exposes snapshot reads plus protocol-aware mouse encoding.
 */
export function useGhosttyCoreTerminal() {
  const handleRef = useRef<number>(0);
  const dirtyRef = useRef(false);
  const gridRef = useRef<GhosttyGridSize | null>(null);
  const themeRef = useRef<TerminalThemePalette | null>(null);

  const logNativeError = useCallback((operation: string, error: unknown) => {
    console.error('[useGhosttyCoreTerminal] ' + operation + ' failed:', error);
  }, []);

  const applyTheme = useCallback((handle: number, theme: TerminalThemePalette) => {
    try {
      setNativeTheme(handle, {
        foreground: theme.foreground,
        background: theme.background,
        cursor: theme.cursor,
        palette: buildTerminalPalette(theme),
      });
      dirtyRef.current = true;
      return true;
    } catch (error) {
      logNativeError('setTheme', error);
      return false;
    }
  }, [logNativeError]);

  const setTheme = useCallback((theme: TerminalThemePalette) => {
    themeRef.current = theme;

    const handle = handleRef.current;
    if (!handle) {
      return false;
    }

    return applyTheme(handle, theme);
  }, [applyTheme]);

  const ensureTerminal = useCallback((grid: GhosttyGridSize) => {
    if (grid.cols <= 0 || grid.rows <= 0) {
      return false;
    }

    if (!handleRef.current) {
      try {
        handleRef.current = createTerminal(grid.cols, grid.rows);
      } catch (error) {
        logNativeError('createTerminal', error);
        return false;
      }
      if (!handleRef.current) {
        return false;
      }
      const theme = themeRef.current;
      if (theme) {
        applyTheme(handleRef.current, theme);
      }
      gridRef.current = null;
      dirtyRef.current = true;
    }

    const previousGrid = gridRef.current;
    const shouldResize = !previousGrid ||
      previousGrid.cols !== grid.cols ||
      previousGrid.rows !== grid.rows ||
      previousGrid.cellWidth !== grid.cellWidth ||
      previousGrid.cellHeight !== grid.cellHeight;

    if (shouldResize) {
      try {
        resizeTerminal(handleRef.current, grid.cols, grid.rows, grid.cellWidth, grid.cellHeight);
        dirtyRef.current = true;
        gridRef.current = grid;
      } catch (error) {
        logNativeError('resize', error);
        return false;
      }
    }

    return true;
  }, [applyTheme, logNativeError]);

  const writeOutput = useCallback((data: string) => {
    const handle = handleRef.current;
    if (!handle || !data) {
      return false;
    }

    try {
      writeData(handle, data);
      dirtyRef.current = true;
      return true;
    } catch (error) {
      logNativeError('writeData', error);
      return false;
    }
  }, [logNativeError]);

  const encodePointer = useCallback((event: MouseEventPayload) => {
    const handle = handleRef.current;
    if (!handle) {
      return '';
    }

    try {
      return encodeMouseEvent(handle, event);
    } catch (error) {
      logNativeError('encodeMouseEvent', error);
      return '';
    }
  }, [logNativeError]);

  const consumeRenderSnapshot = useCallback((): RenderSnapshot | null => {
    const handle = handleRef.current;
    if (!handle || !dirtyRef.current) {
      return null;
    }
    dirtyRef.current = false;

    try {
      const snapshot = getRenderSnapshot(handle);
      return snapshot.dirty === 'none' ? null : snapshot;
    } catch (error) {
      logNativeError('getRenderSnapshot', error);
      return null;
    }
  }, [logNativeError]);

  const getVisibleTextSnapshot = useCallback(() => {
    const handle = handleRef.current;
    if (!handle) {
      return '';
    }

    try {
      return getVisibleText(handle);
    } catch (error) {
      logNativeError('getVisibleText', error);
      return '';
    }
  }, [logNativeError]);

  useEffect(() => {
    return () => {
      const handle = handleRef.current;
      handleRef.current = 0;
      gridRef.current = null;
      themeRef.current = null;
      dirtyRef.current = false;
      if (!handle) {
        return;
      }
      try {
        destroyTerminal(handle);
      } catch (error) {
        logNativeError('destroyTerminal', error);
      }
    };
  }, [logNativeError]);

  return useMemo(() => ({
    ensureTerminal,
    setTheme,
    writeOutput,
    encodePointer,
    consumeRenderSnapshot,
    getVisibleTextSnapshot,
  }), [consumeRenderSnapshot, encodePointer, ensureTerminal, getVisibleTextSnapshot, setTheme, writeOutput]);
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

function buildTerminalPalette(theme: TerminalThemePalette): string[] {
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

function rgbToHex(red: number, green: number, blue: number): string {
  return (
    '#' +
    red.toString(16).padStart(2, '0') +
    green.toString(16).padStart(2, '0') +
    blue.toString(16).padStart(2, '0')
  );
}
