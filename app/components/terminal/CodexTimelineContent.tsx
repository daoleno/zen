import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { CodexTimelineEmptyState } from "./CodexTimelineEmptyState";
import type { ZenTimelineItem } from "./CodexTimelineItemView";

interface CodexTimelineEmptyContentProps {
  items: ZenTimelineItem[];
  loading: boolean;
  error?: string | null;
  unavailable: boolean | null;
  unavailableReason?: string;
  syncing: boolean;
  chrome: TerminalThemeChrome;
  onUnavailableAction(): void;
}

export function CodexTimelineEmptyContent({
  items,
  loading,
  error,
  unavailable,
  unavailableReason,
  syncing,
  chrome,
  onUnavailableAction,
}: CodexTimelineEmptyContentProps) {
  if (loading && items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Loading chat"
        body="Pulling in the latest messages."
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
        actionLabel="Open Terminal"
        onAction={onUnavailableAction}
      />
    );
  }

  if (items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Ready"
        body="Send a message below. Replies and tool activity will appear here."
      />
    );
  }

  return null;
}
