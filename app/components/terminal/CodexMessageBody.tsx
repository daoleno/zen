import React, {
  useEffect,
  useState,
} from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { MessageBody } from "./CodexFallbackMessageBody";
import { CodexNativeMarkdownBody } from "./CodexNativeMarkdownBody";

export { MessageBody } from "./CodexFallbackMessageBody";

export function StreamingMessageBody({
  value,
  chrome,
  theme,
  stream,
}: {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  stream: boolean;
}) {
  const [visibleChars, setVisibleChars] = useState(stream ? 0 : value.length);

  useEffect(() => {
    if (!stream) {
      setVisibleChars(value.length);
      return;
    }
    setVisibleChars((current) => Math.min(current, value.length));
  }, [stream, value.length]);

  useEffect(() => {
    if (!stream || visibleChars >= value.length) {
      return;
    }
    const timer = setTimeout(() => {
      setVisibleChars((current) => Math.min(value.length, current + 18));
    }, 24);
    return () => clearTimeout(timer);
  }, [stream, value.length, visibleChars]);

  const renderedValue = stream ? value.slice(0, visibleChars) : value;
  return (
    <View style={styles.zenAssistantContent}>
      <CodexNativeMarkdownBody
        value={renderedValue}
        chrome={chrome}
        theme={theme}
        streaming={stream && visibleChars < value.length}
        renderFallback={(fallbackValue) => (
          <MessageBody value={fallbackValue} chrome={chrome} theme={theme} />
        )}
      />
      {stream && visibleChars < value.length ? (
        <View style={[styles.zenStreamCursor, { backgroundColor: chrome.accent }]} />
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  zenAssistantContent: {
    minWidth: 0,
  },
  zenStreamCursor: {
    width: 6,
    height: 16,
    borderRadius: 3,
    opacity: 0.65,
  },
});
