import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

export type MermaidThemeMode = "dark" | "light";

export type MermaidRenderTheme = {
  mode: MermaidThemeMode;
  background: string;
  foreground: string;
  lineColor: string;
  primaryColor: string;
  primaryTextColor: string;
  primaryBorderColor: string;
  secondaryColor: string;
  tertiaryColor: string;
  clusterBkg: string;
  clusterBorder: string;
  edgeLabelBackground: string;
  fontSize: string;
};

export function mermaidThemeFromChrome(
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
  mode: MermaidThemeMode,
): MermaidRenderTheme {
  return {
    mode,
    background: "transparent",
    foreground: chrome.text,
    lineColor: chrome.borderStrong,
    primaryColor: chrome.surface,
    primaryTextColor: chrome.text,
    primaryBorderColor: chrome.border,
    secondaryColor: chrome.surfaceMuted,
    tertiaryColor: chrome.surfaceActive,
    clusterBkg: chrome.surfaceMuted,
    clusterBorder: chrome.border,
    edgeLabelBackground: chrome.surface,
    fontSize: "13px",
  };
}

export function mermaidThemeIsDark(theme: TerminalThemePalette) {
  const background = theme.background.trim().toLowerCase();
  if (background === "transparent" || background === "") {
    return true;
  }
  const hex = background.replace("#", "");
  if (!/^[0-9a-f]{6}$/i.test(hex)) {
    return true;
  }
  const red = Number.parseInt(hex.slice(0, 2), 16);
  const green = Number.parseInt(hex.slice(2, 4), 16);
  const blue = Number.parseInt(hex.slice(4, 6), 16);
  const luminance = (0.2126 * red + 0.7152 * green + 0.0722 * blue) / 255;
  return luminance < 0.5;
}
