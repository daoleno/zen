import React, { useContext, useEffect, useState } from "react";
import {
  ActivityIndicator,
  Image,
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
import type { CodexPlanStep } from "../../services/codexConversation";
import {
  TimelineTextSelectableContext,
} from "./CodexMessageBody";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

export type PatchOperation = "add" | "delete" | "update";

export type PatchFileSummary = {
  path: string;
  movePath?: string;
  operation: PatchOperation;
  added: number;
  removed: number;
};

export interface ZenPlanTimelineItem {
  type: "plan";
  id: string;
  timestamp?: string;
  explanation?: string;
  steps: CodexPlanStep[];
}

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

export function ZenPlanUpdate({
  item,
  chrome,
  theme,
}: {
  item: ZenPlanTimelineItem;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  return (
    <View style={styles.wrap}>
      <View style={styles.row}>
        <Ionicons name="checkbox-outline" size={13} color={theme.cyan} />
        <Text style={[styles.title, { color: chrome.textSubtle }]} numberOfLines={1}>
          Updated Plan
        </Text>
      </View>
      <View style={[styles.expanded, styles.planBlock, { borderColor: chrome.border }]}>
        {item.explanation?.trim() ? (
          <Text style={[styles.planExplanation, { color: chrome.textSubtle }]}>
            {item.explanation.trim()}
          </Text>
        ) : null}
        {item.steps.length > 0 ? (
          <View style={styles.planSteps}>
            {item.steps.map((step, index) => (
              <ZenPlanStepRow
                key={`${index}:${step.step}`}
                step={step}
                chrome={chrome}
                theme={theme}
              />
            ))}
          </View>
        ) : (
          <Text style={[styles.planEmpty, { color: chrome.textSubtle }]}>
            (no steps provided)
          </Text>
        )}
      </View>
    </View>
  );
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
        <View style={[styles.expanded, { borderColor: chrome.border }]}>
          {item.previewPath ? (
            assetPreviewUri ? (
              <Image
                source={{ uri: assetPreviewUri }}
                style={[styles.image, { borderColor: chrome.border }]}
                resizeMode="cover"
              />
            ) : (
              <View style={[styles.imagePlaceholder, { borderColor: chrome.border }]}>
                {assetPreviewFailed ? (
                  <Ionicons name="image-outline" size={16} color={chrome.textSubtle} />
                ) : (
                  <ActivityIndicator size="small" color={chrome.textSubtle} />
                )}
              </View>
            )
          ) : null}
          {item.fileSummaries?.length ? (
            <View style={styles.diffFiles}>
              {item.fileSummaries.slice(0, 6).map((file) => (
                <View key={`${file.operation}:${file.path}`} style={styles.diffFileRow}>
                  <Text style={[styles.diffPrefix, { color: chrome.textSubtle }]}>
                    {"\u2514"}
                  </Text>
                  <Text
                    style={[styles.diffPath, { color: chrome.textMuted }]}
                    numberOfLines={1}
                  >
                    {formatPatchPath(file)}
                  </Text>
                  <Text style={[styles.diffAdded, { color: theme.green }]}>
                    +{file.added}
                  </Text>
                  <Text style={[styles.diffRemoved, { color: theme.red }]}>
                    -{file.removed}
                  </Text>
                </View>
              ))}
            </View>
          ) : item.files?.length ? (
            <View style={styles.files}>
              {item.files.slice(0, 4).map((file) => (
                <Text
                  key={file}
                  style={[styles.fileText, { color: chrome.textMuted }]}
                  numberOfLines={1}
                >
                  {file}
                </Text>
              ))}
            </View>
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
      ) : null}
    </View>
  );
}

function ZenPlanStepRow({
  step,
  chrome,
  theme,
}: {
  step: CodexPlanStep;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}) {
  const completed = step.status === "completed";
  const inProgress = step.status === "in_progress";
  const marker = completed ? "\u2714" : "\u25a1";
  const color = completed ? chrome.textSubtle : inProgress ? theme.cyan : chrome.textMuted;
  return (
    <View style={styles.planStepRow}>
      <Text style={[styles.planMarker, { color }]}>{marker}</Text>
      <Text
        style={[
          styles.planStepText,
          completed ? styles.planStepCompleted : null,
          inProgress ? styles.planStepActive : null,
          { color },
        ]}
      >
        {step.step}
      </Text>
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
  files: {
    gap: 4,
  },
  fileText: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffFiles: {
    gap: 5,
  },
  diffFileRow: {
    minWidth: 0,
    flexDirection: "row",
    alignItems: "center",
    gap: 5,
  },
  diffPrefix: {
    width: 10,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffPath: {
    flex: 1,
    minWidth: 0,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffAdded: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffRemoved: {
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  image: {
    width: "100%",
    height: 150,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
  },
  imagePlaceholder: {
    height: 96,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  planBlock: {
    paddingVertical: 2,
  },
  planExplanation: {
    marginBottom: 7,
    fontSize: 12,
    lineHeight: 17,
    fontStyle: "italic",
    fontFamily: Typography.uiFont,
  },
  planSteps: {
    gap: 6,
  },
  planStepRow: {
    minWidth: 0,
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 7,
  },
  planMarker: {
    width: 14,
    fontSize: 13,
    lineHeight: 18,
    fontFamily: Typography.uiFontMedium,
  },
  planStepText: {
    flex: 1,
    minWidth: 0,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.uiFont,
  },
  planStepActive: {
    fontFamily: Typography.uiFontMedium,
  },
  planStepCompleted: {
    textDecorationLine: "line-through",
  },
  planEmpty: {
    fontSize: 12,
    lineHeight: 17,
    fontStyle: "italic",
    fontFamily: Typography.uiFont,
  },
});
