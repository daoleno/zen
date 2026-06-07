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
import { useTimelineSelectableTextProps } from "./TimelineTextSelectableContext";

interface CodexTimelineActivityBodyProps {
  body: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  activityKind?: ZenActivityTimelineItem["activityKind"];
  bodyKind?: ZenActivityTimelineItem["bodyKind"];
  truncateBody(value: string, limit: number): string;
}

export function CodexTimelineActivityBody({
  body,
  chrome,
  theme,
  activityKind,
  bodyKind,
  truncateBody,
}: CodexTimelineActivityBodyProps) {
  const selectableTextProps = useTimelineSelectableTextProps();
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
      />
    );
  }

  return (
    <Text
      {...selectableTextProps}
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
