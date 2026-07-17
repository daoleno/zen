import type { GhosttyGridSize } from './useGhosttyCoreTerminal';

interface TerminalLiveGridTargets {
  requestPtyGrid(cols: number, rows: number): void;
  resetGhostty(sessionId: string, grid: GhosttyGridSize): boolean;
  resizeGhostty(sessionId: string, grid: GhosttyGridSize): boolean;
}

function normalizedGrid(grid: GhosttyGridSize): GhosttyGridSize | null {
  if (
    !Number.isFinite(grid.cols) ||
    !Number.isFinite(grid.rows) ||
    !Number.isFinite(grid.cellWidth) ||
    !Number.isFinite(grid.cellHeight)
  ) {
    return null;
  }

  const cols = Math.trunc(grid.cols);
  const rows = Math.trunc(grid.rows);
  if (cols <= 0 || rows <= 0 || grid.cellWidth <= 0 || grid.cellHeight <= 0) {
    return null;
  }

  return {
    cols,
    rows,
    cellWidth: grid.cellWidth,
    cellHeight: grid.cellHeight,
  };
}

function equalGrid(left: GhosttyGridSize | null, right: GhosttyGridSize): boolean {
  return left?.cols === right.cols &&
    left.rows === right.rows &&
    left.cellWidth === right.cellWidth &&
    left.cellHeight === right.cellHeight;
}

/**
 * Owns the one geometry value shared by the phone renderer, Ghostty, and PTY.
 * It has no source-grid or projection state.
 */
export class TerminalLiveGridOwner {
  private grid: GhosttyGridSize | null = null;
  private attachedSessionId: string | null = null;

  constructor(private readonly targets: TerminalLiveGridTargets) {}

  update(candidate: GhosttyGridSize): boolean {
    const grid = normalizedGrid(candidate);
    if (!grid || equalGrid(this.grid, grid)) {
      return false;
    }

    const sessionId = this.attachedSessionId;
    if (sessionId && !this.targets.resizeGhostty(sessionId, grid)) {
      return false;
    }
    this.grid = grid;
    this.targets.requestPtyGrid(grid.cols, grid.rows);
    return true;
  }

  attach(sessionId: string): boolean {
    const nextSessionId = sessionId.trim();
    if (!nextSessionId || !this.grid) {
      return false;
    }

    this.attachedSessionId = nextSessionId;
    if (this.targets.resetGhostty(nextSessionId, this.grid)) {
      return true;
    }
    this.attachedSessionId = null;
    return false;
  }

  detach(sessionId?: string): void {
    if (sessionId && sessionId !== this.attachedSessionId) {
      return;
    }
    this.attachedSessionId = null;
  }
}
