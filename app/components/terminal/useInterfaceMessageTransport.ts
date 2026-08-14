import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type SetStateAction,
} from "react";
import { Keyboard } from "react-native";
import type { ConnectionState } from "../../store/agents";
import type { ProviderActivity } from "../../services/codexConversation";
import type { CodexSlashCommand } from "../../services/websocket";
import { wsClient } from "../../services/websocket";
import type {
  ComposerAttachment,
  PendingUserMessage,
  PendingUserMessageAttempt,
  PendingUserMessageInput,
  PendingUserMessageRejection,
} from "./InterfaceChatSession";
import { beginLiveMessageAttempt } from "./messageSendRecovery";
import { staleReceiptAutoRetryPolicy } from "./pendingUserMessageLifecycle";
import { submitProviderCommandAsUserInput } from "./providerCommandSubmission";
import {
  beginComposerStop,
  reconcileComposerStopLatch,
  releaseComposerStopLatch,
} from "./composerStopLatch";

interface UseInterfaceMessageTransportInput {
  serverId: string;
  agentId: string;
  conversationScopeKey?: string;
  connectionState: ConnectionState;
  runningActivity?: ProviderActivity;
  draft: string;
  attachments: ComposerAttachment[];
  pendingUserMessages: PendingUserMessage[];
  setDraft(value: string): void;
  restoreDraft(value: string): void;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  clearComposerNativeText(): void;
  addPendingUserMessage(message: PendingUserMessageInput): string;
  beginPendingUserMessageAttempt(
    id: string,
    attempt: PendingUserMessageAttempt,
  ): void;
  rejectPendingUserMessage(
    id: string,
    rejection: PendingUserMessageRejection,
  ): void;
  requestTurnFocus(pendingMessageId: string): void;
}

