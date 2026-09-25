import {
  describeGitDiffScope,
  gitDiffCounts,
  type GitDiffFileInfo,
  type GitDiffScope,
} from "../../services/gitDiff";

export type GitDiffStatusTone =
  | "added"
  | "deleted"
  | "renamed"
  | "modified"
  | "conflict"
  | "untracked"
  | "binary";

export interface GitDiffFilePresentation {
  name: string;
  directory: string;
  oldName: string | null;
  oldDirectory: string | null;
  statusLabel: string;
  scopeLabel: string;
  icon: string;
  /** One-letter status mark shown in the row's tinted tile (M, A, D, R...). */
  glyph: string;
  tone: GitDiffStatusTone;
  additions: number;
  deletions: number;
  binary: boolean;
}

const STATUS_TONES: Record<string, GitDiffStatusTone> = {
  added: "added",
  deleted: "deleted",
  renamed: "renamed",
  copied: "renamed",
  conflict: "conflict",
  untracked: "untracked",
  modified: "modified",
  changed: "modified",
};

const TONE_ICONS: Record<GitDiffStatusTone, string> = {
  added: "add-circle-outline",
  deleted: "remove-circle-outline",
  renamed: "swap-horizontal-outline",
  modified: "ellipse-outline",
  conflict: "warning-outline",
  untracked: "cloud-upload-outline",
  binary: "cube-outline",
};

const STATUS_GLYPHS: Record<string, string> = {
  added: "A",
  deleted: "D",
  renamed: "R",
  copied: "C",
  conflict: "!",
  untracked: "U",
  modified: "M",
  changed: "M",
};

export function splitGitDiffPath(path: string): {
  directory: string;
  name: string;
} {
  const index = path.lastIndexOf("/");
  if (index === -1) {
    return { directory: "", name: path };
  }
  return { directory: path.slice(0, index + 1), name: path.slice(index + 1) };
}

export function describeGitDiffFile(
  file: GitDiffFileInfo,
  scope: GitDiffScope,
): GitDiffFilePresentation {
  const { directory, name } = splitGitDiffPath(file.path);
  const oldPath = file.old_path ? splitGitDiffPath(file.old_path) : null;
  const tone: GitDiffStatusTone = file.binary
    ? "binary"
    : STATUS_TONES[file.status] ?? "modified";
  const [additions, deletions] = gitDiffCounts(file, scope);
  const statusLabel = file.status
    ? file.status.charAt(0).toUpperCase() + file.status.slice(1)
    : "Changed";

  return {
    name,
    directory,
    oldName: oldPath?.name ?? null,
    oldDirectory: oldPath?.directory ?? null,
    statusLabel,
    scopeLabel: describeGitDiffScope(file),
    icon: TONE_ICONS[tone],
    glyph: STATUS_GLYPHS[file.status] ?? "M",
    tone,
    additions,
    deletions,
    binary: Boolean(file.binary),
  };
}

/** Scope words worth showing on a row; the default working change stays quiet. */
export function gitDiffRowScopeNote(
  file: GitDiffFileInfo,
  scope: GitDiffScope,
): string | null {
  if (scope !== "all" || file.untracked) return null;
  if (file.staged && file.unstaged) return "Staged + unstaged";
  if (file.staged) return "Staged";
  return null;
}

/** `+12 −3 · 4 files` style summary for a set of changed files. */
export function summarizeGitDiffFiles(
  files: readonly GitDiffFileInfo[],
  scope: GitDiffScope,
): { additions: number; deletions: number; label: string } {
  let additions = 0;
  let deletions = 0;
  for (const file of files) {
    if (file.binary) continue;
    const [added, deleted] = gitDiffCounts(file, scope);
    additions += added;
    deletions += deleted;
  }
  const count = `${files.length} ${files.length === 1 ? "file" : "files"}`;
  return {
    additions,
    deletions,
    label: files.length ? `+${additions} \u2212${deletions} \u00b7 ${count}` : "No changes",
  };
}

/** Semantic ink for a status tone: terminal palette for change colors. */
export function gitDiffToneColor(
  tone: GitDiffStatusTone,
  palette: { green: string; red: string; blue: string; yellow: string },
  muted: string,
): string {
  switch (tone) {
    case "added":
      return palette.green;
    case "deleted":
    case "conflict":
      return palette.red;
    case "renamed":
      return palette.blue;
    case "modified":
      return palette.yellow;
    case "untracked":
    case "binary":
      return muted;
  }
}
