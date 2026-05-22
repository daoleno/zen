import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { CodexTimelineEmptyState } from "./CodexTimelineEmptyState";
import {
  ZenTimelineItemView,
  type ZenTimelineItem,
} from "./CodexTimelineItemView";
import type { PatchFileSummary } from "./CodexTimelineActivityTypes";

interface CodexTimelineContentProps {
  items: ZenTimelineItem[];
  loading: boolean;
  error?: string | null;
  unavailable: boolean | null;
  unavailableReason?: string;
  streamingAssistantId: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onUnavailableAction(): void;
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

export function CodexTimelineContent({
  items,
  loading,
  error,
  unavailable,
  unavailableReason,
  streamingAssistantId,
  chrome,
  theme,
  onUnavailableAction,
  loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: CodexTimelineContentProps) {
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

  return (
    <>
      {items.map((item) => (
        <ZenTimelineItemView
          key={item.id}
          item={item}
          chrome={chrome}
          theme={theme}
          stream={
            item.type === "message" &&
            item.role === "assistant" &&
            item.id === streamingAssistantId
          }
          loadAssetPreview={loadAssetPreview}
          formatPatchPath={formatPatchPath}
          truncateBody={truncateBody}
        />
      ))}
    </>
  );
}
