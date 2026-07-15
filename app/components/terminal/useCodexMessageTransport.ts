import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type SetStateAction,
} from "react";
import { Alert, Keyboard } from "react-native";
import type { ConnectionState } from "../../store/agents";
import type { StructuredTurn } from "../../services/codexConversation";
import type { CodexSlashCommand } from "../../services/websocket";
import { wsClient } from "../../services/websocket";
import type {
  ComposerAttachment,
  PendingUserMessageAcknowledgement,
  PendingUserMessageInput,
} from "./CodexChatSession";
import { createStructuredTurnIdentity } from "./structuredTurnLifecycle";
import {
  restoreFailedAttachments,
  restoreFailedDraft,
} from "./messageSendRecovery";
import { submitProviderCommandAsUserInput } from "./providerCommandSubmission";
import {
  beginComposerStop,
  reconcileComposerStopLatch,
  releaseComposerStopLatch,
} from "./composerStopLatch";

interface UseCodexMessageTransportInput {
  serverId: string;
  agentId: string;
  conversationScopeKey?: string;
  conversationIdentity?: string;
  connectionState: ConnectionState;
  turnBusy: boolean;
  workingTurn?: StructuredTurn;
  draft: string;
  attachments: ComposerAttachment[];
  setDraft(value: string): void;
  restoreDraft(value: string): void;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  clearComposerNativeText(): void;
  addPendingUserMessage(message: PendingUserMessageInput): string;
  acknowledgePendingUserMessage(
    id: string,
    acknowledgement: PendingUserMessageAcknowledgement,
  ): void;
  removePendingUserMessage(id: string): void;
  markNewChatMessageStarted(): void;
  pinToBottomIfNeeded(animated?: boolean, delay?: number): void;
}

