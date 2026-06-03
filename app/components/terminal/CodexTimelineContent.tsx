import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import type { CodexChatLocalState } from "./CodexChatSession";
import { CodexSessionIdleView } from "./CodexSessionIdleView";
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
  agentCwd?: string;
  onUnavailableAction(): void;
  showUnavailableAction?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
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
  agentCwd,
  onUnavailableAction,
  showUnavailableAction = true,
  emptyTitle,
  emptyBody,
}: CodexTimelineEmptyContentProps) {
  if (suppressed && items.length === 0) {
    return null;
  }

  if (
    items.length === 0 &&
    (localChatState === "starting-new-chat" || localChatState === "new-chat-ready")
  ) {
    return (
      <CodexSessionIdleView
        chrome={chrome}
        cwd={agentCwd}
        busy={localChatState === "starting-new-chat"}
      />
    );
  }

  if (loading && items.length === 0) {
    return <CodexSessionIdleView chrome={chrome} cwd={agentCwd} busy />;
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
    return <CodexSessionIdleView chrome={chrome} cwd={agentCwd} busy />;
  }

  if (unavailable) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Chat view is not available here"
        body={unavailableReason}
        actionLabel={showUnavailableAction ? "Open Terminal" : undefined}
        onAction={showUnavailableAction ? onUnavailableAction : undefined}
      />
    );
  }

  if (items.length === 0) {
    if (!emptyTitle) {
      return <CodexSessionIdleView chrome={chrome} cwd={agentCwd} />;
    }
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title={emptyTitle}
        body={emptyBody}
      />
    );
  }

  return null;
}
