import { useCallback, useEffect, useRef } from 'react';
import {
  createTerminal,
  destroyTerminal,
  getRenderState,
  getVisibleText,
  resize,
  writeData,
} from 'zen-terminal-vt';
import type { RenderState } from 'zen-terminal-vt';

export interface GhosttyGridSize {
  cols: number;
  rows: number;
  cellWidth: number;
  cellHeight: number;
}

function normalizeRenderCells(cells: unknown): number[] {
  if (Array.isArray(cells)) {
    return cells;
  }

  if (!cells || typeof cells !== 'object') {
    return [];
  }

  const arrayLike = cells as { length?: unknown };
  if (typeof arrayLike.length === 'number') {
    try {
      return Array.from(cells as ArrayLike<number>);
    } catch {
      // Fall through to numeric-key extraction.
    }
  }

  const record = cells as Record<string, unknown>;
  return Object.keys(record)
    .filter((key) => /^\d+$/.test(key))
    .sort((a, b) => Number(a) - Number(b))
    .map((key) => Number(record[key] ?? 0));
}

/**
 * Thin lifecycle wrapper around the libghostty-vt native module.
 *
 * This owns the terminal handle, keeps its grid in sync with the renderer,
 * and exposes snapshot reads for the WebView renderer bridge.
 */
export function useGhosttyCoreTerminal() {
  const handleRef = useRef<number>(0);
  const dirtyRef = useRef(false);
  const gridRef = useRef<GhosttyGridSize | null>(null);

  const ensureTerminal = useCallback((grid: GhosttyGridSize) => {
    if (grid.cols <= 0 || grid.rows <= 0) {
      return false;
    }

    if (!handleRef.current) {
      try {
        handleRef.current = createTerminal(grid.cols, grid.rows);
      } catch (error) {
        console.error('[useGhosttyCoreTerminal] createTerminal failed:', error);
        return false;
      }
      if (!handleRef.current) {
        return false;
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
        resize(handleRef.current, grid.cols, grid.rows, grid.cellWidth, grid.cellHeight);
        dirtyRef.current = true;
        gridRef.current = grid;
      } catch (error) {
        console.error('[useGhosttyCoreTerminal] resize failed:', error);
        return false;
      }
    }

    return true;
  }, []);

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
      console.error('[useGhosttyCoreTerminal] writeData failed:', error);
      return false;
    }
  }, []);

  const consumeRenderState = useCallback((): RenderState | null => {
    const handle = handleRef.current;
    if (!handle || !dirtyRef.current) {
      return null;
    }
    dirtyRef.current = false;

    try {
      const state = getRenderState(handle);
      if (!state || state.dirty === 'none') {
        return null;
      }
      const normalizedCells = normalizeRenderCells((state as RenderState & { cells?: unknown }).cells);
      return normalizedCells === state.cells ? state : { ...state, cells: normalizedCells };
    } catch (error) {
      console.error('[useGhosttyCoreTerminal] getRenderState failed:', error);
      return null;
    }
  }, []);

  const getVisibleTextSnapshot = useCallback(() => {
    const handle = handleRef.current;
    if (!handle) {
      return '';
    }

    try {
      return getVisibleText(handle);
    } catch (error) {
      console.error('[useGhosttyCoreTerminal] getVisibleText failed:', error);
      return '';
    }
  }, []);

  useEffect(() => {
    return () => {
      const handle = handleRef.current;
      handleRef.current = 0;
      gridRef.current = null;
      dirtyRef.current = false;
      if (!handle) {
        return;
      }
      try {
        destroyTerminal(handle);
      } catch (error) {
        console.error('[useGhosttyCoreTerminal] destroyTerminal failed:', error);
      }
    };
  }, []);

  return {
    ensureTerminal,
    writeOutput,
    consumeRenderState,
    getVisibleTextSnapshot,
  };
}
