import React from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import type { ZenActivityTimelineItem } from "./CodexTimelineActivityTypes";
import { useTimelineSelectableTextProps } from "./TimelineTextSelectableContext";

interface CodexTimelineActivityOutputProps {
  body: string;
  bodyKind?: ZenActivityTimelineItem["bodyKind"];
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
}

export function CodexTimelineActivityOutput({
  body,
  bodyKind,
  chrome,
  theme,
}: CodexTimelineActivityOutputProps) {
  const selectableTextProps = useTimelineSelectableTextProps();
  const lines = splitOutputLines(body);
  if (lines.length === 0) {
    return null;
  }

  return (
    <ScrollView
      horizontal
      nestedScrollEnabled
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.scrollContent}
    >
      <View style={styles.output}>
        {lines.map((line, index) => (
          <Text
            key={index}
            {...selectableTextProps}
            style={[styles.line, { color: chrome.textSubtle }]}
          >
            {bodyKind === "diff-stat"
              ? renderDiffStatLine(line, chrome, theme)
              : line || " "}
          </Text>
        ))}
      </View>
    </ScrollView>
  );
}

function splitOutputLines(body: string) {
  const lines = body.replace(/\r\n/g, "\n").split("\n");
  return lines.length > 0 ? lines : [];
}

function renderDiffStatLine(
  line: string,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
) {
  if (!line) {
    return " ";
  }

  const stat = /^(.*?)(\s+\|\s+)(\d+)?(\s*)([+\-]+)?(\s*)$/.exec(line);
  if (!stat) {
    return (
      <Text style={{ color: chrome.textMuted }}>
        {line}
      </Text>
    );
  }

  const [, path, divider, count = "", gap, graph = "", tail] = stat;
  return (
    <>
      <Text style={{ color: chrome.textMuted }}>{path}</Text>
      <Text style={{ color: chrome.textSubtle }}>{divider}</Text>
      {count ? <Text style={{ color: theme.yellow }}>{count}</Text> : null}
      {gap ? <Text style={{ color: chrome.textSubtle }}>{gap}</Text> : null}
      {Array.from(graph).map((char, index) => (
        <Text
          key={index}
          style={{ color: char === "+" ? theme.green : theme.red }}
        >
          {char}
        </Text>
      ))}
      {tail ? <Text style={{ color: chrome.textSubtle }}>{tail}</Text> : null}
    </>
  );
}

const styles = StyleSheet.create({
  scrollContent: {
    minWidth: "100%",
  },
  output: {
    minWidth: "100%",
    paddingTop: 2,
  },
  line: {
    fontSize: 12,
    lineHeight: 19,
    fontFamily: Typography.chatMonoFont,
    letterSpacing: 0,
  },
});
