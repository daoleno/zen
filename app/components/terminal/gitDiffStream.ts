import type { GitDiffPage, GitDiffPageRequest, GitDiffRow, GitDiffScope } from "../../services/gitDiff";

export interface IndexedDiffRow { index: number; row: GitDiffRow }
export interface DiffCellFrame { offset: number; length: number }
export function diffBookmark(frames: ReadonlyMap<number, DiffCellFrame>, offset: number, fallback: number) {
  let row = fallback, top = Infinity;
  for (const [index, frame] of frames) {
    if (frame.length > 0 && frame.offset + frame.length > offset + 0.5 && frame.offset < top) {
      row = index; top = frame.offset;
    }
  }
  return { row, offset: Number.isFinite(top) ? Math.max(0, offset - top) : 0 };
}
interface DiffChunk { start: number; rows: readonly IndexedDiffRow[] }
export interface GitDiffStreamState {
  chunks: readonly DiffChunk[];
  start: number;
  end: number;
  total: number | null;
  version?: string;
  maxCharacters: number;
  loading: "next" | "previous" | "search" | null;
  error: string | null;
  stale: boolean;
  query: string;
  matches: number;
  anchor: number;
  targetRow: number | null;
}

const emptyState = (): GitDiffStreamState => ({
  chunks: [], start: 0, end: 0, total: null, maxCharacters: 0,
  loading: null, error: null, stale: false, query: "", matches: 0, anchor: 0, targetRow: null,
});

// A variable-height native list cannot reconstruct a deep pixel offset from
// unmounted cells. Reopen at the saved source row; normal upward scrolling
// resumes the same version before that anchor.
export function restoreDiffStream(snapshot: GitDiffStreamState | undefined, row?: number): GitDiffStreamState | undefined {
  if (!snapshot || row === undefined || snapshot.end === snapshot.start) return snapshot;
  const targetRow = Math.max(snapshot.start, Math.min(row, snapshot.end - 1));
  const start = Math.max(snapshot.start, targetRow - 120);
  const chunks = snapshot.chunks.filter(chunk => chunk.start + chunk.rows.length > start).map(chunk =>
    chunk.start < start ? { start, rows: chunk.rows.slice(start - chunk.start) } : chunk,
  );
  return { ...snapshot, start, chunks, loading: null, targetRow };
}

export function diffStreamRow(state: GitDiffStreamState, index: number): IndexedDiffRow {
  const absolute = state.start + index;
  let low = 0, high = state.chunks.length - 1;
  while (low <= high) {
    const middle = (low + high) >>> 1;
    const chunk = state.chunks[middle];
    if (absolute < chunk.start) high = middle - 1;
    else if (absolute >= chunk.start + chunk.rows.length) low = middle + 1;
    else return chunk.rows[absolute - chunk.start];
  }
  throw new Error("Diff row is outside the loaded range");
}

// Only contiguous, version-matched chunks enter the native list. Requests are
// fenced per reader, so changing a search or leaving a file cannot append late data.
export class GitDiffStream {
  private state: GitDiffStreamState;
  private listeners = new Set<() => void>();
  private generation = 0;
  private disposed = false;
  private retryOperation: (() => Promise<void>) | null = null;

  constructor(
    private loadPage: (request: GitDiffPageRequest) => Promise<GitDiffPage>,
    private path: string,
    private scope: GitDiffScope,
    restored?: GitDiffStreamState,
  ) {
    this.state = restored ? { ...restored, loading: null, error: null } : emptyState();
  }

