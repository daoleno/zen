import {
  useCallback,
  type SetStateAction,
} from "react";
import { wsClient, type CodexSlashCommand } from "../../services/websocket";
import {
  type ChatCommandEvent,
  type ComposerAttachment,
} from "./CodexChatSession";
import { slashCommandTerminalText } from "./CodexSlashCommands";

interface UseCodexTerminalCommandActionsInput {
  serverId: string;
  agentId: string;
  draft: string;
  attachments: ComposerAttachment[];
  setDraft(value: string): void;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  recordChatCommandEvent(
    event: Omit<ChatCommandEvent, "id" | "createdAt">,
  ): void;
  scrollToLatest(animated?: boolean, delay?: number): void;
  onSwitchToTerminal(): void;
}

export function useCodexTerminalCommandActions({
  serverId,
  agentId,
  draft,
  attachments,
  setDraft,
  setAttachments,
  recordChatCommandEvent,
  scrollToLatest,
  onSwitchToTerminal,
}: UseCodexTerminalCommandActionsInput) {
  const clearComposerForLocalCommand = useCallback(() => {
    setDraft("");
    setAttachments([]);
    scrollToLatest(true, 0);
  }, [scrollToLatest, setAttachments, setDraft]);

  const openSlashCommandInTerminal = useCallback(
    (command: CodexSlashCommand, rawText?: string) => {
      const text = slashCommandTerminalText(command, rawText);
      const previousDraft = draft;
      const previousAttachments = attachments;
      setDraft("");
      setAttachments([]);
      try {
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        recordChatCommandEvent({
          command,
          tone: "neutral",
          title: "Opened in Terminal",
          detail: command.value,
          body: command.interactive
            ? "This command uses the terminal renderer because it can open prompts, pickers, or terminal-only output."
            : "This command was routed to the terminal renderer.",
        });
        onSwitchToTerminal();
      } catch {
        setDraft(previousDraft);
        setAttachments(previousAttachments);
        recordChatCommandEvent({
          command,
          tone: "failed",
          title: "Command Not Sent",
          detail: command.value,
          body: "Zen could not send this command to the terminal session.",
        });
      }
    },
    [
      agentId,
      attachments,
      draft,
      onSwitchToTerminal,
      recordChatCommandEvent,
      serverId,
      setAttachments,
      setDraft,
    ],
  );

  return {
    clearComposerForLocalCommand,
    openSlashCommandInTerminal,
  };
}
