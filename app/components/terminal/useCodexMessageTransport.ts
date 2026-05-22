import {
  useCallback,
  useState,
  type SetStateAction,
} from "react";
import type { ConnectionState } from "../../store/agents";
import { wsClient } from "../../services/websocket";
import type { ComposerAttachment } from "./CodexChatSession";

interface UseCodexMessageTransportInput {
  serverId: string;
  agentId: string;
  connectionState: ConnectionState;
  setDraft(value: string): void;
  setAttachments(value: SetStateAction<ComposerAttachment[]>): void;
  refreshConversation(showLoading?: boolean): Promise<void>;
  scrollToLatest(animated?: boolean, delay?: number): void;
}

export function useCodexMessageTransport({
  serverId,
  agentId,
  connectionState,
  setDraft,
  setAttachments,
  refreshConversation,
  scrollToLatest,
}: UseCodexMessageTransportInput) {
  const [sending, setSending] = useState(false);

  const submitTextToCodex = useCallback(
    (
      text: string,
      previousDraft: string,
      previousAttachments: ComposerAttachment[],
    ) => {
      setSending(true);
      setDraft("");
      setAttachments([]);
      scrollToLatest(true);
      try {
        wsClient.sendInput(serverId, agentId, `${text}\n`);
        setTimeout(() => {
          void refreshConversation(false);
          setSending(false);
        }, 600);
      } catch {
        setDraft(previousDraft);
        setAttachments(previousAttachments);
        setSending(false);
      }
    },
    [
      agentId,
      refreshConversation,
      scrollToLatest,
      serverId,
      setAttachments,
      setDraft,
    ],
  );

  const interruptCodex = useCallback(() => {
    if (connectionState !== "connected" || sending) {
      return;
    }
    setSending(true);
    try {
      wsClient.sendAction(serverId, agentId, "pause");
      setTimeout(() => {
        void refreshConversation(false);
        setSending(false);
      }, 600);
    } catch {
      setSending(false);
    }
  }, [agentId, connectionState, refreshConversation, sending, serverId]);

  return {
    sending,
    submitTextToCodex,
    interruptCodex,
  };
}
