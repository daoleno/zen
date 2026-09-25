import React from "react";
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
  loadAssetPreview: _loadAssetPreview,
  formatPatchPath,
  truncateBody,
}: ZenActivityEventProps) {
  const defaultExpanded = shouldAutoExpandActivity(item);
  const { detailsExpanded, expanded, toggle } = useTimelineActivityExpansion(
    item.id,
    defaultExpanded,
  );
  const activityPresentation = buildInterfaceTimelineActivityPresentation(
    item,
    chrome,
    theme,
  );


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
          assetPreviewUri={null}
          assetPreviewFailed={false}
          formatPatchPath={formatPatchPath}
          truncateBody={truncateBody}
        />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  // Tool rows are margin annotations: a compact, even rhythm between rows
  // and a little extra air where they meet prose (message spacing owns the
  // rest).
  wrap: {
    paddingVertical: 1,
    marginBottom: 3,
    paddingLeft: 1,
  },
});
