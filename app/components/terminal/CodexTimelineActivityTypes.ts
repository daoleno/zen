import type { Ionicons } from "@expo/vector-icons";
import type { ComponentProps } from "react";

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
  title: string;
  tone: "neutral" | "running" | "success" | "failed";
  icon: TimelineActivityIconName;
  detail?: string;
  body?: string;
  files?: string[];
  fileSummaries?: PatchFileSummary[];
  previewPath?: string;
}