  getSnapshot = () => this.state;
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };
  dispose() { this.disposed = true; this.generation++; this.state = { ...this.state, loading: null }; }
  resume() { this.disposed = false; }
  private publish(patch: Partial<GitDiffStreamState>) {
    this.state = { ...this.state, ...patch };
    this.listeners.forEach(listener => listener());
  }
  private current(generation: number) { return !this.disposed && generation === this.generation; }
  private validate(page: GitDiffPage, requested: number) {
    if (page.path !== this.path || page.scope !== this.scope || page.start !== requested) {
      throw new Error("Unexpected diff response");
    }
    if (page.stale || (this.state.version && page.version !== this.state.version) ||
      (this.state.total !== null && page.total !== this.state.total)) {
      this.publish({ stale: true });
      return false;
    }
    if (!Number.isInteger(page.total) || page.total < 0 || page.start + page.rows.length > page.total ||
      (!page.rows.length && page.start < page.total)) throw new Error("Incomplete diff response");
    return true;
  }

  loadNext = async () => {
    if (this.disposed || this.state.loading || this.state.error || this.state.stale ||
      (this.state.total !== null && this.state.end >= this.state.total)) return;
    await this.loadEdge("next");
  };
  loadPrevious = async () => {
    if (this.disposed || this.state.loading || this.state.error || this.state.stale || this.state.start === 0) return;
    await this.loadEdge("previous");
  };
  retry = async () => {
    if (!this.state.loading && !this.state.stale) await this.retryOperation?.();
  };
  refresh = async () => {
    this.generation++;
    const anchor = this.state.anchor + 1;
    this.state = { ...emptyState(), anchor };
    this.publish({});
    await this.loadNext();
  };
  reanchor(row: number) {
    if (this.state.loading) return;
    const restored = restoreDiffStream(this.state, row);
    if (restored) this.publish({ ...restored, anchor: this.state.anchor + 1 });
  }

  private async loadEdge(edge: "next" | "previous") {
    const generation = ++this.generation;
    const start = edge === "next" ? this.state.end : Math.max(0, this.state.start - 120);
    this.retryOperation = () => this.loadEdge(edge);
    this.publish({ loading: edge, error: null });
    try {
      const page = await this.loadPage({ path: this.path, scope: this.scope, row: start, version: this.state.version });
      if (!this.current(generation) || !this.validate(page, start)) return;
      const end = Math.min(start + page.rows.length, edge === "previous" ? this.state.start : page.total);
      if (edge === "previous" && end !== this.state.start) throw new Error("Incomplete diff response");
      const rows = page.rows.slice(0, end - start).map((row, index) => ({ index: start + index, row }));
      const chunk = { start, rows };
      this.publish({
        chunks: rows.length ? edge === "next" ? [...this.state.chunks, chunk] : [chunk, ...this.state.chunks] : this.state.chunks,
        start: edge === "previous" ? start : this.state.start,
        end: edge === "next" ? end : this.state.end,
        total: page.total, version: page.version,
        maxCharacters: Math.max(this.state.maxCharacters, ...rows.map(item => item.row.text.length)),
      });
      this.retryOperation = null;
    } catch (error) {
      if (this.current(generation)) this.publish({ error: error instanceof Error ? error.message : "Could not load diff" });
    } finally {
      if (this.current(generation)) this.publish({ loading: null });
    }
  }

  search = async (query: string) => {
    if (!query) {
      if (!this.state.query && this.state.loading !== "search") return;
      this.generation++;
      this.retryOperation = null;
      this.publish({ query: "", matches: 0, loading: null, error: null });
      if (!this.state.chunks.length) await this.loadNext();
      return;
    }
    await this.navigate(query, 0, "first");
  };
  seek = async (direction: "previous" | "next", row: number, kind: "match" | "hunk" = "match") => {
    await this.navigate(this.state.query, row, direction, kind);
  };

  private async navigate(query: string, row: number, direction: "first" | "previous" | "next", kind: "match" | "hunk" = "match") {
    if (this.disposed || this.state.stale) return;
    const generation = ++this.generation;
    this.retryOperation = () => this.navigate(query, row, direction, kind);
    this.publish({ loading: "search", error: null });
    try {
      const request = (at: number) => this.loadPage({ path: this.path, scope: this.scope, row: at, query, version: this.state.version });
      let page = await request(row);
      if (!this.current(generation) || !this.validate(page, row)) return;
      const probe = page;
      const firstMatches = page.matches > 0 && page.rows[0]?.kind !== "scope" && page.rows[0]?.text.toLowerCase().includes(query.toLowerCase());
      const target = direction === "first" && firstMatches ? row
        : kind === "hunk" ? direction === "previous" ? page.previous_hunk : page.next_hunk
        : direction === "previous" ? page.previous_match : page.next_match;
      if (target >= 0 && target !== row) {
        const version = page.version;
        page = await this.loadPage({ path: this.path, scope: this.scope, row: target, query, version });
        if (!this.current(generation) || !this.validate(page, target)) return;
        if (page.version !== version) { this.publish({ stale: true }); return; }
      }
      if (target >= 0 || !this.state.chunks.length) {
        const rows = page.rows.map((value, index) => ({ index: page.start + index, row: value }));
        const chunks: DiffChunk[] = [{ start: page.start, rows }];
        const start = Math.max(0, page.start - 120);
        if (target >= 0 && start < page.start) {
          const previous = probe.start === start ? probe : await this.loadPage({ path: this.path, scope: this.scope, row: start, query, version: page.version });
          if (!this.current(generation) || !this.validate(previous, start)) return;
          if (previous.version !== page.version || previous.total !== page.total) { this.publish({ stale: true }); return; }
          const prefix = previous.rows.slice(0, page.start - start).map((value, index) => ({ index: start + index, row: value }));
          if (prefix.length !== page.start - start) throw new Error("Incomplete diff response");
          chunks.unshift({ start, rows: prefix });
        }
        this.publish({
          chunks, start: chunks[0].start, end: page.start + rows.length, targetRow: target >= 0 ? target : null,
          total: page.total, version: page.version, anchor: this.state.anchor + 1,
          maxCharacters: Math.max(0, ...chunks.flatMap(chunk => chunk.rows.map(item => item.row.text.length))),
        });
      }
      this.publish({ query, matches: page.matches });
      this.retryOperation = null;
    } catch (error) {
      if (this.current(generation)) this.publish({ error: error instanceof Error ? error.message : "Could not search diff" });
    } finally {
      if (this.current(generation)) this.publish({ loading: null });
    }
  }
}
