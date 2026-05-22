import { useCallback } from "react";
import * as Clipboard from "expo-clipboard";
import type { Agent, ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { CodexSlashCommand } from "../../services/websocket";
import type { ChatCommandEvent } from "./CodexChatSession";
import { buildChatStatusCommandBody } from "./CodexChatControllerModel";
import { latestAssistantMessageBody } from "./CodexTimelineModel";

interface GitDiffAction {
  label: string;
  tone: "clean" | "dirty" | "error" | "loading";
  onPress(): void;
}

interface UseCodexNativeCommandsInput {
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  slashCommands: CodexSlashCommand[];
  statusMeta: string;
  gitDiff?: GitDiffAction | null;
  onOpenGitDiff?: () => void;
  clearComposerForLocalCommand(): void;
  recordChatCommandEvent(
    event: Omit<ChatCommandEvent, "id" | "createdAt">,
  ): void;
}

export function useCodexNativeCommands({
  agent,
  connectionState,
  connectionIssue,
  conversation,
  events,
  slashCommands,
  statusMeta,
  gitDiff,
  onOpenGitDiff,
  clearComposerForLocalCommand,
  recordChatCommandEvent,
}: UseCodexNativeCommandsInput) {
  return useCallback(
    async (command: CodexSlashCommand) => {
      clearComposerForLocalCommand();
      switch (command.name) {
        case "status":
          recordChatCommandEvent({
            command,
            tone: connectionState === "connected" ? "success" : "failed",
            title: "Session Status",
            detail: statusMeta,
            body: buildChatStatusCommandBody({
              agent,
              conversation,
              connectionState,
              connectionIssue,
              slashCommands,
            }),
          });
          return;
        case "diff":
          if (onOpenGitDiff || gitDiff?.onPress) {
            (onOpenGitDiff ?? gitDiff?.onPress)?.();
            recordChatCommandEvent({
              command,
              tone: gitDiff?.tone === "error" ? "failed" : "success",
              title: "Opened Git Diff",
              detail: gitDiff?.label || command.value,
              body: gitDiff?.label
                ? `Git diff panel opened. Current summary: ${gitDiff.label}`
                : "Git diff panel opened.",
            });
            return;
          }
          recordChatCommandEvent({
            command,
            tone: "failed",
            title: "Git Diff Unavailable",
            detail: command.value,
            body: "Zen does not have a working directory for this Codex session yet.",
          });
          return;
        case "copy": {
          const latestAssistantMessage = latestAssistantMessageBody(events);
          if (!latestAssistantMessage) {
            recordChatCommandEvent({
              command,
              tone: "failed",
              title: "Nothing to Copy",
              detail: command.value,
              body: "No assistant response is available in the current transcript.",
            });
            return;
          }
          try {
            await Clipboard.setStringAsync(latestAssistantMessage);
            recordChatCommandEvent({
              command,
              tone: "success",
              title: "Copied Last Response",
              detail: `${Array.from(latestAssistantMessage).length} chars`,
              body: "The latest assistant response was copied as markdown.",
            });
          } catch (err: any) {
            recordChatCommandEvent({
              command,
              tone: "failed",
              title: "Copy Failed",
              detail: command.value,
              body: err?.message || "Zen could not write to the clipboard.",
            });
          }
          return;
        }
        default:
          recordChatCommandEvent({
            command,
            tone: "failed",
            title: "Command Not Available",
            detail: command.value,
            body: "Zen does not have a native chat renderer for this slash command yet.",
          });
      }
    },
    [
      agent,
      clearComposerForLocalCommand,
      connectionIssue,
      connectionState,
      conversation,
      events,
      gitDiff,
      onOpenGitDiff,
      recordChatCommandEvent,
      slashCommands,
      statusMeta,
    ],
  );
}