export function useCodexMessageTransport({
  serverId,
  agentId,
  conversationScopeKey,
  conversationIdentity,
  connectionState,
  turnBusy,
  workingTurn,
  draft,
  attachments,
  setDraft,
  restoreDraft,
  setAttachments,
  clearComposerNativeText,
  addPendingUserMessage,
  acknowledgePendingUserMessage,
  removePendingUserMessage,
  markNewChatMessageStarted,
  pinToBottomIfNeeded,
}: UseCodexMessageTransportInput) {
  const [sending, setSending] = useState(false);
  const [interrupting, setInterrupting] = useState(false);
  const sendLockedRef = useRef(false);
  const interruptTurnIdRef = useRef<string | undefined>(undefined);
  const currentDraftRef = useRef(draft);
  const currentAttachmentsRef = useRef(attachments);
  currentDraftRef.current = draft;
  currentAttachmentsRef.current = attachments;

  const unlockSend = useCallback(() => {
    sendLockedRef.current = false;
    setSending(false);
  }, []);

  useEffect(
    () => () => {
      sendLockedRef.current = false;
      interruptTurnIdRef.current = undefined;
    },
    [],
  );

  useEffect(() => {
    const reconciled = reconcileComposerStopLatch(
      interruptTurnIdRef.current,
      workingTurn?.id,
    );
    if (reconciled === interruptTurnIdRef.current) {
      return;
    }
    interruptTurnIdRef.current = reconciled;
    setInterrupting(false);
  }, [workingTurn?.id]);

  const submitTextToCodex = useCallback(
    (
      text: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      if (sendLockedRef.current) {
        return;
      }
      sendLockedRef.current = true;
      markNewChatMessageStarted();
      const turnIdentity = createStructuredTurnIdentity();
      const pendingMessageId = addPendingUserMessage({
        turnId: turnIdentity.id,
        turnStartedAt: turnIdentity.startedAt,
        body: previousDraft.trim(),
        sentText: text,
        attachments: previousAttachments.map((attachment) => ({
          name: attachment.name,
          path: attachment.path,
          localUri: attachment.localUri,
          mimeType: attachment.mimeType,
        })),
        lifecycle: "sending",
      });
      setSending(true);
      setDraft("");
      clearComposerNativeText();
      setAttachments([]);
      currentDraftRef.current = "";
      currentAttachmentsRef.current = [];
      pinToBottomIfNeeded(false, 0);
      void (async () => {
        try {
          const accepted = await wsClient.sendInput(
            serverId,
            agentId,
            `${text}\n`,
            {
              conversationScopeKey,
              turnId: turnIdentity.id,
              turnStartedAt: turnIdentity.startedAt,
              turnQueued: turnBusy,
              turnConversationIdentity: conversationIdentity,
            },
          );
          acknowledgePendingUserMessage(pendingMessageId, {
            turnId: accepted.turnId || turnIdentity.id,
            lifecycle: accepted.queued ? "queued" : "sending",
            acceptedAt: new Date().toISOString(),
            turnEpoch: accepted.turnEpoch,
            turnRevision: accepted.turnRevision,
          });
          unlockSend();
        } catch (err: any) {
          sendLockedRef.current = false;
          removePendingUserMessage(pendingMessageId);
          const restoredDraft = restoreFailedDraft(
            previousDraft,
            currentDraftRef.current,
          );
          const restoredAttachments = restoreFailedAttachments(
            previousAttachments,
            currentAttachmentsRef.current,
          );
          currentDraftRef.current = restoredDraft;
          currentAttachmentsRef.current = restoredAttachments;
          restoreDraft(restoredDraft);
          setAttachments(restoredAttachments);
          setSending(false);
          Alert.alert(
            "Message not sent",
            err?.message || "Could not send this message.",
          );
        }
      })();
    },
    [
      agentId,
      acknowledgePendingUserMessage,
      addPendingUserMessage,
      clearComposerNativeText,
      conversationScopeKey,
      conversationIdentity,
      markNewChatMessageStarted,
      removePendingUserMessage,
      restoreDraft,
      pinToBottomIfNeeded,
      serverId,
      setAttachments,
      setDraft,
      turnBusy,
      unlockSend,
    ],
  );

  const startNewCodexChat = useCallback((
    commandText: string = "/new",
    previousDraft: string = commandText,
    previousAttachments: ComposerAttachment[] = [],
  ) => {
    const submittedText = commandText.trim() || "/new";
    if (!turnBusy) {
      Keyboard.dismiss();
    }
    submitProviderCommandAsUserInput(
      submittedText,
      previousDraft,
      previousAttachments,
      submitTextToCodex,
    );
  }, [
    submitTextToCodex,
    turnBusy,
  ]);

  const sendSlashCommandToCodex = useCallback(
    (
      text: string,
      _command?: CodexSlashCommand,
      previousDraft?: string,
      previousAttachments?: ComposerAttachment[],
    ) => {
      submitProviderCommandAsUserInput(
        text,
        previousDraft,
        previousAttachments,
        submitTextToCodex,
      );
    },
    [submitTextToCodex],
  );

  const interruptCodex = useCallback(() => {
    if (connectionState !== "connected") {
      return;
    }
    const latch = beginComposerStop(
      interruptTurnIdRef.current,
      workingTurn?.id,
    );
    if (!latch.accepted) {
      return;
    }
    interruptTurnIdRef.current = latch.latchedTurnId;
    const claimedTurnId = latch.latchedTurnId;
    setInterrupting(true);
    try {
      const request = wsClient.sendAction(serverId, agentId, "pause", {
        conversationScopeKey,
        turnId: workingTurn?.id,
        turnStartedAt: workingTurn?.started_at,
      });
      void request.catch((error: any) => {
        const previousLatch = interruptTurnIdRef.current;
        const nextLatch = releaseComposerStopLatch(
          previousLatch,
          claimedTurnId,
        );
        if (nextLatch === previousLatch) {
          return;
        }
        interruptTurnIdRef.current = nextLatch;
        setInterrupting(false);
        Alert.alert(
          "Response not stopped",
          error?.message || "Could not stop this response.",
        );
      });
    } catch (error: any) {
      const previousLatch = interruptTurnIdRef.current;
      const nextLatch = releaseComposerStopLatch(
        previousLatch,
        claimedTurnId,
      );
      interruptTurnIdRef.current = nextLatch;
      if (nextLatch !== previousLatch) {
        setInterrupting(false);
        Alert.alert(
          "Response not stopped",
          error?.message || "Could not stop this response.",
        );
      }
    }
  }, [
    agentId,
    connectionState,
    conversationScopeKey,
    serverId,
    workingTurn?.id,
    workingTurn?.started_at,
  ]);

  return {
    sending,
    interrupting,
    startingNewChat: false,
    submitTextToCodex,
    startNewCodexChat,
    sendSlashCommandToCodex,
    interruptCodex,
  };
}
