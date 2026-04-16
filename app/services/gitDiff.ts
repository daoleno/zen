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
    return snapshot.branch ? `${snapshot.branch} clean` : "Git clean";
  }

  const fileLabel = snapshot.file_count === 1 ? "1 file" : `${snapshot.file_count} files`;
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
