import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { mixHex, relativeLuminance } from "../../theme/colorUtils";

function isDistinctBubbleColor(candidate: string, reference: string): boolean {
  const left = relativeLuminance(candidate);
  const right = relativeLuminance(reference);
  return Math.abs(left - right) >= 0.06;
}

export function resolveSentBubbleColor(chrome: TerminalThemeChrome): string {
  if (isDistinctBubbleColor(chrome.surfaceMuted, chrome.appBackground)) {
    return chrome.surfaceMuted;
  }
  return chrome.accent;
}

export function resolveReceivedBubbleColor(chrome: TerminalThemeChrome): string {
  if (isDistinctBubbleColor(chrome.surface, chrome.appBackground)) {
    return chrome.surface;
  }
  return mixHex(chrome.surface, chrome.text, 0.1);
}

export function chromeForSentBubble(
  chrome: TerminalThemeChrome,
  bubbleColor: string,
): TerminalThemeChrome {
  if (relativeLuminance(bubbleColor) >= 0.58) {
    return chrome;
  }
  return {
    ...chrome,
    text: "#FFFFFF",
    textMuted: "rgba(255,255,255,0.88)",
    textSubtle: "rgba(255,255,255,0.68)",
  };
}