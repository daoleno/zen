import { useCallback } from "react";
import type { ConnectionState } from "../../store/agents";
import { buildCodexComposerMessage } from "./CodexChatControllerModel";
import type { ComposerAttachment } from "./CodexChatSession";

interface RouteDraftSubmissionInput {
  draft: string;
  composedText: string;
  previousDraft: string;
  previousAttachments: ComposerAttachment[];
}

interface UseCodexDraftSubmissionInput {
  draft: string;
  attachments: ComposerAttachment[];
  connectionState: ConnectionState;
  sending: boolean;
  uploading: boolean;
  routeDraftSubmission(input: RouteDraftSubmissionInput): boolean;
  submitTextToCodex(
    text: string,
    previousDraft: string,
    previousAttachments: ComposerAttachment[],
  ): void;
}

export function useCodexDraftSubmission({
  draft,
  attachments,
  connectionState,
  sending,
  uploading,
  routeDraftSubmission,
  submitTextToCodex,
}: UseCodexDraftSubmissionInput) {
  return useCallback(() => {
    const text = buildCodexComposerMessage(draft, attachments);
    if (!text || connectionState !== "connected" || sending || uploading) {
      return;
    }
    const previousDraft = draft;
    const previousAttachments = attachments;
    if (
      routeDraftSubmission({
        draft,
        composedText: text,
        previousDraft,
        previousAttachments,
      })
    ) {
      return;
    }
    submitTextToCodex(text, previousDraft, previousAttachments);
  }, [
    attachments,
    connectionState,
    draft,
    routeDraftSubmission,
    sending,
    submitTextToCodex,
    uploading,
  ]);
}
