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
    tone,
    additions,
    deletions,
    binary: Boolean(file.binary),
  };
}
