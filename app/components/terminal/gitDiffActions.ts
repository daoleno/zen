import type {
  GitDiffPage,
  GitDiffPageRequest,
  GitDiffRow,
  GitDiffScope,
} from "../../services/gitDiff";
import type { ActionMenuItem } from "../ui/ActionMenu";

/** Rows beyond this are not copied; the reader stays the place for huge diffs. */
// Every page request re-runs the file's git diff on the daemon, so the walk
// stays short (about 25 pages of 120 rows).
export const GIT_DIFF_COPY_ROW_LIMIT = 3_000;

export type GitDiffPatchResult =
  | { ok: true; text: string; lines: number }
  | { ok: false; reason: "too-large" | "changed" | "empty"; total?: number };

/**
 * Rebuilds raw patch text from paged rows. Section labels are UI only, and
 * the daemon splits long lines into continuation rows that join without a
 * newline, so every byte of the original line survives.
 */
export function joinGitDiffRows(rows: readonly GitDiffRow[]): string {
  const lines: string[] = [];
  for (const row of rows) {
    if (row.kind === "scope") continue;
    if (row.continuation && lines.length) {
      lines[lines.length - 1] += row.text;
    } else {
      lines.push(row.text);
    }
  }
  return lines.length ? `${lines.join("\n")}\n` : "";
}

/** Reads one file's complete comparison, pinned to the first page's version. */
export async function collectGitDiffPatch(
  loadPage: (request: GitDiffPageRequest) => Promise<GitDiffPage>,
  path: string,
  scope: GitDiffScope,
  limit = GIT_DIFF_COPY_ROW_LIMIT,
  /** Called once when the comparison needs more than one page. */
  onLongWalk?: (total: number) => void,
): Promise<GitDiffPatchResult> {
  const rows: GitDiffRow[] = [];
  let version: string | undefined;
  let total = Infinity;
  let row = 0;
  while (row < total) {
    const page = await loadPage({ path, scope, row, version });
    if (page.stale) return { ok: false, reason: "changed" };
    if (version === undefined) {
      version = page.version;
      total = page.total;
      if (total > limit) return { ok: false, reason: "too-large", total };
      if (total > page.rows.length) onLongWalk?.(total);
    }
    if (!page.rows.length) break;
    rows.push(...page.rows);
    row = page.start + page.rows.length;
  }
  const text = joinGitDiffRows(rows);
  if (!text) return { ok: false, reason: "empty" };
  return { ok: true, text, lines: text.split("\n").length - 1 };
}

export interface GitDiffMenuRequest {
  title?: string;
  items: ActionMenuItem[];
}

/** Secondary actions for one changed file, shared by header and row menus. */
export function buildGitDiffFileActions({
  path,
  deleted,
  reading,
  searchOpen,
  optionsOpen,
  onToggleSearch,
  onToggleOptions,
  onViewDiff,
  onOpenFile,
  onCopyPath,
  onCopyPatch,
}: {
  path: string;
  deleted: boolean;
  /** Reader-only controls (find, display) appear when the diff is open. */
  reading: boolean;
  searchOpen?: boolean;
  optionsOpen?: boolean;
  onToggleSearch?(): void;
  onToggleOptions?(): void;
  onViewDiff?(): void;
  onOpenFile(path: string): void;
  onCopyPath(path: string): void;
  onCopyPatch(path: string): void;
}): ActionMenuItem[] {
  const items: ActionMenuItem[] = [];
  if (!reading && onViewDiff) {
    items.push({ key: "view", label: "View diff", icon: "git-compare-outline", onPress: onViewDiff });
  }
  if (reading && onToggleSearch) {
    items.push({
      key: "find",
      label: searchOpen ? "Hide find bar" : "Find in diff",
      icon: "search",
      onPress: onToggleSearch,
    });
  }
  if (reading && onToggleOptions) {
    items.push({
      key: "display",
      label: optionsOpen ? "Hide display options" : "Display options",
      icon: "text-outline",
      onPress: onToggleOptions,
    });
  }
  items.push(
    {
      key: "open",
      label: "Open working file",
      detail: deleted ? "Deleted from the working tree" : undefined,
      icon: "document-text-outline",
      disabled: deleted,
      onPress: () => onOpenFile(path),
    },
    { key: "copy-path", label: "Copy path", icon: "link-outline", onPress: () => onCopyPath(path) },
    { key: "copy-diff", label: "Copy diff", icon: "copy-outline", onPress: () => onCopyPatch(path) },
  );
  return items;
}
