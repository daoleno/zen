import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { MessageBody } from "./InterfaceMessageBody";
import { InterfaceTimelineActivityOutput } from "./InterfaceTimelineActivityOutput";
import type { ZenActivityTimelineItem } from "./InterfaceTimelineActivityTypes";

interface InterfaceTimelineActivityBodyProps {
  body: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  activityKind?: ZenActivityTimelineItem["activityKind"];
  bodyKind?: ZenActivityTimelineItem["bodyKind"];
  tone?: ZenActivityTimelineItem["tone"];
  streaming?: boolean;
  truncateBody(value: string, limit: number): string;
}

export function InterfaceTimelineActivityBody({
  body,
  chrome,
  theme,
  activityKind,
  bodyKind,
  tone,
  streaming,
  truncateBody,
}: InterfaceTimelineActivityBodyProps) {
  const displayBody = truncateBody(body, 1800);
  const emphasizeError = tone === "failed";

  if (activityKind === "reasoning") {
    return (
      <MessageBody
        value={displayBody}
        chrome={chrome}
        theme={theme}
        compact
        streaming={streaming}
      />
    );
  }

  return (
    <InterfaceTimelineActivityOutput
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
