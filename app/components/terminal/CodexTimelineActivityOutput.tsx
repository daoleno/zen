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
  emphasizeError?: boolean;
}

const OUTPUT_MAX_HEIGHT = 220;

export function CodexTimelineActivityOutput({
  body,
  bodyKind,
  chrome,
  theme,
  emphasizeError = false,
}: CodexTimelineActivityOutputProps) {
  const selectableTextProps = useTimelineSelectableTextProps();
  const lines = splitOutputLines(body);
  if (lines.length === 0) {
    return null;
  }

  const lineColor = emphasizeError ? chrome.danger : chrome.textMuted;
  const content = (
    <View style={styles.output}>
      {lines.map((line, index) => (
        <Text
          key={index}
          {...selectableTextProps}
          style={[styles.line, { color: lineColor }]}
        >
          {bodyKind === "diff-stat"
            ? renderDiffStatLine(line, chrome, theme)
            : line || " "}
        </Text>
      ))}
    </View>
  );

  return (
    <View
      style={[
        styles.frame,
        {
          backgroundColor: emphasizeError
            ? chrome.dangerSoft
            : chrome.composerInput,
          borderColor: chrome.border,
        },
      ]}
    >
      {bodyKind === "diff-stat" ? (
        <ScrollView
          horizontal
          nestedScrollEnabled
          showsHorizontalScrollIndicator={false}
          style={styles.verticalBound}
          contentContainerStyle={styles.scrollContent}
        >
          {content}
        </ScrollView>
      ) : (
        <ScrollView
          nestedScrollEnabled
          showsVerticalScrollIndicator={lines.length > 10}
          style={styles.verticalBound}
        >
          {content}
        </ScrollView>
      )}
    </View>
  );
}

function splitOutputLines(body: string) {
  const lines = body.replace(/\r\n/g, "\n").split("\n");
  if (lines.length > 1 && lines[lines.length - 1] === "") {
    return lines.slice(0, -1);
  }
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
  frame: {
    borderRadius: 10,
    borderWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 10,
    paddingVertical: 8,
    overflow: "hidden",
  },
  scrollContent: {
    minWidth: "100%",
  },
  verticalBound: {
    maxHeight: OUTPUT_MAX_HEIGHT,
  },
  output: {
    minWidth: "100%",
  },
  line: {
    fontSize: 12.5,
    lineHeight: 18,
    fontFamily: Typography.chatMonoFont,
    letterSpacing: 0,
  },
});
