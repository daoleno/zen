import React from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  View,
} from "react-native";
import { Typography } from "../../constants/tokens";
import {
  buildTerminalChrome,
  type TerminalThemePalette,
} from "../../constants/terminalThemes";
import {
  highlightCodeLine,
  type HighlightTokenKind,
} from "./gitDiffSyntaxHighlight";
import { withAlpha } from "./gitDiffColor";

export function GitDiffBlock({
  path,
  patch,
  theme,
}: {
  path: string;
  patch: string;
  theme: TerminalThemePalette;
}) {
  const chrome = React.useMemo(() => buildTerminalChrome(theme), [theme]);
  const lines = React.useMemo(() => patch.split("\n"), [patch]);

  return (
    <ScrollView horizontal showsHorizontalScrollIndicator nestedScrollEnabled={false}>
      <View style={styles.diffBlock}>
        {lines.map((line, index) => {
          const presentation = linePresentation(line, theme, chrome);
          return (
            <View
              key={index}
              style={[
                styles.diffLineWrap,
                presentation.backgroundColor
                  ? { backgroundColor: presentation.backgroundColor }
                  : null,
              ]}
            >
              <Text style={[styles.diffLineNumber, { color: chrome.textSubtle }]}>
                {index + 1}
              </Text>
              <Text selectable style={[styles.diffLine, { color: presentation.color }]}>
                {renderDiffLine(line, path, presentation, theme, chrome)}
              </Text>
            </View>
          );
        })}
      </View>
    </ScrollView>
  );
}

function linePresentation(
  line: string,
  theme: TerminalThemePalette,
  chrome: ReturnType<typeof buildTerminalChrome>,
): { color: string; backgroundColor?: string } {
  if (line.startsWith("@@")) {
    return {
      color: theme.yellow,
      backgroundColor: withAlpha(theme.yellow, 0.1),
    };
  }
  if (line.startsWith("+") && !line.startsWith("+++")) {
    return {
      color: theme.green,
      backgroundColor: withAlpha(theme.green, 0.08),
    };
  }
  if (line.startsWith("-") && !line.startsWith("---")) {
    return {
      color: theme.red,
      backgroundColor: withAlpha(theme.red, 0.08),
    };
  }
  if (line.startsWith("diff --git") || line.startsWith("index ")) {
    return {
      color: chrome.textMuted,
      backgroundColor: withAlpha(chrome.textMuted, 0.06),
    };
  }
  if (
    line.startsWith("rename from ")
    || line.startsWith("rename to ")
    || line.startsWith("new file mode")
    || line.startsWith("deleted file mode")
  ) {
    return {
      color: theme.blue,
      backgroundColor: withAlpha(theme.blue, 0.08),
    };
  }
  if (line.startsWith("+++ ") || line.startsWith("--- ")) {
    return {
      color: theme.cursor,
      backgroundColor: withAlpha(theme.cursor, 0.08),
    };
  }
  return { color: chrome.text };
}

function renderDiffLine(
  line: string,
  path: string,
  presentation: { color: string; backgroundColor?: string },
  theme: TerminalThemePalette,
  chrome: ReturnType<typeof buildTerminalChrome>,
) {
  if (!line) {
    return " ";
  }
  if (!isPatchCodeLine(line)) {
    return line;
  }

  const prefix = line[0];
  const code = line.slice(1) || " ";
  const baseColor = prefix === " " ? chrome.text : presentation.color;
  return (
    <>
      <Text style={{ color: presentation.color }}>{prefix}</Text>
      {highlightCodeLine(code, path).map((segment, index) => (
        <Text
          key={`${index}:${segment.kind}`}
          style={{ color: syntaxColor(segment.kind, theme, chrome, baseColor) }}
        >
          {segment.text}
        </Text>
      ))}
    </>
  );
}

function isPatchCodeLine(line: string) {
  if (line.startsWith("+++") || line.startsWith("---")) {
    return false;
  }
  return line.startsWith("+") || line.startsWith("-") || line.startsWith(" ");
}

function syntaxColor(
  kind: HighlightTokenKind,
  theme: TerminalThemePalette,
  chrome: ReturnType<typeof buildTerminalChrome>,
  baseColor: string,
) {
  switch (kind) {
    case "attribute":
    case "property":
      return theme.cyan;
    case "comment":
      return chrome.textSubtle;
    case "constant":
    case "number":
      return theme.yellow;
    case "function":
      return theme.blue;
    case "keyword":
    case "tag":
      return theme.magenta;
    case "operator":
    case "punctuation":
      return chrome.textMuted;
    case "string":
      return theme.green;
    default:
      return baseColor;
  }
}

const styles = StyleSheet.create({
  diffBlock: {
    minWidth: "100%",
  },
  diffLineWrap: {
    minHeight: 18,
    flexDirection: "row",
    alignItems: "center",
    paddingRight: 12,
  },
  diffLineNumber: {
    width: 36,
    textAlign: "right",
    paddingRight: 8,
    fontSize: 10,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
  diffLine: {
    fontSize: 10,
    lineHeight: 15,
    fontFamily: Typography.terminalFont,
  },
});
