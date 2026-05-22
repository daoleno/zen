import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  ZenActivityEvent,
  ZenPlanUpdate,
  type PatchFileSummary,
  type ZenActivityTimelineItem,
  type ZenPlanTimelineItem,
} from "./CodexTimelineActivity";
import {
  ZenAssistantMessage,
  ZenUserMessage,
  type ZenMessageTimelineItem,
} from "./CodexTimelineMessage";

export type ZenTimelineItem =
  | (ZenMessageTimelineItem & { role: "user" })
  | (ZenMessageTimelineItem & { role: "assistant" })
  | ZenActivityTimelineItem
  | ZenPlanTimelineItem;

interface ZenTimelineItemViewProps {
  item: ZenTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  stream: boolean;
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

export function ZenTimelineItemView({
  item,
  chrome,
  theme,
  stream,
  loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: ZenTimelineItemViewProps) {
  if (item.type === "message") {
    if (item.role === "user") {
      return <ZenUserMessage item={item} chrome={chrome} theme={theme} />;
    }
    return (
      <ZenAssistantMessage
        item={item}
        chrome={chrome}
        theme={theme}
        stream={stream}
      />
    );
  }
  if (item.type === "plan") {
    return <ZenPlanUpdate item={item} chrome={chrome} theme={theme} />;
  }
  return (
    <ZenActivityEvent
      item={item}
      chrome={chrome}
      theme={theme}
      loadAssetPreview={loadAssetPreview}
      formatPatchPath={formatPatchPath}
      truncateBody={truncateBody}
    />
  );
}
