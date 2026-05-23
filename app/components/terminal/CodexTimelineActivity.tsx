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
import {
  buildCodexTimelineActivityPresentation,
  shouldAutoExpandActivity,
} from "./CodexTimelineActivityModel";
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
  const defaultExpanded = shouldAutoExpandActivity(item);
  const [expanded, setExpanded] = useState(defaultExpanded);
  const [assetPreviewUri, setAssetPreviewUri] = useState<string | null>(null);
  const [assetPreviewFailed, setAssetPreviewFailed] = useState(false);
  const textSelectable = useContext(TimelineTextSelectableContext);
  const activityPresentation = buildCodexTimelineActivityPresentation(
    item,
    chrome,
    theme,
  );

  useEffect(() => {
    setExpanded(defaultExpanded);
  }, [defaultExpanded, item.id, item.statusKey]);

  useEffect(() => {
    let cancelled = false;
    setAssetPreviewUri(null);
    setAssetPreviewFailed(false);
    if (!item.previewPath || !expanded) {
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
  }, [expanded, item.previewPath, loadAssetPreview]);

  return (
    <View style={styles.wrap}>
      <CodexTimelineActivityHeader
        title={item.title}
        tone={item.tone}
        icon={item.icon}
        detail={item.detail}
        canExpand={activityPresentation.canExpand}
        expanded={expanded}
        toneColor={activityPresentation.toneColor}
        chrome={chrome}
        onPress={() => {
          if (activityPresentation.canExpand) {
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

const styles = StyleSheet.create({
  wrap: {
    marginBottom: 7,
    paddingLeft: 1,
  },
});
