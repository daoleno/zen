import React, { useMemo } from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { prepareCodexMarkdown } from "./CodexNativeMarkdownBodyModel";

interface CodexNativeMarkdownBodyProps {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
  streaming?: boolean;
  renderFallback(value: string): React.ReactNode;
}

export function CodexNativeMarkdownBody({
  value,
  streaming = false,
  renderFallback,
}: CodexNativeMarkdownBodyProps) {
  const markdown = useMemo(
    () => prepareCodexMarkdown(value, streaming),
    [streaming, value],
  );

  return renderFallback(markdown || value);
}
