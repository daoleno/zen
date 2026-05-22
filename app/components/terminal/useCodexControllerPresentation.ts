import { useMemo } from "react";
import type { Agent, ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { buildCodexStatusMeta } from "./CodexChatControllerModel";
import type { ComposerAttachment } from "./CodexChatSession";

interface UseCodexControllerPresentationInput {
  agent?: Agent;
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  draft: string;
  attachments: ComposerAttachment[];
  sending: boolean;
  uploading: boolean;
}

export function useCodexControllerPresentation({
  agent,
  connectionState,
  connectionIssue,
  conversation,
  events,
  draft,
  attachments,
  sending,
  uploading,
}: UseCodexControllerPresentationInput) {
  const statusMeta = useMemo(
    () =>
      buildCodexStatusMeta({
        agent,
        connectionState,
        connectionIssue,
        conversation,
        events,
        sending,
      }),
    [agent, connectionIssue, connectionState, conversation, events, sending],
  );

  const canSend =
    connectionState === "connected" &&
    (draft.trim().length > 0 || attachments.length > 0) &&
    !sending &&
    !uploading;

  return {
    statusMeta,
    canSend,
  };
}
