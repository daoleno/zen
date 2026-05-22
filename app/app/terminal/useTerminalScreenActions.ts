import { useCallback } from "react";

interface UseTerminalScreenActionsInput {
  displayName: string;
  closeMenu(): void;
  openGitDiffSheet(): void;
  setPickerVisible(value: boolean): void;
  setRenameDraft(value: string): void;
  setRenameVisible(value: boolean): void;
}

export function useTerminalScreenActions({
  displayName,
  closeMenu,
  openGitDiffSheet,
  setPickerVisible,
  setRenameDraft,
  setRenameVisible,
}: UseTerminalScreenActionsInput) {
  const closePicker = useCallback(() => {
    setPickerVisible(false);
  }, [setPickerVisible]);

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
    closePicker,
    openGitDiff,
    openRenameModal,
  };
}
