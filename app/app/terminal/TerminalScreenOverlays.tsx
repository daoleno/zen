import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { SessionResourceSnapshot } from "../../services/sessionResourceSnapshot";
import { GitDiffSheet } from "../../components/terminal/GitDiffSheet";
import { NewTerminalSheet } from "../../components/terminal/NewTerminalSheet";
import { SessionResourceSheet } from "../../components/terminal/SessionResourceSheet";
import { TerminalActionPopover } from "../../components/terminal/TerminalActionPopover";
import { TerminalRenameModal } from "../../components/terminal/TerminalRenameModal";

type GitDiffSheetProps = React.ComponentProps<typeof GitDiffSheet>;
type NewTerminalSubmitInput = Parameters<
  React.ComponentProps<typeof NewTerminalSheet>["onSubmit"]
>[0];

export interface TerminalScreenOverlaysProps {
  resourceSheetVisible: boolean;
  resourceSheetLoading: boolean;
  resourceSheetError?: string | null;
  resourceSheetSnapshot?: SessionResourceSnapshot | null;
  creatingSession: boolean;
  menuVisible: boolean;
  menuPosition: { left: number; top: number };
  newTerminalDisabled: boolean;
  showLinkedWork: boolean;
  showToggleRenderMode?: boolean;
  toggleRenderModeLabel?: string;
  newTerminalVisible: boolean;
  newTerminalInitialCwd: string;
  selectedServerId: string;
  gitDiffSheetProps: Omit<GitDiffSheetProps, "theme">;
  renameVisible: boolean;
  renameDraft: string;
  renamePlaceholder: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onCloseResourceSheet(): void;
  onRetryResourceSheet(): void;
  onNewTerminal(): void;
  onCloseMenu(): void;
  onRename(): void;
  onOpenLinkedWork(): void;
  onToggleRenderMode?(): void;
  onTerminate(): void;
  onCloseNewTerminal(): void;
  onSubmitNewTerminal(input: NewTerminalSubmitInput): void;
  onRenameDraftChange(value: string): void;
  onCloseRename(): void;
  onSaveRename(): void;
}

export function TerminalScreenOverlays({
  resourceSheetVisible,
  resourceSheetLoading,
  resourceSheetError,
  resourceSheetSnapshot,
  creatingSession,
  menuVisible,
  menuPosition,
  newTerminalDisabled,
  showLinkedWork,
  showToggleRenderMode = false,
  toggleRenderModeLabel,
  newTerminalVisible,
  newTerminalInitialCwd,
  selectedServerId,
  gitDiffSheetProps,
  renameVisible,
  renameDraft,
  renamePlaceholder,
  chrome,
  theme,
  onCloseResourceSheet,
  onRetryResourceSheet,
  onNewTerminal,
  onCloseMenu,
  onRename,
  onOpenLinkedWork,
  onToggleRenderMode,
  onTerminate,
  onCloseNewTerminal,
  onSubmitNewTerminal,
  onRenameDraftChange,
  onCloseRename,
  onSaveRename,
}: TerminalScreenOverlaysProps) {
  return (
    <>
      <SessionResourceSheet
        visible={resourceSheetVisible}
        loading={resourceSheetLoading}
        error={resourceSheetError}
        snapshot={resourceSheetSnapshot}
        chrome={chrome}
        onClose={onCloseResourceSheet}
        onRetry={onRetryResourceSheet}
      />

      <TerminalActionPopover
        visible={menuVisible}
        left={menuPosition.left}
        top={menuPosition.top}
        creatingSession={creatingSession}
        newTerminalLabel={creatingSession ? "Starting Terminal…" : "New Terminal"}
        newTerminalDisabled={newTerminalDisabled}
        showLinkedWork={showLinkedWork}
        showToggleRenderMode={showToggleRenderMode}
        toggleRenderModeLabel={toggleRenderModeLabel}
        chrome={chrome}
        theme={theme}
        onClose={onCloseMenu}
        onNewTerminal={onNewTerminal}
        onRename={onRename}
        onOpenLinkedWork={onOpenLinkedWork}
        onToggleRenderMode={onToggleRenderMode}
        onTerminate={onTerminate}
      />

      <NewTerminalSheet
        visible={newTerminalVisible}
        title="Session"
        subtitle=""
        initialCwd={newTerminalInitialCwd}
        selectedServerId={selectedServerId}
        submitting={creatingSession}
        onClose={onCloseNewTerminal}
        onSubmit={onSubmitNewTerminal}
      />

      <GitDiffSheet
        theme={theme}
        {...gitDiffSheetProps}
      />

      <TerminalRenameModal
        visible={renameVisible}
        draft={renameDraft}
        placeholder={renamePlaceholder}
        chrome={chrome}
        theme={theme}
        onDraftChange={onRenameDraftChange}
        onClose={onCloseRename}
        onSave={onSaveRename}
      />
    </>
  );
}
