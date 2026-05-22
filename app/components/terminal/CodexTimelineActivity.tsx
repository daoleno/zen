import React, { useContext, useEffect, useState } from "react";
import {
  StyleSheet,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  TimelineTextSelectableContext,
} from "./TimelineTextSelectableContext";
import { CodexTimelineActivityDetails } from "./CodexTimelineActivityDetails";
import {
  CodexTimelineActivityHeader,
} from "./CodexTimelineActivityHeader";
import type {
  PatchFileSummary,
  ZenActivityTimelineItem,
} from "./CodexTimelineActivityTypes";

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
      <CodexTimelineActivityHeader
        title={item.title}
        tone={item.tone}
        icon={item.icon}
        detail={item.detail}
        canExpand={canExpand}
        expanded={expanded}
        toneColor={toneColor}
        chrome={chrome}
        onPress={() => {
          if (canExpand) {
            setExpanded((value) => !value);
          }
        }}
      />

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
});
