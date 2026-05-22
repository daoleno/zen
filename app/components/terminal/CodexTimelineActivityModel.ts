import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { ZenActivityTimelineItem } from "./CodexTimelineActivityTypes";

export interface CodexTimelineActivityPresentation {
  canExpand: boolean;
  toneColor: string;
}

export function buildCodexTimelineActivityPresentation(
  item: ZenActivityTimelineItem,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
): CodexTimelineActivityPresentation {
  return {
    canExpand: canExpandActivity(item),
    toneColor: activityToneColor(item, chrome, theme),
  };
}

export function shouldAutoExpandActivity(item: ZenActivityTimelineItem) {
  if (typeof item.defaultExpanded === "boolean") {
    return item.defaultExpanded;
  }
  if (
    item.tone === "running" ||
    item.tone === "failed" ||
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

function canExpandActivity(item: ZenActivityTimelineItem) {
  return Boolean(
    item.body || item.fileSummaries?.length || item.files?.length || item.previewPath,
  );
}

function activityToneColor(
  item: ZenActivityTimelineItem,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
) {
  if (item.tone === "failed") {
    return theme.red;
  }
  if (item.tone === "running") {
    return theme.yellow;
  }
  if (item.tone === "success") {
    return theme.green;
  }
  return chrome.textSubtle;
}
