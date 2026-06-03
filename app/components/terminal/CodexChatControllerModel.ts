import type { ConnectionState } from "../../store/agents";
import type {
  CodexConversation,
  CodexConversationEvent,
} from "../../services/codexConversation";
import type { ConnectionIssue } from "../../services/connectionIssue";
import type { ComposerAttachment } from "./CodexChatSession";
import { isEventRunning } from "./CodexTimelineModel";

export function buildCodexStatusMeta({
  connectionState,
  connectionIssue,
  conversation,
  events,
  sending,
}: {
  connectionState: ConnectionState;
  connectionIssue?: ConnectionIssue | null;
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
  sending: boolean;
}) {
  if (connectionIssue) {
    return connectionIssue.title;
  }
  if (connectionState === "connecting") {
    return "Reconnecting";
  }
  if (connectionState !== "connected") {
    return "Offline";
  }
  if (
    sending ||
    isCodexRequestRunning({
      conversation,
      events,
    })
  ) {
    return "Working";
  }
  if (conversation?.updated_at) {
    return `Updated ${formatTime(conversation.updated_at)}`;
  }
  return "Live";
}

export function isCodexRequestRunning({
  conversation,
  events,
}: {
  conversation: CodexConversation | null;
  events: CodexConversationEvent[];
}) {
  const latestAssistantResponse = latestAssistantResponseForLatestUserTurn(events);
  if (typeof conversation?.active === "boolean") {
    if (!conversation.active) {
      return false;
    }
    if (!latestAssistantResponse) {
      return true;
    }
    return events.some((event, index) =>
      isEventRunning(event) &&
      isConversationEventPositionAfter(event, index, latestAssistantResponse),
    );
  }
  if (conversation) {
    return events.some(isEventRunning);
  }
  return events.some(isEventRunning);
}

function latestAssistantResponseForLatestUserTurn(
  events: CodexConversationEvent[],
) {
  const latestUser = latestConversationEventPosition(events, "user_message");
  if (!latestUser) {
    return null;
  }
  let latestAssistant:
    | {
        timestamp: number;
        index: number;
      }
    | null = null;
  events.forEach((event, index) => {
    if (
      event.kind !== "assistant_message" ||
      !isConversationEventPositionAfter(event, index, latestUser)
    ) {
      return;
    }
    if (
      !latestAssistant ||
      isConversationEventPositionAfter(event, index, latestAssistant)
    ) {
      latestAssistant = conversationEventPosition(event, index);
    }
  });
  return latestAssistant;
}

function latestConversationEventPosition(
  events: CodexConversationEvent[],
  kind: CodexConversationEvent["kind"],
) {
  let latest:
    | {
        timestamp: number;
        index: number;
      }
    | null = null;
  events.forEach((event, index) => {
    if (event.kind !== kind) {
      return;
    }
    if (!latest || isConversationEventPositionAfter(event, index, latest)) {
      latest = conversationEventPosition(event, index);
    }
  });
  return latest;
}

function conversationEventPosition(
  event: CodexConversationEvent,
  index: number,
) {
  const timestamp = new Date(event.timestamp || "").getTime();
  return {
    timestamp: Number.isFinite(timestamp) ? timestamp : Number.NaN,
    index,
  };
}

function isConversationEventPositionAfter(
  event: CodexConversationEvent,
  index: number,
  anchor: { timestamp: number; index: number },
) {
  const timestamp = new Date(event.timestamp || "").getTime();
  if (Number.isFinite(timestamp) && Number.isFinite(anchor.timestamp)) {
    return timestamp > anchor.timestamp ||
      (timestamp === anchor.timestamp && index > anchor.index);
  }
  return index > anchor.index;
}

export function buildCodexComposerMessage(
  draft: string,
  attachments: ComposerAttachment[],
) {
  const body = draft.trim();
  if (attachments.length === 0) {
    return body;
  }
  const attachmentBlock = `<zen_attachments>${JSON.stringify({
    files: attachments.map((attachment) => ({
      name: attachment.name,
      path: attachment.path,
    })),
  })}</zen_attachments>`;
  return [body, attachmentBlock].filter(Boolean).join("\n\n");
}

export function conversationUnavailableReason(reason?: string) {
  switch (reason) {
    case "not_codex":
      return "Chat is only available for supported agent sessions.";
    case "not_visible":
      return "This chat is not available from the current view yet.";
    case "missing_cwd":
      return "This chat is still getting its workspace ready.";
    case "transcript_not_found":
      return "Messages are still syncing for this session.";
    case "agent_not_found":
      return "This agent session is no longer available.";
    case "session_not_ready":
      return "This chat is getting ready.";
    default:
      return "Open the terminal view for this session.";
  }
}

export function isConversationSyncingReason(reason?: string) {
  return (
    reason === "session_not_ready" ||
    reason === "transcript_not_found" ||
    reason === "missing_cwd" ||
    reason === "not_visible"
  );
}

function formatTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
}