export function useInterfaceMessageTransport({
  serverId,
  agentId,
  conversationScopeKey,
  connectionState,
  runningActivity,
  draft,
  attachments,
  pendingUserMessages,
  setDraft,
  restoreDraft,
  setAttachments,
  clearComposerNativeText,
  addPendingUserMessage,
  beginPendingUserMessageAttempt,
  rejectPendingUserMessage,
  requestTurnFocus,
}: UseInterfaceMessageTransportInput) {
  const [sending, setSending] = useState(false);
  const [interrupting, setInterrupting] = useState(false);
  const [operationalError, setOperationalError] = useState<string>();
  const sendLockedRef = useRef(false);
  const interruptActivityIdRef = useRef<string | undefined>(undefined);
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
      interruptActivityIdRef.current = undefined;
    },
    [],
  );

  useEffect(() => {
    const reconciled = reconcileComposerStopLatch(
      interruptActivityIdRef.current,
      runningActivity?.id,
    );
    if (reconciled === interruptActivityIdRef.current) {
      return;
    }
    interruptActivityIdRef.current = reconciled;
    setInterrupting(false);
  }, [runningActivity?.id]);

  useEffect(() => {
    setOperationalError(undefined);
  }, [agentId, conversationScopeKey, serverId]);

  const dispatchPendingUserMessageRef = useRef<
    (
      message: PendingUserMessage,
      options: { reuseIdentity: boolean; staleAutoRetry: boolean },
    ) => boolean
  >(() => false);

  const observeInputOutcome = useCallback(
    (
      pendingMessageId: string,
      receipt: ReturnType<typeof wsClient.sendInput>,
    ) => {
      void receipt.outcome.then((outcome) => {
        if (outcome.kind !== "failed") {
          return;
        }
        const message = pendingUserMessagesRef.current.find(
          (candidate) => candidate.id === pendingMessageId,
        );
        if (
          message &&
          staleReceiptAutoRetryPolicy(message, outcome.failure).autoRetry
        ) {
          // Legacy/stale receipt binding for this logical input: the daemon
          // proved no provider mutation. Invalidate the stale identity and
          // transparently resubmit the same payload once with a fresh
          // identity; the row flag bounds the recovery to one attempt. If the
          // composer is mid-send, surface the failure instead of stranding
          // the row pending.
          if (
            dispatchPendingUserMessageRef.current(message, {
              reuseIdentity: false,
              staleAutoRetry: true,
            })
          ) {
            return;
          }
        }
        rejectPendingUserMessage(pendingMessageId, {
          requestId: outcome.failure.requestId,
          code: outcome.failure.code,
          message: outcome.failure.message,
        });
      });
    },
    [rejectPendingUserMessage],
  );

  const dispatchPendingUserMessage = useCallback(
    (
      message: PendingUserMessage,
      options: { reuseIdentity: boolean; staleAutoRetry: boolean } = {
        reuseIdentity: true,
        staleAutoRetry: false,
      },
    ) => {
      if (sendLockedRef.current) {
        return false;
      }
      sendLockedRef.current = true;
      setSending(true);
      setOperationalError(undefined);
      let receipt: ReturnType<typeof wsClient.sendInput>;
      try {
        receipt = wsClient.sendInput(
          serverId,
          agentId,
          `${message.sentText}\n`,
          {
            displayBody: message.sentText,
            conversationScopeKey,
            // Retries of the exact same logical input reuse its stable
            // identity so the daemon receipt ledger stays idempotent; a
            // stale-receipt recovery mints a fresh identity.
            requestId: options.reuseIdentity
              ? message.dispatchRequestId
              : undefined,
          },
        );
      } catch (error: any) {
        setOperationalError(
          error?.message || "Retry was not dispatched. Try again.",
        );
        unlockSend();
        return false;
      }
      beginPendingUserMessageAttempt(message.id, {
        requestId: receipt.requestId,
        ...(options.staleAutoRetry
          ? { staleReceiptAutoRetried: true }
          : {}),
      });
      unlockSend();
      observeInputOutcome(message.id, receipt);
      return true;
    },
    [
      agentId,
      beginPendingUserMessageAttempt,
      conversationScopeKey,
      observeInputOutcome,
      serverId,
      unlockSend,
    ],
  );

  useEffect(() => {
    dispatchPendingUserMessageRef.current = dispatchPendingUserMessage;
  }, [dispatchPendingUserMessage]);

  const submitTextToInterface = useCallback(
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
      const attempt = beginLiveMessageAttempt({
        writeNow: () =>
          wsClient.sendInput(serverId, agentId, `${text}\n`, {
            displayBody: text,
            conversationScopeKey,
          }),
        createOptimisticRow: (receipt) => {
          return addPendingUserMessage({
            body: previousDraft.trim(),
            sentText: text,
            attachments: previousAttachments.map((attachment) => ({
              name: attachment.name,
              path: attachment.path,
              localUri: attachment.localUri,
              mimeType: attachment.mimeType,
            })),
            lifecycle: "pending",
            dispatchRequestId: receipt.requestId,
          });
        },
        previousDraft,
        currentDraft: currentDraftRef.current,
        previousAttachments,
        currentAttachments: currentAttachmentsRef.current,
      });
      if (attempt.kind === "write_failed") {
        currentDraftRef.current = attempt.restoredDraft;
        currentAttachmentsRef.current = attempt.restoredAttachments;
        restoreDraft(attempt.restoredDraft);
        setAttachments(attempt.restoredAttachments);
        setOperationalError(
          attempt.error instanceof Error && attempt.error.message
            ? attempt.error.message
            : "Message was not dispatched. Send to retry.",
        );
        unlockSend();
        return;
      }
      const { pendingMessageId, receipt } = attempt;
      requestTurnFocus(pendingMessageId);
      setDraft("");
      clearComposerNativeText();
      setAttachments([]);
      currentDraftRef.current = "";
      currentAttachmentsRef.current = [];
      unlockSend();
      observeInputOutcome(pendingMessageId, receipt);
    },
    [
      agentId,
      addPendingUserMessage,
      clearComposerNativeText,
      conversationScopeKey,
      observeInputOutcome,
      restoreDraft,
      requestTurnFocus,
      serverId,
      setAttachments,
      setDraft,
      unlockSend,
    ],
  );

  const retryPendingUserMessage = useCallback(
    (pendingMessageId: string) => {
      if (sendLockedRef.current) {
        return;
      }
      const message = pendingUserMessages.find(
        (candidate) =>
          candidate.id === pendingMessageId && candidate.lifecycle === "failed",
      );
      if (!message) {
        return;
      }
      // Retry is the exact same logical input: reuse its stable identity so
      // the daemon receipt ledger stays idempotent (an accepted retry is
      // acknowledged without replay; a proven non-mutation is re-armed and
      // submitted exactly once more).
      dispatchPendingUserMessage(message, {
        reuseIdentity: true,
        staleAutoRetry: false,
      });
    },
    [dispatchPendingUserMessage, pendingUserMessages],
  );

  const startNewInterfaceChat = useCallback(
    (
      commandText: string = "/new",
      previousDraft: string = commandText,
      previousAttachments: ComposerAttachment[] = [],
    ) => {
      const submittedText = commandText.trim() || "/new";
      if (!runningActivity) {
        Keyboard.dismiss();
      }
      submitProviderCommandAsUserInput(
        submittedText,
        previousDraft,
        previousAttachments,
        submitTextToInterface,
      );
    },
    [submitTextToInterface, runningActivity],
  );

  const sendSlashCommandToInterface = useCallback(
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
        submitTextToInterface,
      );
    },
    [submitTextToInterface],
  );

  const interruptInterface = useCallback(() => {
    if (connectionState !== "connected") {
      return;
    }
    const latch = beginComposerStop(
      interruptActivityIdRef.current,
      runningActivity?.id,
    );
    if (!latch.accepted) {
      return;
    }
    interruptActivityIdRef.current = latch.latchedActivityId;
    const claimedActivityId = latch.latchedActivityId;
    setInterrupting(true);
    setOperationalError(undefined);
    const releaseFailedStop = (message: string) => {
      const previousLatch = interruptActivityIdRef.current;
      const nextLatch = releaseComposerStopLatch(
        previousLatch,
        claimedActivityId,
      );
      interruptActivityIdRef.current = nextLatch;
      if (nextLatch !== previousLatch) {
        setInterrupting(false);
        setOperationalError(message);
      }
    };
    try {
      const receipt = wsClient.sendAction(serverId, agentId, "pause");
      void receipt.outcome.then((outcome) => {
        if (outcome.kind === "failed") {
          releaseFailedStop(outcome.failure.message);
        } else if (outcome.kind === "connection_closed") {
          releaseFailedStop(
            "Stop delivery is uncertain. Check provider state before retrying.",
          );
        }
      });
    } catch (error: any) {
      releaseFailedStop(
        error?.message || "Stop was not dispatched. Try again.",
      );
    }
  }, [agentId, connectionState, serverId, runningActivity?.id]);

  return {
    sending,
    interrupting,
    operationalError,
    submitTextToInterface,
    startNewInterfaceChat,
    sendSlashCommandToInterface,
    interruptInterface,
    retryPendingUserMessage,
  };
}
