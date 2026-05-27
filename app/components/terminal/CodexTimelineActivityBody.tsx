import React from "react";
import {
  StyleSheet,
  Text,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";
import { MessageBody } from "./CodexMessageBody";
import { CodexTimelineActivityOutput } from "./CodexTimelineActivityOutput";
import type { ZenActivityTimelineItem } from "./CodexTimelineActivityTypes";

interface CodexTimelineActivityBodyProps {
  body: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  activityKind?: ZenActivityTimelineItem["activityKind"];
  bodyKind?: ZenActivityTimelineItem["bodyKind"];
  textSelectable: boolean;
  truncateBody(value: string, limit: number): string;
}

export function CodexTimelineActivityBody({
  body,
  chrome,
  theme,
  activityKind,
  bodyKind,
  textSelectable,
  truncateBody,
}: CodexTimelineActivityBodyProps) {
  const displayBody = truncateBody(body, 1800);
  if (activityKind === "reasoning") {
    return (
      <MessageBody
        value={displayBody}
        chrome={chrome}
        theme={theme}
        compact
      />
    );
  }

  if (bodyKind) {
    return (
      <CodexTimelineActivityOutput
        body={displayBody}
        bodyKind={bodyKind}
        chrome={chrome}
        theme={theme}
        textSelectable={textSelectable}
      />
    );
  }

  return (
    <Text
      selectable={textSelectable}
      style={[styles.body, { color: chrome.textSubtle }]}
    >
      {displayBody}
    </Text>
  );
}

const styles = StyleSheet.create({
  body: {
    marginTop: 6,
    fontSize: 12,
    lineHeight: 18,
    fontFamily: Typography.chatMonoFont,
  },
});
