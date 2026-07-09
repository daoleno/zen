import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { AgentDirectorySection } from "../../services/serverSelection";
import type {
  StoredAgentAliases,
} from "../../services/storage";
import { GitDiffSheet } from "../../components/terminal/GitDiffSheet";
import { NewTerminalSheet } from "../../components/terminal/NewTerminalSheet";
import { TerminalAgentPickerSheet } from "../../components/terminal/TerminalAgentPickerSheet";
import { TerminalActionPopover } from "../../components/terminal/TerminalActionPopover";
import { TerminalRenameModal } from "../../components/terminal/TerminalRenameModal";

type GitDiffSheetProps = React.ComponentProps<typeof GitDiffSheet>;
type NewTerminalSubmitInput = Parameters<
  React.ComponentProps<typeof NewTerminalSheet>["onSubmit"]
>[0];

export interface TerminalScreenOverlaysProps {
  pickerVisible: boolean;
  pickerSections: AgentDirectorySection[];
  pickerAgentCount: number;
  activeSessionKey: string | null;
  showPickerServerNames: boolean;
  agentAliases: StoredAgentAliases;
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
  onClosePicker(): void;
  onOpenAgent(sessionKey: string): void;
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
  pickerVisible,
  pickerSections,
  pickerAgentCount,
  activeSessionKey,
  showPickerServerNames,
  agentAliases,
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
  onClosePicker,
  onOpenAgent,
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
      <TerminalAgentPickerSheet
        visible={pickerVisible}
        sections={pickerSections}
        agentCount={pickerAgentCount}
        activeSessionKey={activeSessionKey}
        showServerNames={showPickerServerNames}
        agentAliases={agentAliases}
        creatingSession={creatingSession}
        chrome={chrome}
        onClose={onClosePicker}
        onOpenAgent={onOpenAgent}
        onNewTerminal={onNewTerminal}
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
