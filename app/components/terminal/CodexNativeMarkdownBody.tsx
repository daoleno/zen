import React, {
  useCallback,
  useContext,
  useMemo,
} from "react";
import { Linking, StyleSheet } from "react-native";
import {
  EnrichedMarkdownText,
  type LinkPressEvent,
} from "react-native-enriched-markdown";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { CodexMarkdownErrorBoundary } from "./CodexMarkdownErrorBoundary";
import {
  codexMarkdownStyle,
  isSafeMarkdownUrl,
  prepareCodexMarkdown,
} from "./CodexNativeMarkdownBodyModel";
import { TimelineTextSelectableContext } from "./TimelineTextSelectableContext";

const USE_NATIVE_MARKDOWN_BODY = true;

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
  chrome,
  theme,
  compact = false,
  streaming = false,
  renderFallback,
}: CodexNativeMarkdownBodyProps) {
  const textSelectable = useContext(TimelineTextSelectableContext);
  const markdown = useMemo(() => prepareCodexMarkdown(value, streaming), [streaming, value]);
  const markdownStyle = useMemo(
    () => codexMarkdownStyle(chrome, theme, compact),
    [chrome, compact, theme],
  );
  const fallback = renderFallback(markdown || value);
  const handleLinkPress = useCallback((event: LinkPressEvent) => {
    const url = event.url.trim();
    if (!isSafeMarkdownUrl(url)) {
      return;
    }
    void Linking.openURL(url).catch(() => undefined);
  }, []);

  if (!USE_NATIVE_MARKDOWN_BODY || !markdown) {
    return fallback;
  }

  return (
    <CodexMarkdownErrorBoundary fallback={fallback} resetKey={markdown}>
      <EnrichedMarkdownText
        markdown={markdown}
        markdownStyle={markdownStyle}
        containerStyle={styles.messageBody}
        flavor="github"
        selectable={textSelectable}
        allowFontScaling={false}
        allowTrailingMargin={false}
        enableLinkPreview={false}
        md4cFlags={{ latexMath: false, underline: false }}
        onLinkPress={handleLinkPress}
        streamingAnimation={streaming}
        spoilerOverlay="solid"
      />
    </CodexMarkdownErrorBoundary>
  );
}

const styles = StyleSheet.create({
  messageBody: {
    minWidth: 0,
  },
});
