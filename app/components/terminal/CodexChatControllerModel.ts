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
    return "Thinking";
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
  if (typeof conversation?.active === "boolean") {
    return conversation.active;
  }
  if (conversation) {
    return events.some(isEventRunning);
  }
  return events.some(isEventRunning);
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
