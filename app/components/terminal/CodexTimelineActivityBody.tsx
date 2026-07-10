import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { MessageBody } from "./CodexMessageBody";
import { CodexTimelineActivityOutput } from "./CodexTimelineActivityOutput";
import type { ZenActivityTimelineItem } from "./CodexTimelineActivityTypes";

interface CodexTimelineActivityBodyProps {
  body: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  activityKind?: ZenActivityTimelineItem["activityKind"];
  bodyKind?: ZenActivityTimelineItem["bodyKind"];
  tone?: ZenActivityTimelineItem["tone"];
  truncateBody(value: string, limit: number): string;
}

export function CodexTimelineActivityBody({
  body,
  chrome,
  theme,
  activityKind,
  bodyKind,
  tone,
  truncateBody,
}: CodexTimelineActivityBodyProps) {
  const displayBody = truncateBody(body, 1800);
  const emphasizeError = tone === "failed";

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

  return (
    <CodexTimelineActivityOutput
      body={displayBody}
      bodyKind={bodyKind}
      chrome={chrome}
      theme={theme}
      emphasizeError={emphasizeError}
    />
  );
}

export function isMeaningfulActivityBody(
  body: string | undefined,
  tone: ZenActivityTimelineItem["tone"],
): body is string {
  if (!body || !body.trim()) {
    return false;
  }
  const normalized = body.trim().toLowerCase();
  if (
    (tone === "success" || tone === "neutral") &&
    (normalized === "(no output)" || normalized === "no output")
  ) {
    return false;
  }
  return true;
}
