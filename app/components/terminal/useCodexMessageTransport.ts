import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type SetStateAction,
} from "react";
import { Keyboard } from "react-native";
import type { ConnectionState } from "../../store/agents";
import type { StructuredTurn } from "../../services/codexConversation";
import type { CodexSlashCommand } from "../../services/websocket";
import { wsClient } from "../../services/websocket";
import type {
  ComposerAttachment,
  PendingUserMessage,
  PendingUserMessageAcknowledgement,
  PendingUserMessageDispatchAttempt,
  PendingUserMessageInput,
  PendingUserMessageRejection,
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
  pendingUserMessages: PendingUserMessage[];
  setDraft(value: string): void;
  restoreDraft(value: string): void;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  clearComposerNativeText(): void;
  addPendingUserMessage(message: PendingUserMessageInput): string;
  acknowledgePendingUserMessage(
    id: string,
    acknowledgement: PendingUserMessageAcknowledgement,
  ): void;
  markPendingUserMessageDispatched(
    id: string,
    attempt: PendingUserMessageDispatchAttempt,
  ): void;
  rejectPendingUserMessage(
    id: string,
    rejection: PendingUserMessageRejection,
  ): void;
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
  pendingUserMessages,
  setDraft,
  restoreDraft,
  setAttachments,
  clearComposerNativeText,
  addPendingUserMessage,
  acknowledgePendingUserMessage,
  markPendingUserMessageDispatched,
  rejectPendingUserMessage,
  markNewChatMessageStarted,
  pinToBottomIfNeeded,
}: UseCodexMessageTransportInput) {
  const [sending, setSending] = useState(false);
  const [interrupting, setInterrupting] = useState(false);
  const [operationalError, setOperationalError] = useState<string>();
  const sendLockedRef = useRef(false);
  const interruptTurnIdRef = useRef<string | undefined>(undefined);
  const retryRequestByPendingIdRef = useRef(new Map<string, string>());
  const currentDraftRef = useRef(draft);
  const currentAttachmentsRef = useRef(attachments);
  const pendingUserMessagesRef = useRef(pendingUserMessages);
  currentDraftRef.current = draft;
  currentAttachmentsRef.current = attachments;
  pendingUserMessagesRef.current = pendingUserMessages;

  const unlockSend = useCallback(() => {
    sendLockedRef.current = false;
    setSending(false);
  }, []);

  useEffect(
    () => () => {
      sendLockedRef.current = false;
      interruptTurnIdRef.current = undefined;
      retryRequestByPendingIdRef.current.clear();
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

  useEffect(() => {
    const pendingById = new Map(
      pendingUserMessages.map((message) => [message.id, message]),
    );
    for (const [id, requestId] of retryRequestByPendingIdRef.current) {
      const message = pendingById.get(id);
      if (
        !message ||
        (message.lifecycle === "failed" &&
          message.dispatchRequestId === requestId)
      ) {
        retryRequestByPendingIdRef.current.delete(id);
      }
    }
  }, [pendingUserMessages]);

  useEffect(() => {
    setOperationalError(undefined);
  }, [agentId, conversationScopeKey, serverId]);

  const observeInputOutcome = useCallback((
    pendingMessageId: string,
    receipt: ReturnType<typeof wsClient.sendInput>,
    fallbackTurnId: string,
  ) => {
    void receipt.outcome.then((outcome) => {
      if (outcome.kind === "confirmed") {
        const accepted = outcome.value;
        acknowledgePendingUserMessage(pendingMessageId, {
          requestId: receipt.requestId,
          turnId: accepted.turnId || fallbackTurnId,
          lifecycle: accepted.queued ? "queued" : "sending",
          acceptedAt: new Date().toISOString(),
          turnEpoch: accepted.turnEpoch,
          turnRevision: accepted.turnRevision,
        });
        return;
      }
      if (outcome.kind === "rejected") {
        rejectPendingUserMessage(pendingMessageId, {
          requestId: outcome.rejection.requestId,
          code: outcome.rejection.code,
          message: outcome.rejection.message,
          failedAt: new Date().toISOString(),
        });
      }
    });
  }, [acknowledgePendingUserMessage, rejectPendingUserMessage]);

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
      setSending(true);
      setOperationalError(undefined);
      const turnIdentity = createStructuredTurnIdentity();
      let receipt: ReturnType<typeof wsClient.sendInput>;
      try {
        receipt = wsClient.sendInput(
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
      } catch (error: any) {
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
        setOperationalError(
          error?.message || "Message was not dispatched. Send to retry.",
        );
        unlockSend();
        return;
      }
      markNewChatMessageStarted();
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
        lifecycle: "unconfirmed",
        queuedHint: turnBusy,
        dispatchRequestId: receipt.requestId,
        lastAttemptAt: turnIdentity.startedAt,
      });
      setDraft("");
      clearComposerNativeText();
      setAttachments([]);
      currentDraftRef.current = "";
      currentAttachmentsRef.current = [];
      pinToBottomIfNeeded(false, 0);
      unlockSend();
      observeInputOutcome(pendingMessageId, receipt, turnIdentity.id);
    },
    [
      agentId,
      acknowledgePendingUserMessage,
      addPendingUserMessage,
      clearComposerNativeText,
      conversationScopeKey,
      conversationIdentity,
      markNewChatMessageStarted,
      observeInputOutcome,
      restoreDraft,
      pinToBottomIfNeeded,
      serverId,
      setAttachments,
      setDraft,
      turnBusy,
      unlockSend,
    ],
  );

  const retryPendingUserMessage = useCallback((pendingMessageId: string) => {
    if (
      sendLockedRef.current ||
      retryRequestByPendingIdRef.current.has(pendingMessageId)
    ) {
      return;
    }
    const message = pendingUserMessagesRef.current.find(
      (candidate) =>
        candidate.id === pendingMessageId && candidate.lifecycle === "failed",
    );
    if (!message) {
      return;
    }
    sendLockedRef.current = true;
    retryRequestByPendingIdRef.current.set(pendingMessageId, "dispatching");
    setSending(true);
    setOperationalError(undefined);
    let receipt: ReturnType<typeof wsClient.sendInput>;
    try {
      receipt = wsClient.sendInput(
        serverId,
        agentId,
        `${message.sentText}\n`,
        {
          conversationScopeKey,
          turnId: message.turnId,
          turnStartedAt: message.turnStartedAt,
          turnQueued: turnBusy,
          turnConversationIdentity: conversationIdentity,
        },
      );
    } catch (error: any) {
      retryRequestByPendingIdRef.current.delete(pendingMessageId);
      setOperationalError(
        error?.message || "Retry was not dispatched. Try again.",
      );
      unlockSend();
      return;
    }
    retryRequestByPendingIdRef.current.set(
      pendingMessageId,
      receipt.requestId,
    );
    markPendingUserMessageDispatched(pendingMessageId, {
      requestId: receipt.requestId,
      attemptedAt: new Date().toISOString(),
      queuedHint: turnBusy,
    });
    pinToBottomIfNeeded(false, 0);
    unlockSend();
    observeInputOutcome(pendingMessageId, receipt, message.turnId);
  }, [
    agentId,
    conversationIdentity,
    conversationScopeKey,
    markPendingUserMessageDispatched,
    observeInputOutcome,
    pinToBottomIfNeeded,
    serverId,
    turnBusy,
    unlockSend,
  ]);

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
    setOperationalError(undefined);
    try {
      wsClient.sendAction(serverId, agentId, "pause", {
        conversationScopeKey,
        turnId: workingTurn?.id,
        turnStartedAt: workingTurn?.started_at,
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
        setOperationalError(
          error?.message || "Stop was not dispatched. Try again.",
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
    operationalError,
    submitTextToCodex,
    startNewCodexChat,
    sendSlashCommandToCodex,
    interruptCodex,
    retryPendingUserMessage,
  };
}
