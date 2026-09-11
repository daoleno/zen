export type GitDiffViewMode = "changes" | "files";

export interface GitDiffBackState {
  view: GitDiffViewMode;
  hasSelectedFile: boolean;
  hasBrowserFile: boolean;
  /** Where the currently open working file was opened from. */
  fileOrigin: GitDiffViewMode;
}

export type GitDiffBackAction =
  | "close"
  | "deselect-file"
  | "close-browser-file-to-reader"
  | "close-browser-file-to-browser"
  | "browser-to-changes";

/**
 * Resolves what Android hardware back (or a header back control) should unwind.
 * A child state must never close the whole feature; only the overview closes.
 * Closing a working file returns to the state it was opened from, so every
 * exposed Back control shares one destination.
 */
export function resolveGitDiffBack(state: GitDiffBackState): GitDiffBackAction {
  if (state.view === "files") {
    if (!state.hasBrowserFile) return "browser-to-changes";
    return state.fileOrigin === "changes"
      ? "close-browser-file-to-reader"
      : "close-browser-file-to-browser";
  }
  return state.hasSelectedFile ? "deselect-file" : "close";
}
