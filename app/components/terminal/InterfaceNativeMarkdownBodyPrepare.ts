import remend, { type RemendOptions } from "remend";
import {
  isTimelineProjectionPerfEnabled,
  recordMarkdownPrepareSample,
} from "./timelineProjectionPerf";

/** Shared Android+iOS product path. Fallback is error-boundary only, not a platform fork. */
export const INTERFACE_MARKDOWN_MOBILE_RENDERER = "enriched" as const;

const STREAMING_REMEND_OPTIONS: RemendOptions = {
  images: true,
  inlineKatex: false,
  linkMode: "text-only",
};

export function prepareInterfaceMarkdown(value: string, streaming: boolean) {
  const measure = isTimelineProjectionPerfEnabled();
  const started = measure ? nowMs() : 0;
  let markdown = value
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\r\n/g, "\n")
    .trim();
  if (!markdown) {
    if (measure) {
      recordMarkdownPrepareSample({
        durationMs: nowMs() - started,
        streaming,
        inputChars: value.length,
        outputChars: 0,
      });
    }
    return "";
  }
  if (streaming) {
    markdown = remend(markdown, STREAMING_REMEND_OPTIONS);
  }
  const prepared = stripMarkdownImages(markdown);
  if (measure) {
    recordMarkdownPrepareSample({
      durationMs: nowMs() - started,
      streaming,
      inputChars: value.length,
      outputChars: prepared.length,
    });
  }
  return prepared;
}

function nowMs() {
  const perf = (
    globalThis as { performance?: { now(): number } }
  ).performance;
  return perf?.now?.() ?? Date.now();
}

function stripMarkdownImages(value: string) {
  return value.replace(
    /!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g,
    (_match, alt, url) => {
      const label = String(alt || "").trim();
      const href = String(url || "").trim();
      if (!href) {
        return label;
      }
      return label ? `[${label}](${href})` : href;
    },
  );
}
