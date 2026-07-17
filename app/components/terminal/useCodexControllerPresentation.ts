import { useMemo } from "react";
import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  ProviderActivity,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { buildCodexStatusMeta } from "./CodexChatControllerModel";
import type { ComposerAttachment } from "./CodexChatSession";

interface UseCodexControllerPresentationInput {
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  runningActivity?: ProviderActivity;
  draft: string;
  attachments: ComposerAttachment[];
  sending: boolean;
  uploading: boolean;
}

export function useCodexControllerPresentation({
  connectionState,
  connectionIssue,
  conversation,
  runningActivity,
  draft,
  attachments,
  sending,
  uploading,
}: UseCodexControllerPresentationInput) {
  const statusMeta = useMemo(
    () =>
      buildCodexStatusMeta({
        connectionState,
        connectionIssue,
        conversation,
        runningActivity,
        sending,
      }),
    [
      connectionIssue,
      connectionState,
      conversation,
      runningActivity,
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
