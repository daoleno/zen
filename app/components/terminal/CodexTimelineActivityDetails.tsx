import React from "react";
import { StyleSheet, Text, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { TypeScale, Typography } from "../../constants/tokens";
import {
  ActivityFileList,
  ActivityPreview,
  PatchFileSummaryList,
} from "./CodexTimelineActivityArtifacts";
import {
  CodexTimelineActivityBody,
  isMeaningfulActivityBody,
} from "./CodexTimelineActivityBody";
import { CodexTimelineExpandedBlock } from "./CodexTimelineExpandedBlock";
import type {
  PatchFileSummary,
  ZenActivityChild,
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
  const showBody = isMeaningfulActivityBody(item.body, item.tone);
  const showPreview = Boolean(item.previewPath);
  const showSteps = Boolean(item.children?.length);
  const showFiles = Boolean(
    item.fileSummaries?.length || item.files?.length,
  );
  const showTechnical = Boolean(item.providerToolId?.trim());

  return (
    <CodexTimelineExpandedBlock chrome={chrome}>
      {/* 1. Useful result / error first */}
      {showBody ? (
        <CodexTimelineActivityBody
          body={item.body!}
          chrome={chrome}
          theme={theme}
          activityKind={item.activityKind}
          bodyKind={item.bodyKind}
          tone={item.tone}
          truncateBody={truncateBody}
        />
      ) : null}

      {showPreview ? (
        <ActivityPreview
          uri={assetPreviewUri}
          failed={assetPreviewFailed}
          chrome={chrome}
        />
      ) : null}

      {/* 2. Multi-call children as Steps */}
      {showSteps ? (
        <View style={styles.section}>
          <SectionLabel chrome={chrome}>Steps</SectionLabel>
          <View style={styles.steps}>
            {item.children!.map((child) => (
              <StepRow key={child.id} child={child} chrome={chrome} theme={theme} />
            ))}
          </View>
        </View>
      ) : null}

      {/* 3. Files / patch summaries */}
      {showFiles ? (
        <View style={styles.section}>
          <SectionLabel chrome={chrome}>Files</SectionLabel>
          {item.fileSummaries?.length ? (
            <PatchFileSummaryList
              files={item.fileSummaries}
              chrome={chrome}
              theme={theme}
              formatPatchPath={formatPatchPath}
            />
          ) : (
            <ActivityFileList files={item.files!} chrome={chrome} />
          )}
        </View>
      ) : null}

      {/* 4. Raw provider tool id last */}
      {showTechnical ? (
        <View style={styles.technical}>
          <SectionLabel chrome={chrome}>Technical</SectionLabel>
          <Text
            style={[styles.providerId, { color: chrome.textMuted }]}
            numberOfLines={2}
            selectable
          >
            {item.providerToolId}
          </Text>
        </View>
      ) : null}
    </CodexTimelineExpandedBlock>
  );
}

function SectionLabel({
  chrome,
  children,
}: {
  chrome: TerminalThemeChrome;
  children: string;
}) {
  return (
    <Text style={[styles.sectionLabel, { color: chrome.textMuted }]}>
      {children}
    </Text>
  );
}

function StepRow({
  child,
  chrome,
  theme,
}: {
  child: ZenActivityChild;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const tone = child.tone ?? "neutral";
  const markerColor =
    tone === "failed"
      ? theme.red
      : tone === "running"
        ? chrome.accent
        : tone === "success"
          ? theme.green
          : chrome.textSubtle;

  return (
    <View style={styles.stepRow}>
      <View
        accessibilityLabel={`${tone} step`}
        style={[styles.stepMarker, { backgroundColor: markerColor }]}
      />
      <Text
        style={[styles.stepTitle, { color: chrome.text }]}
        numberOfLines={2}
      >
        {child.title}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  section: {
    gap: 5,
  },
  sectionLabel: {
    ...TypeScale.micro,
    textTransform: "uppercase",
    letterSpacing: 0.4,
  },
  steps: {
    gap: 5,
  },
  stepRow: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 8,
    minHeight: 18,
  },
  stepMarker: {
    width: 7,
    height: 7,
    borderRadius: 4,
    marginTop: 5,
  },
  stepTitle: {
    ...TypeScale.caption,
    flex: 1,
    minWidth: 0,
    fontFamily: Typography.uiFontMedium,
    lineHeight: 18,
  },
  technical: {
    gap: 3,
    paddingTop: 2,
  },
  providerId: {
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
});
