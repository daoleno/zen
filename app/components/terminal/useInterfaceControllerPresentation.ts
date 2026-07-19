import { useMemo } from "react";
import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  ProviderActivity,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import { buildInterfaceStatusMeta } from "./InterfaceChatControllerModel";
import type { ComposerAttachment } from "./InterfaceChatSession";

interface UseInterfaceControllerPresentationInput {
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  runningActivity?: ProviderActivity;
  draft: string;
  attachments: ComposerAttachment[];
  sending: boolean;
  uploading: boolean;
}

export function useInterfaceControllerPresentation({
  connectionState,
  connectionIssue,
  conversation,
  runningActivity,
  draft,
  attachments,
  sending,
  uploading,
}: UseInterfaceControllerPresentationInput) {
  const statusMeta = useMemo(
    () =>
      buildInterfaceStatusMeta({
        connectionState,
        connectionIssue,
        conversation,
        runningActivity,
        sending,
      }),
    [connectionIssue, connectionState, conversation, runningActivity, sending],
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
