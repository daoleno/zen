import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { withAlpha } from "./colorWithAlpha";

const HEX_COLOR = /^#[0-9a-fA-F]{6}$/;

/**
 * Translucent tint of a chrome ink. Chat chrome is hex in every shipped
 * theme; any other color form (rgba, "transparent") resolves to the given
 * opaque fallback instead of silently painting the full-strength ink.
 */
export function chromeTint(color: string, alpha: number, fallback: string): string {
  return HEX_COLOR.test(color.trim()) ? withAlpha(color, alpha) : fallback;
}

/** True when the chat canvas reads as a light surface. */
export function chromeIsLight(chrome: TerminalThemeChrome): boolean {
  const value = chrome.text.trim();
  if (!HEX_COLOR.test(value)) {
    return false;
  }
  const red = Number.parseInt(value.slice(1, 3), 16);
  const green = Number.parseInt(value.slice(3, 5), 16);
  const blue = Number.parseInt(value.slice(5, 7), 16);
  // Dark ink means a light canvas.
  return (0.299 * red + 0.587 * green + 0.114 * blue) / 255 < 0.5;
}

/**
 * Quiet neutral fill for resting controls (Plus, disabled Send, stop) that
 * sit inside the Composer capsule or on the chat canvas.
 */
export function composerNeutralFill(
  chrome: TerminalThemeChrome,
  strength: "rest" | "strong" = "rest",
): string {
  const alpha = strength === "strong" ? 0.14 : 0.075;
  return chromeTint(chrome.text, alpha, chrome.disabledSurface);
}

/** Lit top edge of a floating glass material. */
export function composerLitEdge(chrome: TerminalThemeChrome): string {
  return withAlpha("#FFFFFF", chromeIsLight(chrome) ? 0.85 : 0.09);
}

/** Visual diameter of the circular Composer controls inside a 44 pt target. */
export const COMPOSER_CONTROL_DISC_SIZE = 34;
