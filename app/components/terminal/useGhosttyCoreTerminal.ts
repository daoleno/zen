import { useCallback, useEffect, useMemo, useRef } from 'react';
import {
  buildTerminalPalette,
  type TerminalThemePalette,
} from '../../constants/terminalThemes';
import {
  createTerminal,
  destroyTerminal,
  encodeMouseEvent,
  getRenderSnapshot,
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
  const modelSessionIdRef = useRef<string | null>(null);
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

  const resetTerminal = useCallback((sessionId: string, grid: GhosttyGridSize) => {
    const nextSessionId = sessionId.trim();
    if (
      !nextSessionId ||
      !Number.isFinite(grid.cols) ||
      !Number.isFinite(grid.rows) ||
      !Number.isFinite(grid.cellWidth) ||
      !Number.isFinite(grid.cellHeight) ||
      grid.cols <= 0 ||
      grid.rows <= 0 ||
      grid.cellWidth <= 0 ||
      grid.cellHeight <= 0
    ) {
      return false;
    }

    if (
      handleRef.current &&
      modelSessionIdRef.current === nextSessionId
    ) {
      return true;
    }

    const previousHandle = handleRef.current;
    handleRef.current = 0;
    modelSessionIdRef.current = null;
    gridRef.current = null;
    dirtyRef.current = false;
    if (previousHandle) {
      try {
        destroyTerminal(previousHandle);
      } catch (error) {
        logNativeError('destroyTerminal', error);
      }
    }

    let nextHandle = 0;
    try {
      nextHandle = createTerminal(Math.trunc(grid.cols), Math.trunc(grid.rows));
      if (!nextHandle) {
        return false;
      }
      resizeTerminal(
        nextHandle,
        Math.trunc(grid.cols),
        Math.trunc(grid.rows),
        grid.cellWidth,
        grid.cellHeight,
      );
    } catch (error) {
      logNativeError(nextHandle ? 'resize' : 'createTerminal', error);
      if (nextHandle) {
        try {
          destroyTerminal(nextHandle);
        } catch (destroyError) {
          logNativeError('destroyTerminal', destroyError);
        }
      }
      return false;
    }

    handleRef.current = nextHandle;
    modelSessionIdRef.current = nextSessionId;
    gridRef.current = {
      ...grid,
      cols: Math.trunc(grid.cols),
      rows: Math.trunc(grid.rows),
    };
    dirtyRef.current = true;
    const theme = themeRef.current;
    if (theme) {
      applyTheme(nextHandle, theme);
    }

    return true;
  }, [applyTheme, logNativeError]);

  const writeOutput = useCallback((sessionId: string, data: string) => {
    const handle = handleRef.current;
    if (!handle || sessionId !== modelSessionIdRef.current || !data) {
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

  const resizeGrid = useCallback((sessionId: string, grid: GhosttyGridSize) => {
    const handle = handleRef.current;
    if (
      !handle ||
      sessionId !== modelSessionIdRef.current ||
      !Number.isFinite(grid.cols) ||
      !Number.isFinite(grid.rows) ||
      !Number.isFinite(grid.cellWidth) ||
      !Number.isFinite(grid.cellHeight) ||
      grid.cols <= 0 ||
      grid.rows <= 0 ||
      grid.cellWidth <= 0 ||
      grid.cellHeight <= 0
    ) {
      return false;
    }

    const nextGrid = {
      ...grid,
      cols: Math.trunc(grid.cols),
      rows: Math.trunc(grid.rows),
    };
    const previousGrid = gridRef.current;
    if (
      previousGrid?.cols === nextGrid.cols &&
      previousGrid.rows === nextGrid.rows &&
      previousGrid.cellWidth === nextGrid.cellWidth &&
      previousGrid.cellHeight === nextGrid.cellHeight
    ) {
      return true;
    }

    try {
      resizeTerminal(
        handle,
        nextGrid.cols,
        nextGrid.rows,
        nextGrid.cellWidth,
        nextGrid.cellHeight,
      );
      gridRef.current = nextGrid;
      dirtyRef.current = true;
      return true;
    } catch (error) {
      logNativeError('resize', error);
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

  const consumeRenderSnapshot = useCallback((): {
    sessionId: string;
    snapshot: RenderSnapshot;
  } | null => {
    const handle = handleRef.current;
    const sessionId = modelSessionIdRef.current;
    if (!handle || !sessionId || !dirtyRef.current) {
      return null;
    }
    dirtyRef.current = false;

    try {
      const snapshot = getRenderSnapshot(handle);
      return snapshot.dirty === 'none' ? null : { sessionId, snapshot };
    } catch (error) {
      logNativeError('getRenderSnapshot', error);
      return null;
    }
  }, [logNativeError]);

  const currentGrid = useCallback(() => gridRef.current, []);

  useEffect(() => {
    return () => {
      const handle = handleRef.current;
      handleRef.current = 0;
      modelSessionIdRef.current = null;
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
    resetTerminal,
    resizeGrid,
    setTheme,
    writeOutput,
    encodePointer,
    consumeRenderSnapshot,
    currentGrid,
  }), [
    consumeRenderSnapshot,
    currentGrid,
    encodePointer,
    resetTerminal,
    resizeGrid,
    setTheme,
    writeOutput,
  ]);
}
