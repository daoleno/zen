import React, {
  useMemo,
} from "react";
import { StyleSheet, View } from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { parseMessageBlocks } from "./CodexMessageBodyModel";
import { CodexMessageBlock } from "./CodexMessageBlock";
import { prepareCodexMarkdown } from "./CodexNativeMarkdownBodyModel";

interface MessageBodyProps {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
  dense?: boolean;
  streaming?: boolean;
}

export function MessageBody({
  value,
  chrome,
  theme,
  compact = false,
  dense = false,
  streaming = false,
}: MessageBodyProps) {
  const displayValue = useMemo(
    () => streaming ? prepareCodexMarkdown(value, true) : value,
    [streaming, value],
  );
  const blocks = useMemo(() => parseMessageBlocks(displayValue), [displayValue]);
  if (blocks.length === 0) {
    return null;
  }
  return (
    <View style={styles.messageBody}>
      {blocks.map((block, index) => {
        const isLast = index === blocks.length - 1;
        return (
          <CodexMessageBlock
            key={index}
            block={block}
            chrome={chrome}
            theme={theme}
            compact={compact}
            dense={dense}
            isLast={isLast}
          />
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  messageBody: {
    alignSelf: "stretch",
    width: "100%",
    minWidth: 0,
  },
});
