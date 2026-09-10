export type GitDiffViewMode = "changes" | "files";

export interface GitDiffBackState {
  view: GitDiffViewMode;
  hasSelectedFile: boolean;
  hasBrowserFile: boolean;
}

export type GitDiffBackAction =
  | "close"
  | "deselect-file"
  | "close-browser-file"
  | "browser-to-changes";

/**
 * Resolves what an Android hardware back (or the header back control) should unwind.
 * A child state must never close the whole feature; only the overview closes.
 */
export function resolveGitDiffBack(state: GitDiffBackState): GitDiffBackAction {
  if (state.view === "files") {
    return state.hasBrowserFile ? "close-browser-file" : "browser-to-changes";
  }
  return state.hasSelectedFile ? "deselect-file" : "close";
}
