import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type SetStateAction,
} from "react";
import { Alert } from "react-native";
import type { ConnectionState } from "../../store/agents";
import { CurrentAttachmentUpload } from "../../services/currentAttachmentUpload";
import {
  createAttachmentUploadOperation,
  pickUploadDocument,
  resolveServerUploadTarget,
  type ActiveAttachmentUpload,
} from "../../services/uploads";
import type { ComposerAttachment } from "./InterfaceChatSession";

const MAX_COMPOSER_ATTACHMENTS = 8;

interface UseInterfaceComposerAttachmentsInput {
  serverId: string;
  connectionState: ConnectionState;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  focusComposer(): void;
}

export function useInterfaceComposerAttachments({
  serverId,
  connectionState,
  setAttachments,
  focusComposer,
}: UseInterfaceComposerAttachmentsInput) {
  const uploadOwnerRef = useRef(new CurrentAttachmentUpload());
  const selectionGenerationRef = useRef(0);
  const [selecting, setSelecting] = useState(false);
  const [activeUpload, setActiveUpload] =
    useState<ActiveAttachmentUpload | null>(null);
  const uploading = selecting || activeUpload !== null;
  const canAttach = connectionState === "connected" && !uploading;

  useEffect(() => {
    setSelecting(false);
    setActiveUpload(null);
    return () => {
      selectionGenerationRef.current += 1;
      uploadOwnerRef.current.cancel();
    };
  }, [serverId]);

  const handleUploadAttachment = useCallback(async () => {
    if (!canAttach) {
      return;
    }
    const selectionGeneration = selectionGenerationRef.current + 1;
    selectionGenerationRef.current = selectionGeneration;
    setSelecting(true);
    let handle: ReturnType<CurrentAttachmentUpload["start"]> | null = null;
    try {
      const asset = await pickUploadDocument();
      if (selectionGenerationRef.current !== selectionGeneration || !asset) {
        return;
      }
      const target = await resolveServerUploadTarget(serverId);
      if (selectionGenerationRef.current !== selectionGeneration) {
        return;
      }

      setSelecting(false);
      setActiveUpload({ name: asset.name || "upload", progress: null });
      handle = uploadOwnerRef.current.start(
        (onProgress) =>
          createAttachmentUploadOperation(asset, target, { onProgress }),
        (progress) => {
          setActiveUpload((current) =>
            current ? { ...current, progress } : current,
          );
        },
      );
      const attachment = await handle.result;
      if (!uploadOwnerRef.current.finish(handle)) {
        return;
      }
      setActiveUpload(null);
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
      if (handle && !uploadOwnerRef.current.finish(handle)) {
        return;
      }
      setActiveUpload(null);
      Alert.alert("Upload failed", uploadErrorMessage(err));
    } finally {
      if (selectionGenerationRef.current === selectionGeneration) {
        setSelecting(false);
      }
    }
  }, [canAttach, focusComposer, serverId, setAttachments]);

  const cancelUpload = useCallback(() => {
    selectionGenerationRef.current += 1;
    const cancellationError = uploadOwnerRef.current.cancel();
    setSelecting(false);
    setActiveUpload(null);
    if (cancellationError) {
      Alert.alert("Cancel failed", uploadErrorMessage(cancellationError));
    }
  }, []);

  const removeAttachment = useCallback(
    (id: string) => {
      setAttachments((current) =>
        current.filter((attachment) => attachment.id !== id),
      );
    },
    [setAttachments],
  );

  return {
    activeUpload,
    canAttach,
    cancelUpload,
    handleUploadAttachment,
    removeAttachment,
    uploading,
  };
}

function uploadErrorMessage(err: any) {
  const message = typeof err?.message === "string" ? err.message.trim() : "";
  return message || "Could not upload this file.";
}
