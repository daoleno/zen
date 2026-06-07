import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  ActivityFileList,
  ActivityPreview,
  PatchFileSummaryList,
} from "./CodexTimelineActivityArtifacts";
import { CodexTimelineActivityBody } from "./CodexTimelineActivityBody";
import { CodexTimelineExpandedBlock } from "./CodexTimelineExpandedBlock";
import type {
  PatchFileSummary,
  ZenActivityTimelineItem,
} from "./CodexTimelineActivityTypes";

interface CodexTimelineActivityDetailsProps {
  item: ZenActivityTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  assetPreviewUri: string | null;
  assetPreviewFailed: boolean;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

export function CodexTimelineActivityDetails({
  item,
  chrome,
  theme,
  assetPreviewUri,
  assetPreviewFailed,
  formatPatchPath,
  truncateBody,
}: CodexTimelineActivityDetailsProps) {
  return (
    <CodexTimelineExpandedBlock borderColor={chrome.border}>
      {item.previewPath ? (
        <ActivityPreview
          uri={assetPreviewUri}
          failed={assetPreviewFailed}
          chrome={chrome}
        />
      ) : null}
      {item.fileSummaries?.length ? (
        <PatchFileSummaryList
          files={item.fileSummaries}
          chrome={chrome}
          theme={theme}
          formatPatchPath={formatPatchPath}
        />
      ) : item.files?.length ? (
        <ActivityFileList files={item.files} chrome={chrome} />
      ) : null}
      {item.body ? (
        <CodexTimelineActivityBody
          body={item.body}
          chrome={chrome}
          theme={theme}
          activityKind={item.activityKind}
          bodyKind={item.bodyKind}
          truncateBody={truncateBody}
        />
      ) : null}
    </CodexTimelineExpandedBlock>
  );
}
