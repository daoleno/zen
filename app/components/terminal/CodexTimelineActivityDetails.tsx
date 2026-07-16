import React, { useState } from "react";
import { StyleSheet, Text, TouchableOpacity, View } from "react-native";
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
  const showStatus = Boolean(item.statusLine?.trim());
  const showQuery = Boolean(item.queryText?.trim());
  const showCommand = Boolean(item.commandText?.trim());
  const showBody = isMeaningfulActivityBody(item.body, item.tone);
  const showPreview = Boolean(item.previewPath);
  const showSteps = Boolean(item.children?.length);
  const showFiles = Boolean(
    item.fileSummaries?.length || item.files?.length,
  );
  const developer = item.developerDetails;
  const showDeveloper = Boolean(
    developer?.providerToolId?.trim()
      || developer?.rawInput?.trim()
      || (developer?.transport && Object.keys(developer.transport).length > 0),
  );

  return (
    <CodexTimelineExpandedBlock chrome={chrome}>
      {showStatus ? (
        <Text style={[styles.statusLine, { color: chrome.text }]} numberOfLines={2}>
          {item.statusLine}
        </Text>
      ) : null}

      {showQuery ? (
        <DetailBlock label="Query" chrome={chrome}>
          <Text
            style={[styles.mono, { color: chrome.textMuted }]}
            numberOfLines={3}
            selectable
          >
            {item.queryText}
          </Text>
        </DetailBlock>
      ) : null}

      {showCommand ? (
        <DetailBlock label="Command" chrome={chrome}>
          <Text
            style={[styles.mono, { color: chrome.textMuted }]}
            numberOfLines={4}
            selectable
          >
            {item.commandText}
          </Text>
        </DetailBlock>
      ) : null}

      {showBody ? (
        <CodexTimelineActivityBody
          body={item.body!}
          chrome={chrome}
          theme={theme}
          activityKind={item.activityKind}
          bodyKind={item.bodyKind}
          tone={item.tone}
          streaming={item.streaming}
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

      {showSteps ? (
        <DetailBlock label="Steps" chrome={chrome}>
          <View style={styles.steps}>
            {item.children!.map((child) => (
              <StepRow key={child.id} child={child} chrome={chrome} theme={theme} />
            ))}
          </View>
        </DetailBlock>
      ) : null}

      {showFiles ? (
        <DetailBlock
          label={item.files && item.files.length > 1 ? `Files · ${item.files.length}` : "Files"}
          chrome={chrome}
        >
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
        </DetailBlock>
      ) : null}

      {showDeveloper ? (
        <DeveloperDetails details={developer!} chrome={chrome} />
      ) : null}
    </CodexTimelineExpandedBlock>
  );
}

function DetailBlock({
  label,
  chrome,
  children,
}: {
  label: string;
  chrome: TerminalThemeChrome;
  children: React.ReactNode;
}) {
  return (
    <View style={styles.section}>
      <Text style={[styles.sectionLabel, { color: chrome.textSubtle }]}>
        {label}
      </Text>
      {children}
    </View>
  );
}

function DeveloperDetails({
  details,
  chrome,
}: {
  details: NonNullable<ZenActivityTimelineItem["developerDetails"]>;
  chrome: TerminalThemeChrome;
}) {
  const [open, setOpen] = useState(false);
  const transportEntries = Object.entries(details.transport || {});

  return (
    <View style={styles.developer}>
      <TouchableOpacity
        accessibilityRole="button"
        accessibilityState={{ expanded: open }}
        accessibilityLabel={open ? "Hide developer details" : "Show developer details"}
        onPress={() => setOpen((value) => !value)}
        activeOpacity={0.75}
        hitSlop={{ top: 6, bottom: 6, left: 4, right: 4 }}
        style={styles.developerToggle}
      >
        <Text style={[styles.developerToggleText, { color: chrome.textSubtle }]}>
          {open ? "Hide developer details" : "Developer details"}
        </Text>
      </TouchableOpacity>
      {open ? (
        <View style={styles.developerBody}>
          {details.providerToolId ? (
            <Text style={[styles.mono, { color: chrome.textMuted }]} selectable>
              {details.providerToolId}
            </Text>
          ) : null}
          {transportEntries.map(([key, value]) => (
            <Text
              key={key}
              style={[styles.mono, { color: chrome.textMuted }]}
              selectable
            >
              {key}: {value}
            </Text>
          ))}
          {details.rawInput ? (
            <Text
              style={[styles.mono, { color: chrome.textMuted }]}
              numberOfLines={8}
              selectable
            >
              {details.rawInput}
            </Text>
          ) : null}
        </View>
      ) : null}
    </View>
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
  statusLine: {
    ...TypeScale.caption,
    fontFamily: Typography.uiFontMedium,
    lineHeight: 18,
  },
  mono: {
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
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
  developer: {
    gap: 4,
    paddingTop: 2,
  },
  developerToggle: {
    alignSelf: "flex-start",
    minHeight: 22,
    justifyContent: "center",
  },
  developerToggleText: {
    ...TypeScale.micro,
    letterSpacing: 0.2,
  },
  developerBody: {
    gap: 4,
  },
});
