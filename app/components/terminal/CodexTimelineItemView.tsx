import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  ZenActivityEvent,
} from "./CodexTimelineActivity";
import type {
  PatchFileSummary,
  ZenActivityTimelineItem,
} from "./CodexTimelineActivityTypes";
import {
  ZenPlanUpdate,
} from "./CodexTimelinePlan";
import type { ZenPlanTimelineItem } from "./CodexTimelinePlanTypes";
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
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

function ZenTimelineItemViewImpl({
  item,
  chrome,
  theme,
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

export const ZenTimelineItemView = React.memo(
  ZenTimelineItemViewImpl,
  areZenTimelineItemViewPropsEqual,
);

function areZenTimelineItemViewPropsEqual(
  previous: ZenTimelineItemViewProps,
  next: ZenTimelineItemViewProps,
) {
  return (
    previous.item === next.item &&
    previous.chrome === next.chrome &&
    previous.theme === next.theme &&
    previous.loadAssetPreview === next.loadAssetPreview &&
    previous.formatPatchPath === next.formatPatchPath &&
    previous.truncateBody === next.truncateBody
  );
}
