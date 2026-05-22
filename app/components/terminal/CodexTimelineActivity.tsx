import React, { useContext, useEffect, useState } from "react";
import {
  ActivityIndicator,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  TimelineTextSelectableContext,
} from "./CodexMessageBody";
import { CodexTimelineActivityDetails } from "./CodexTimelineActivityDetails";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

export type PatchOperation = "add" | "delete" | "update";

export type PatchFileSummary = {
  path: string;
  movePath?: string;
  operation: PatchOperation;
  added: number;
  removed: number;
};

export interface ZenActivityTimelineItem {
  type: "activity";
  id: string;
  timestamp?: string;
  title: string;
  tone: "neutral" | "running" | "success" | "failed";
  icon: IoniconName;
  detail?: string;
  body?: string;
  files?: string[];
  fileSummaries?: PatchFileSummary[];
  previewPath?: string;
}

interface ZenActivityEventProps {
  item: ZenActivityTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  loadAssetPreview(path: string): Promise<string | null>;
  formatPatchPath(file: PatchFileSummary): string;
  truncateBody(value: string, limit: number): string;
}

export function ZenActivityEvent({
  item,
  chrome,
  theme,
  loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: ZenActivityEventProps) {
  const [expanded, setExpanded] = useState(() => shouldAutoExpandActivity(item));
  const [assetPreviewUri, setAssetPreviewUri] = useState<string | null>(null);
  const [assetPreviewFailed, setAssetPreviewFailed] = useState(false);
  const textSelectable = useContext(TimelineTextSelectableContext);
  const toneColor =
    item.tone === "failed"
      ? theme.red
      : item.tone === "running"
        ? theme.yellow
        : item.tone === "success"
          ? theme.green
          : chrome.textSubtle;

  useEffect(() => {
    let cancelled = false;
    setAssetPreviewUri(null);
    setAssetPreviewFailed(false);
    if (!item.previewPath) {
      return () => {
        cancelled = true;
      };
    }
    void loadAssetPreview(item.previewPath)
      .then((uri) => {
        if (!cancelled && uri) {
          setAssetPreviewUri(uri);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setAssetPreviewFailed(true);
        }
      });
    return () => {
      cancelled = true;
    };
  }, [item.previewPath, loadAssetPreview]);

  const canExpand = Boolean(
    item.body || item.fileSummaries?.length || item.files?.length || item.previewPath,
  );

  return (
    <View style={styles.wrap}>
      <TouchableOpacity
        accessibilityLabel={item.title}
        style={styles.row}
        onPress={() => {
          if (canExpand) {
            setExpanded((value) => !value);
          }
        }}
        disabled={!canExpand}
        activeOpacity={0.76}
      >
        {item.tone === "running" ? (
          <ActivityIndicator size="small" color={toneColor} />
        ) : (
          <Ionicons name={item.icon} size={13} color={toneColor} />
        )}
        <Text style={[styles.title, { color: chrome.textSubtle }]} numberOfLines={1}>
          {item.title}
        </Text>
        {item.detail ? (
          <Text style={[styles.detail, { color: chrome.textSubtle }]} numberOfLines={1}>
            {item.detail}
          </Text>
        ) : null}
        {canExpand ? (
          <Ionicons
            name={expanded ? "chevron-up" : "chevron-down"}
            size={12}
            color={chrome.textSubtle}
          />
        ) : null}
      </TouchableOpacity>

      {expanded ? (
        <CodexTimelineActivityDetails
          item={item}
          chrome={chrome}
          theme={theme}
          assetPreviewUri={assetPreviewUri}
          assetPreviewFailed={assetPreviewFailed}
          textSelectable={textSelectable}
          formatPatchPath={formatPatchPath}
          truncateBody={truncateBody}
        />
      ) : null}
    </View>
  );
}

function shouldAutoExpandActivity(item: ZenActivityTimelineItem) {
  if (
    item.tone === "running" ||
    item.tone === "failed" ||
    item.previewPath ||
    item.fileSummaries?.length ||
    item.files?.length
  ) {
    return true;
  }
  if (!item.body) {
    return false;
  }
  return item.body.length <= 700 && item.body.split("\n").length <= 10;
}

const styles = StyleSheet.create({
  wrap: {
    marginBottom: 10,
    paddingLeft: 1,
  },
  row: {
    alignSelf: "flex-start",
    minHeight: 24,
    maxWidth: "100%",
    flexDirection: "row",
    alignItems: "center",
    gap: 6,
    opacity: 0.78,
  },
  title: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFontMedium,
  },
  detail: {
    flexShrink: 1,
    maxWidth: 210,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
});
