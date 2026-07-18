import { useCallback } from "react";

interface UseTerminalScreenActionsInput {
  displayName: string;
  closeMenu(): void;
  openGitDiffSheet(): void;
  setRenameDraft(value: string): void;
  setRenameVisible(value: boolean): void;
}

export function useTerminalScreenActions({
  displayName,
  closeMenu,
  openGitDiffSheet,
  setRenameDraft,
  setRenameVisible,
}: UseTerminalScreenActionsInput) {
  const openGitDiff = useCallback(() => {
    closeMenu();
    openGitDiffSheet();
  }, [closeMenu, openGitDiffSheet]);

  const openRenameModal = useCallback(() => {
    closeMenu();
    setRenameDraft(displayName);
    setRenameVisible(true);
  }, [closeMenu, displayName, setRenameDraft, setRenameVisible]);

  return {
    openGitDiff,
    openRenameModal,
  };
}
