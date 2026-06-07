import React, {
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
} from "react";
import { Linking, StyleSheet, View } from "react-native";
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

interface CodexNativeMarkdownBodyProps {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
  streaming?: boolean;
  renderFallback(value: string): React.ReactNode;
}

const MARKDOWN_SELECTION_FREEZE_DELAY_MS = 520;

export function CodexNativeMarkdownBody({
  value,
  chrome,
  theme,
  compact = false,
  streaming = false,
  renderFallback,
}: CodexNativeMarkdownBodyProps) {
  const {
    selectable: textSelectable,
    onTextSelectionGestureStart,
    onTextSelectionGestureEnd,
  } = useContext(TimelineTextSelectableContext);
  const selectionStartTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const selectionFreezeActiveRef = useRef(false);
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
  const clearSelectionStartTimer = useCallback(() => {
    if (!selectionStartTimerRef.current) {
      return;
    }
    clearTimeout(selectionStartTimerRef.current);
    selectionStartTimerRef.current = null;
  }, []);
  const handleTouchStart = useCallback(() => {
    if (!onTextSelectionGestureStart) {
      return;
    }
    clearSelectionStartTimer();
    selectionFreezeActiveRef.current = false;
    selectionStartTimerRef.current = setTimeout(() => {
      selectionStartTimerRef.current = null;
      selectionFreezeActiveRef.current = true;
      onTextSelectionGestureStart();
    }, MARKDOWN_SELECTION_FREEZE_DELAY_MS);
  }, [
    clearSelectionStartTimer,
    onTextSelectionGestureStart,
  ]);
  const handleTouchEnd = useCallback(() => {
    const freezeStarted = selectionFreezeActiveRef.current;
    clearSelectionStartTimer();
    if (!freezeStarted) {
      return;
    }
    selectionFreezeActiveRef.current = false;
    onTextSelectionGestureEnd?.();
  }, [clearSelectionStartTimer, onTextSelectionGestureEnd]);

  useEffect(() => {
    return clearSelectionStartTimer;
  }, [clearSelectionStartTimer]);

  if (!markdown) {
    return fallback;
  }

  return (
    <View
      collapsable={false}
      onTouchStart={handleTouchStart}
      onTouchEnd={handleTouchEnd}
      onTouchCancel={handleTouchEnd}
      style={styles.messageBody}
    >
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
    </View>
  );
}

const styles = StyleSheet.create({
  messageBody: {
    minWidth: 0,
  },
});
