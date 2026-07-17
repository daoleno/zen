import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { CodexSessionIdleView } from "./CodexSessionIdleView";
import { CodexTimelineEmptyState } from "./CodexTimelineEmptyState";
import type { ZenTimelineItem } from "./CodexTimelineItemView";

interface CodexTimelineEmptyContentProps {
  items: ZenTimelineItem[];
  loading: boolean;
  error?: string | null;
  suppressed: boolean;
  unavailable: boolean | null;
  unavailableReason?: string;
  syncing: boolean;
  chrome: TerminalThemeChrome;
  agentCwd?: string;
  onUnavailableAction?: () => void;
  showUnavailableAction?: boolean;
  emptyTitle?: string;
  emptyBody?: string;
}

export function CodexTimelineEmptyContent({
  items,
  loading,
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

  if (loading && items.length === 0) {
    if (emptyTitle) {
      return (
        <CodexTimelineEmptyState
          chrome={chrome}
          title={emptyTitle}
          body={emptyBody}
          busy
        />
      );
    }
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
    if (emptyTitle) {
      return (
        <CodexTimelineEmptyState
          chrome={chrome}
          title={emptyTitle}
          body={emptyBody}
          busy
        />
      );
    }
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
