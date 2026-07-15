import { useMemo } from "react";
import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { AgentStatus } from "../../constants/tokens";
import { buildCodexStatusMeta } from "./CodexChatControllerModel";
import type { ComposerAttachment } from "./CodexChatSession";

interface UseCodexControllerPresentationInput {
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  agentStatus?: AgentStatus;
  draft: string;
  attachments: ComposerAttachment[];
  sending: boolean;
  uploading: boolean;
  requestRunning: boolean;
}

export function useCodexControllerPresentation({
  connectionState,
  connectionIssue,
  conversation,
  events,
  agentStatus,
  draft,
  attachments,
  sending,
  uploading,
  requestRunning,
}: UseCodexControllerPresentationInput) {
  const statusMeta = useMemo(
    () =>
      buildCodexStatusMeta({
        connectionState,
        connectionIssue,
        conversation,
        events,
        agentStatus,
        sending,
        requestRunning,
      }),
    [
      agentStatus,
      connectionIssue,
      connectionState,
      conversation,
      events,
      requestRunning,
      sending,
    ],
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
