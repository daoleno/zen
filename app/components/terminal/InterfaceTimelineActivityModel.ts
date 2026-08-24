import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { ZenActivityTimelineItem } from "./InterfaceTimelineActivityTypes";

export interface InterfaceTimelineActivityPresentation {
  canExpand: boolean;
  toneColor: string;
}

export function buildInterfaceTimelineActivityPresentation(
  item: ZenActivityTimelineItem,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
): InterfaceTimelineActivityPresentation {
  return {
    canExpand: canExpandActivity(item),
    toneColor: activityToneColor(item, chrome, theme),
  };
}

export function shouldAutoExpandActivity(item: ZenActivityTimelineItem) {
  if (typeof item.defaultExpanded === "boolean") {
    return item.defaultExpanded;
  }
  return item.tone === "failed";
}

export function expandedActivityStatusLine(
  item: ZenActivityTimelineItem,
): string | undefined {
  const status = item.statusLine?.trim();
  if (!status || status === item.detail?.trim()) {
    return undefined;
  }
  return item.statusLine;
}

function canExpandActivity(item: ZenActivityTimelineItem) {
  return Boolean(
    item.body ||
    expandedActivityStatusLine(item) ||
    item.commandText ||
    item.queryText ||
    item.fileSummaries?.length ||
    item.files?.length ||
    item.previewPath ||
    item.children?.length,
  );
}

function activityToneColor(
  item: ZenActivityTimelineItem,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
) {
  if (item.activityKind === "reasoning") {
    return chrome.accent;
  }
  if (item.tone === "failed") {
    return theme.red;
  }
  if (item.tone === "running") {
    return chrome.textMuted;
  }
  if (item.tone === "success") {
    return chrome.textSubtle;
  }
  return chrome.textSubtle;
}
