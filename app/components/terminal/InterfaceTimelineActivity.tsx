import React, { useEffect, useState } from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { InterfaceTimelineActivityDetails } from "./InterfaceTimelineActivityDetails";
import { InterfaceTimelineActivityHeader } from "./InterfaceTimelineActivityHeader";
import { useTimelineActivityExpansion } from "./InterfaceTimelineActivityExpansionState";
import {
  buildInterfaceTimelineActivityPresentation,
  shouldAutoExpandActivity,
} from "./InterfaceTimelineActivityModel";
import type {
  PatchFileSummary,
  ZenActivityTimelineItem,
} from "./InterfaceTimelineActivityTypes";

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
  const { detailsExpanded, expanded, toggle } = useTimelineActivityExpansion(
    item.id,
    defaultExpanded,
  );
  const [assetPreviewUri, setAssetPreviewUri] = useState<string | null>(null);
  const [assetPreviewFailed, setAssetPreviewFailed] = useState(false);
  const activityPresentation = buildInterfaceTimelineActivityPresentation(
    item,
    chrome,
    theme,
  );

  useEffect(() => {
    let cancelled = false;
    setAssetPreviewUri(null);
    setAssetPreviewFailed(false);
    if (!item.previewPath || !detailsExpanded) {
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
  }, [detailsExpanded, item.previewPath, loadAssetPreview]);

  return (
    <View style={styles.wrap}>
      <InterfaceTimelineActivityHeader
        title={item.title}
        tone={item.tone}
        icon={item.icon}
        activityKind={item.activityKind}
        detail={item.detail}
        canExpand={activityPresentation.canExpand}
        expanded={expanded}
        toneColor={activityPresentation.toneColor}
        chrome={chrome}
        accessibilityLabel={item.accessibilityLabel}
        onPress={() => {
          if (activityPresentation.canExpand) {
            toggle();
          }
        }}
      />

      {detailsExpanded ? (
        <InterfaceTimelineActivityDetails
          item={item}
          chrome={chrome}
          theme={theme}
          assetPreviewUri={assetPreviewUri}
          assetPreviewFailed={assetPreviewFailed}
          formatPatchPath={formatPatchPath}
          truncateBody={truncateBody}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  wrap: {
    marginBottom: 2,
    paddingLeft: 1,
  },
});
