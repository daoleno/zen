import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { CodexChatLocalState } from "./CodexChatSession";
import { CodexTimelineEmptyState } from "./CodexTimelineEmptyState";
import type { ZenTimelineItem } from "./CodexTimelineItemView";

interface CodexTimelineEmptyContentProps {
  items: ZenTimelineItem[];
  loading: boolean;
  localChatState: CodexChatLocalState;
  error?: string | null;
  suppressed: boolean;
  unavailable: boolean | null;
  unavailableReason?: string;
  syncing: boolean;
  chrome: TerminalThemeChrome;
  onUnavailableAction(): void;
}

export function CodexTimelineEmptyContent({
  items,
  loading,
  localChatState,
  error,
  suppressed,
  unavailable,
  unavailableReason,
  syncing,
  chrome,
  onUnavailableAction,
}: CodexTimelineEmptyContentProps) {
  if (suppressed && items.length === 0) {
    return null;
  }

  if (localChatState === "starting-new-chat" && items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="New chat"
        body="Starting fresh."
        busy
        variant="session"
      />
    );
  }

  if (localChatState === "new-chat-ready" && items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="New chat"
        body="Ready."
        variant="session"
      />
    );
  }

  if (loading && items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Loading chat"
        body="Syncing messages."
        busy
      />
    );
  }

  if (error && items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Could not load this chat"
        body={error}
      />
    );
  }

  if (syncing && items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Syncing chat"
        body={unavailableReason || "Waiting for the daemon to index the latest Codex transcript."}
        busy
      />
    );
  }

  if (unavailable) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Chat view is not available here"
        body={unavailableReason}
        actionLabel="Show Raw Session"
        onAction={onUnavailableAction}
      />
    );
  }

  if (items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Ready"
        body="Message Codex below."
      />
    );
  }

  return null;
}
