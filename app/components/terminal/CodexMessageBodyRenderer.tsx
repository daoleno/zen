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

interface MessageBodyProps {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
  dense?: boolean;
}

export function MessageBody({
  value,
  chrome,
  theme,
  compact = false,
  dense = false,
}: MessageBodyProps) {
  const blocks = useMemo(() => parseMessageBlocks(value), [value]);
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
    minWidth: 0,
  },
});
