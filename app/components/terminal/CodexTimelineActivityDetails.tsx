import React from "react";
import {
  StyleSheet,
  Text,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import {
  ActivityFileList,
  ActivityPreview,
  PatchFileSummaryList,
} from "./CodexTimelineActivityArtifacts";
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
  textSelectable: boolean;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

export function CodexTimelineActivityDetails({
  item,
  chrome,
  theme,
  assetPreviewUri,
  assetPreviewFailed,
  textSelectable,
  formatPatchPath,
  truncateBody,
}: CodexTimelineActivityDetailsProps) {
  return (
    <View style={[styles.expanded, { borderColor: chrome.border }]}>
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
        <Text
          selectable={textSelectable}
          style={[styles.body, { color: chrome.textSubtle }]}
        >
          {truncateBody(item.body, 1800)}
        </Text>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  expanded: {
    marginTop: 6,
    marginLeft: 19,
    maxWidth: "92%",
    borderLeftWidth: StyleSheet.hairlineWidth,
    paddingLeft: 10,
    paddingVertical: 4,
  },
  body: {
    marginTop: 6,
    fontSize: 11,
    lineHeight: 16,
    fontFamily: Typography.terminalFont,
  },
});
