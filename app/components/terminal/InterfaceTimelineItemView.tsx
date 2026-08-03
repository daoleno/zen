import React from "react";
import {
  BrainWorkEventCard,
  type BrainWorkEventTimelineItem,
} from "../brain/BrainWorkEventCard";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { ZenActivityEvent } from "./InterfaceTimelineActivity";
import type {
  PatchFileSummary,
  ZenActivityTimelineItem,
} from "./InterfaceTimelineActivityTypes";
import { ZenPlanUpdate } from "./InterfaceTimelinePlan";
import type { ZenPlanTimelineItem } from "./InterfaceTimelinePlanTypes";
import type { MessagePresentation } from "./InterfaceTimelineGrouping";
import {
  ZenAssistantMessage,
  ZenUserMessage,
  type ZenMessageTimelineItem,
} from "./InterfaceTimelineMessage";

export type ZenTimelineItem =
  | (ZenMessageTimelineItem & { role: "user" })
  | (ZenMessageTimelineItem & { role: "assistant" })
  | ZenActivityTimelineItem
  | ZenPlanTimelineItem
  | BrainWorkEventTimelineItem;

interface ZenTimelineItemViewProps {
  item: ZenTimelineItem;
  presentation?: MessagePresentation;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

function ZenTimelineItemViewImpl({
  item,
  presentation,
  chrome,
  theme,
  loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: ZenTimelineItemViewProps) {
  if (item.type === "message") {
    if (item.role === "user") {
      return (
        <ZenUserMessage
          item={item}
          presentation={presentation}
          chrome={chrome}
          theme={theme}
        />
      );
    }
    return (
      <ZenAssistantMessage
        item={item}
        presentation={presentation}
        chrome={chrome}
        theme={theme}
      />
    );
  }
  if (item.type === "plan") {
    return <ZenPlanUpdate item={item} chrome={chrome} theme={theme} />;
  }
  if (item.type === "brain-work-event") {
    return <BrainWorkEventCard item={item} chrome={chrome} />;
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
    previous.presentation === next.presentation &&
    previous.item === next.item &&
    previous.chrome === next.chrome &&
    previous.theme === next.theme &&
    previous.loadAssetPreview === next.loadAssetPreview &&
    previous.formatPatchPath === next.formatPatchPath &&
    previous.truncateBody === next.truncateBody
  );
}
