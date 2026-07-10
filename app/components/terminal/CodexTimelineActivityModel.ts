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
  return item.tone === "failed";
}

function canExpandActivity(item: ZenActivityTimelineItem) {
  return Boolean(
    item.body
      || item.fileSummaries?.length
      || item.files?.length
      || item.previewPath
      || item.children?.length
      || item.providerToolId,
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
