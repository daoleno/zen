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
  chrome: TerminalThemeChrome;
  onUnavailableAction(): void;
}

export function CodexTimelineEmptyContent({
  items,
  loading,
  error,
  unavailable,
  unavailableReason,
  chrome,
  onUnavailableAction,
}: CodexTimelineEmptyContentProps) {
  if (loading && items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Loading Codex transcript"
        busy
      />
    );
  }

  if (error && items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Transcript unavailable"
        body={error}
      />
    );
  }

  if (unavailable) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Native transcript unavailable"
        body={unavailableReason}
        actionLabel="Terminal"
        onAction={onUnavailableAction}
      />
    );
  }

  if (items.length === 0) {
    return (
      <CodexTimelineEmptyState
        chrome={chrome}
        title="Waiting for Codex transcript"
      />
    );
  }

  return null;
}
