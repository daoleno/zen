import { useCallback, useState, type SetStateAction } from "react";
import { Alert } from "react-native";
import type { ConnectionState } from "../../store/agents";
import { uploadDocumentForServer } from "../../services/uploads";
import type { ComposerAttachment } from "./CodexChatSession";

const MAX_COMPOSER_ATTACHMENTS = 8;

interface UseCodexComposerAttachmentsInput {
  serverId: string;
  connectionState: ConnectionState;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  focusComposer(): void;
}

export function useCodexComposerAttachments({
  serverId,
  connectionState,
  setAttachments,
  focusComposer,
}: UseCodexComposerAttachmentsInput) {
  const [uploading, setUploading] = useState(false);
  const canAttach = connectionState === "connected" && !uploading;

  const handleUploadAttachment = useCallback(async () => {
    if (!canAttach) {
      return;
    }
    setUploading(true);
    try {
      const attachment = await uploadDocumentForServer(serverId);
      if (!attachment) {
        return;
      }
      setAttachments((current) =>
        [
          ...current,
          {
            ...attachment,
            id: `${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 8)}`,
          },
        ].slice(-MAX_COMPOSER_ATTACHMENTS),
      );
      focusComposer();
    } catch (err: any) {
      Alert.alert("Upload failed", err?.message || "Could not upload this file.");
    } finally {
      setUploading(false);
    }
  }, [canAttach, focusComposer, serverId, setAttachments]);

  const removeAttachment = useCallback(
    (id: string) => {
      setAttachments((current) =>
        current.filter((attachment) => attachment.id !== id),
      );
    },
    [setAttachments],
  );

  return {
    canAttach,
    handleUploadAttachment,
    removeAttachment,
    uploading,
  };
}
