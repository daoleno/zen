import React from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../../constants/terminalThemes";
import { TypeScale } from "../../../constants/tokens";
import type { SessionResourceSnapshot } from "../../../services/sessionResourceSnapshot";
import type {
  ProviderError,
  ProviderModelChoice,
  ProviderSessionSelection,
} from "../../../services/providers";
import type { MenuAnchorLayout } from "./TerminalScreenModel";
import {
  SessionModelSheet,
  type SessionModelChoice,
} from "../../providers/SessionModelSheet";
import { GitDiffSheet } from "../GitDiffSheet";
import { NewTerminalSheet } from "../NewTerminalSheet";
import { SessionResourceSheet } from "../SessionResourceSheet";
import { TerminalActionPopover } from "../TerminalActionPopover";
import { TerminalRenameModal } from "../TerminalRenameModal";

type GitDiffSheetProps = React.ComponentProps<typeof GitDiffSheet>;
type NewTerminalSubmitInput = Parameters<
  React.ComponentProps<typeof NewTerminalSheet>["onSubmit"]
>[0];

export interface TerminalScreenOverlaysProps {
  resourceSheetVisible: boolean;
  resourceSheetLoading: boolean;
  resourceSheetError?: string | null;
  resourceSheetSnapshot?: SessionResourceSnapshot | null;
  routeSheetVisible: boolean;
  routeSheetAnchor?: MenuAnchorLayout | null;
  routeSheetLoading: boolean;
  routeSheetActivating: boolean;
  routeSheetError?: ProviderError | string | null;
  routeSheetSelection?: ProviderSessionSelection | null;
  routeSheetChoices: ProviderModelChoice[];
  createDurabilityWarning?: string | null;
  onDismissCreateDurabilityWarning?(): void;
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
  onCloseRouteSheet(): void;
  onRetryRouteSheet(): void;
  onActivateSessionModel(choice: SessionModelChoice): void;
  onOpenModel?(): void;
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
  routeSheetVisible,
  routeSheetAnchor,
  routeSheetLoading,
  routeSheetActivating,
  routeSheetError,
  routeSheetSelection,
  routeSheetChoices,
  createDurabilityWarning,
  onDismissCreateDurabilityWarning,
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
  onCloseRouteSheet,
  onRetryRouteSheet,
  onActivateSessionModel,
  onOpenModel,
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
      {createDurabilityWarning ? (
        <View
          style={[
            styles.durabilityBanner,
            { backgroundColor: chrome.surfaceMuted, borderColor: chrome.border },
          ]}
          accessibilityRole="summary"
        >
          <Text style={[styles.durabilityText, { color: chrome.textMuted }]}>
            {createDurabilityWarning}
          </Text>
          {onDismissCreateDurabilityWarning ? (
            <Pressable
              onPress={onDismissCreateDurabilityWarning}
              accessibilityRole="button"
              accessibilityLabel="Dismiss durability warning"
            >
              <Text style={[styles.durabilityDismiss, { color: chrome.accent }]}>
                Dismiss
              </Text>
            </Pressable>
          ) : null}
        </View>
      ) : null}

      <SessionResourceSheet
        visible={resourceSheetVisible}
        loading={resourceSheetLoading}
        error={resourceSheetError}
        snapshot={resourceSheetSnapshot}
        chrome={chrome}
        onClose={onCloseResourceSheet}
        onRetry={onRetryResourceSheet}
      />

      <SessionModelSheet
        visible={routeSheetVisible}
        anchor={routeSheetAnchor}
        loading={routeSheetLoading}
        activating={routeSheetActivating}
        error={routeSheetError}
        selection={routeSheetSelection}
        choices={routeSheetChoices}
        chrome={chrome}
        onClose={onCloseRouteSheet}
        onRetry={onRetryRouteSheet}
        onActivate={onActivateSessionModel}
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
        onOpenModel={onOpenModel}
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

const styles = StyleSheet.create({
  durabilityBanner: {
    marginHorizontal: 12,
    marginTop: 8,
    marginBottom: 4,
    paddingHorizontal: 12,
    paddingVertical: 10,
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    gap: 6,
  },
  durabilityText: {
    ...TypeScale.caption,
    lineHeight: 16,
  },
  durabilityDismiss: {
    ...TypeScale.label,
  },
});
