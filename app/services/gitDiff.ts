export type GitDiffUnavailableReason = "no_cwd" | "not_git_repo";

export type GitDiffFileStatus =
  | "added"
  | "changed"
  | "conflict"
  | "copied"
  | "deleted"
  | "modified"
  | "renamed"
  | "untracked";

export interface GitDiffFileInfo {
  path: string;
  old_path?: string;
  status: GitDiffFileStatus | string;
  staged: boolean;
  unstaged: boolean;
  untracked: boolean;
  additions?: number;
  deletions?: number;
  binary?: boolean;
  staged_additions?: number;
  staged_deletions?: number;
  working_additions?: number;
  working_deletions?: number;
}

export type GitDiffScope = "all" | "working" | "staged";

export interface GitDiffPageRequest {
  path: string;
  scope: GitDiffScope;
  row: number;
  version?: string;
  query?: string;
}

export interface GitDiffRow {
  kind: "scope" | "meta" | "hunk" | "add" | "delete" | "context" | "marker";
  text: string;
  old?: number;
  new?: number;
  continuation?: boolean;
}

export interface GitDiffPage {
  path: string;
  scope: GitDiffScope;
  version: string;
  stale: boolean;
  start: number;
  total: number;
  rows: GitDiffRow[];
  hunks: number;
  previous_hunk: number;
  next_hunk: number;
  matches: number;
  previous_match: number;
  next_match: number;
}

export function filterGitDiffFiles(
  files: GitDiffFileInfo[],
  scope: GitDiffScope,
  query: string,
) {
  const needle = query.toLocaleLowerCase();
  return files.filter(
    (file) =>
      (scope === "all" ||
        (scope === "staged" ? file.staged : file.unstaged || file.untracked)) &&
      (!needle ||
        file.path.toLocaleLowerCase().includes(needle) ||
        file.old_path?.toLocaleLowerCase().includes(needle)),
  );
}

export function gitDiffCounts(file: GitDiffFileInfo, scope: GitDiffScope) {
  return scope === "staged"
    ? [file.staged_additions ?? 0, file.staged_deletions ?? 0]
    : scope === "working"
      ? [file.working_additions ?? 0, file.working_deletions ?? 0]
      : [file.additions ?? 0, file.deletions ?? 0];
}

export interface GitDiffStatusSnapshot {
  available: boolean;
  reason?: GitDiffUnavailableReason | string;
  repo_root?: string;
  repo_name?: string;
  branch?: string;
  clean: boolean;
  file_count: number;
  staged_file_count: number;
  unstaged_file_count: number;
  untracked_file_count: number;
  additions: number;
  deletions: number;
  files?: GitDiffFileInfo[];
}

export interface GitDiffPatchSection {
  scope: "staged" | "unstaged" | "untracked" | string;
  title: string;
  patch: string;
}

export interface GitDiffPatchPayload {
  repo_root: string;
  path: string;
  sections: GitDiffPatchSection[];
}

export interface GitDiffContentSnapshot {
  label: string;
  exists: boolean;
  binary?: boolean;
  truncated?: boolean;
  reason?: string;
  byte_count: number;
  line_count: number;
  content?: string;
}

export interface GitDiffFileContentPayload {
  repo_root: string;
  path: string;
  current: GitDiffContentSnapshot;
  base: GitDiffContentSnapshot;
}

export interface GitRepoBrowserEntry {
  name: string;
  path: string;
  kind: "directory" | "file" | string;
}

export interface GitRepoBrowserPayload {
  repo_root: string;
  path: string;
  entries: GitRepoBrowserEntry[];
}

export interface GitRepoFileContentPayload {
  repo_root: string;
  path: string;
  snapshot: GitDiffContentSnapshot;
}

export function buildGitDiffChipLabel(
  snapshot: GitDiffStatusSnapshot | null,
  loading: boolean,
): string {
  if (loading && !snapshot) {
    return "Checking repo…";
  }
  if (!snapshot?.available) {
    return "Git diff";
  }
  if (snapshot.clean) {
    return snapshot.branch || "Git";
  }

  const fileLabel =
    snapshot.file_count === 1 ? "1 file" : `${snapshot.file_count} files`;
  if (snapshot.additions > 0 || snapshot.deletions > 0) {
    return `${fileLabel}  +${snapshot.additions}  -${snapshot.deletions}`;
  }
  return fileLabel;
}

export function describeGitDiffScope(file: GitDiffFileInfo): string {
  if (file.untracked) return "Untracked";
  if (file.staged && file.unstaged) return "Staged + unstaged";
  if (file.staged) return "Staged";
  if (file.unstaged) return "Unstaged";
  return "Changed";
}
