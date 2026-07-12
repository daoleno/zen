import type { Ionicons } from "@expo/vector-icons";
import type { ComponentProps } from "react";
import type { ToolDeveloperDetails } from "../../services/toolCallDetails";

export type TimelineActivityIconName =
  ComponentProps<typeof Ionicons>["name"];

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
  statusKey?: string;
  title: string;
  tone: "neutral" | "running" | "success" | "failed";
  icon: TimelineActivityIconName;
  activityKind?: "reasoning";
  detail?: string;
  body?: string;
  bodyKind?: "terminal" | "diff-stat";
  /** Primary command shown in expanded details (not the collapsed title). */
  commandText?: string;
  /** Search / lookup query for expanded details. */
  queryText?: string;
  /** Status summary such as Completed · 7.8s or Exit 1. */
  statusLine?: string;
  files?: string[];
  fileSummaries?: PatchFileSummary[];
  previewPath?: string;
  defaultExpanded?: boolean;
  accessibilityLabel?: string;
  providerToolId?: string;
  developerDetails?: ToolDeveloperDetails;
  children?: ZenActivityChild[];
}

export type ZenActivityChild = {
  id: string;
  title: string;
  tone?: "neutral" | "running" | "success" | "failed";
  providerToolId?: string;
};
