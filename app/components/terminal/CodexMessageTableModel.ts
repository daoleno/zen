import type { TerminalThemeChrome } from "../../constants/terminalThemes";

export type MessageTableRowTone = "section" | "even" | "odd";

export function messageTableRowTone(
  row: string[],
  rowIndex: number,
): MessageTableRowTone {
  const populatedCells = row.filter((cell) => cell.trim().length > 0);
  if (populatedCells.length === 1) {
    return "section";
  }
  return rowIndex % 2 === 0 ? "even" : "odd";
}

export function messageTableSemanticColors(chrome: TerminalThemeChrome) {
  return {
    header: chrome.surfaceMuted,
    section: chrome.accentSoft,
    even: chrome.surface,
    odd: chrome.disabledSurface,
  } as const;
}
