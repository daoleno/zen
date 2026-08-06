import { parseMarkdownLinkToken } from "../markdown/markdownLinks";
import {
  isTimelineProjectionPerfEnabled,
  recordMarkdownParseSample,
} from "./timelineProjectionPerf";

export type MessageBlock =
  | { type: "heading"; level: number; text: string }
  | { type: "paragraph"; text: string }
  | { type: "list"; items: MessageListItem[] }
  | MessageTableBlock
  | { type: "code"; text: string; language?: string }
  | { type: "quote"; text: string };

export type MessageListItem = {
  marker: string;
  text: string;
  depth: number;
  ordered: boolean;
  taskState?: "checked" | "unchecked";
};

export type InlineMessagePart = {
  text: string;
  kind?: "bold" | "code" | "italic" | "link" | "strike";
  url?: string;
};

export type MessageTableAlignment = "left" | "center" | "right";

export type MessageTableBlock = {
  type: "table";
  headers: string[];
  alignments: MessageTableAlignment[];
  rows: string[][];
};

const MAX_BARE_JSON_LINES = 240;
const MAX_BARE_JSON_CHARS = 140_000;

export function parseMessageBlocks(value: string): MessageBlock[] {
  const measure = isTimelineProjectionPerfEnabled();
  const started = measure ? markdownPerfNowMs() : 0;
  const blocks = parseMessageBlocksUninstrumented(value);
  if (measure) {
    recordMarkdownParseSample({
      durationMs: markdownPerfNowMs() - started,
      inputChars: value.length,
      blockCount: blocks.length,
    });
  }
  return blocks;
}

function markdownPerfNowMs() {
  const perf = (
    globalThis as { performance?: { now(): number } }
  ).performance;
  return perf?.now?.() ?? Date.now();
}

function parseMessageBlocksUninstrumented(value: string): MessageBlock[] {
  const lines = value
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\r\n/g, "\n")
    .split("\n");
  const blocks: MessageBlock[] = [];
  let paragraph: string[] = [];
  let list: MessageListItem[] = [];
  let quote: string[] = [];
  let code: { fence: string; language?: string; lines: string[] } | null = null;

  const flushParagraph = () => {
    const text = normalizeProseText(paragraph.join(" ")).trim();
    if (text) {
      blocks.push({ type: "paragraph", text });
    }
    paragraph = [];
  };
  const flushList = () => {
    if (list.length > 0) {
      blocks.push({ type: "list", items: list });
    }
    list = [];
  };
  const flushQuote = () => {
    const text = normalizeProseText(quote.join(" ")).trim();
    if (text) {
      blocks.push({ type: "quote", text });
    }
    quote = [];
  };
  const flushOpenBlocks = () => {
    flushParagraph();
    flushList();
    flushQuote();
  };

  for (let lineIndex = 0; lineIndex < lines.length; lineIndex += 1) {
    const rawLine = lines[lineIndex];
    const line = rawLine.trimEnd();
    const trimmed = line.trim();

    if (code) {
      if (new RegExp(`^${escapeRegex(code.fence)}\\s*$`).test(trimmed)) {
        blocks.push({
          type: "code",
          text: code.lines.join("\n").replace(/\n+$/, ""),
          language: code.language,
        });
        code = null;
      } else {
        code.lines.push(rawLine);
      }
      continue;
    }

    const fence = /^(```|~~~)\s*(.*)$/.exec(trimmed);
    if (fence) {
      flushOpenBlocks();
      code = {
        fence: fence[1],
        language: normalizeCodeFenceLanguage(fence[2]),
        lines: [],
      };
      continue;
    }

    if (!trimmed) {
      flushOpenBlocks();
      continue;
    }

    const jsonBlock = parseBareJsonBlockAt(lines, lineIndex);
    if (jsonBlock) {
      flushOpenBlocks();
      blocks.push({
        type: "code",
        text: jsonBlock.text,
        language: "json",
      });
      lineIndex = jsonBlock.nextLineIndex - 1;
      continue;
    }

    const table = parseTableAt(lines, lineIndex);
    if (table) {
      flushOpenBlocks();
      blocks.push(table.block);
      lineIndex = table.nextLineIndex - 1;
      continue;
    }

    const heading = /^(#{1,6})\s+(.+)$/.exec(trimmed);
    if (heading) {
      flushOpenBlocks();
      blocks.push({
        type: "heading",
        level: heading[1].length,
        text: normalizeProseText(heading[2]).trim(),
      });
      continue;
    }

    const listItem = /^(\s*)((?:[-*+])|\d+[.)])\s+(.+)$/.exec(line);
    if (listItem) {
      flushParagraph();
      flushQuote();
      const task = /^\[([ xX])]\s+(.+)$/.exec(listItem[3]);
      list.push({
        marker: normalizeListMarker(listItem[2]),
        text: normalizeProseText(task ? task[2] : listItem[3]).trim(),
        depth: listDepthForIndent(listItem[1]),
        ordered: /^\d/.test(listItem[2]),
        taskState: task
          ? task[1].trim()
            ? "checked"
            : "unchecked"
          : undefined,
      });
      continue;
    }

    const quoteItem = /^>\s?(.+)$/.exec(trimmed);
    if (quoteItem) {
      flushParagraph();
      flushList();
      quote.push(quoteItem[1].trim());
      continue;
    }

    if (appendListContinuation(list, line)) {
      continue;
    }

    flushList();
    flushQuote();
    paragraph.push(trimmed);
  }

  if (code) {
    blocks.push({
      type: "code",
      text: code.lines.join("\n").replace(/\n+$/, ""),
      language: code.language,
    });
  }
  flushOpenBlocks();
  return blocks;
}

function appendListContinuation(list: MessageListItem[], line: string) {
  if (list.length === 0 || !/^\s+\S/.test(line)) {
    return false;
  }

  const leading = line.match(/^\s*/)?.[0] ?? "";
  const text = normalizeProseText(line.trim()).trim();
  if (!text) {
    return false;
  }

  const continuationDepth = listDepthForIndent(leading);
  const targetItem = findListContinuationTarget(list, continuationDepth);
  const separator = continuationDepth > targetItem.depth ? "\n" : " ";
  targetItem.text = normalizeProseText(
    `${targetItem.text}${separator}${text}`,
  ).trim();
  return true;
}

function findListContinuationTarget(
  list: MessageListItem[],
  continuationDepth: number,
) {
  for (let index = list.length - 1; index >= 0; index -= 1) {
    if (list[index].depth <= continuationDepth) {
      return list[index];
    }
  }
  return list[list.length - 1];
}

function normalizeListMarker(value: string) {
  if (/^\d+[.)]$/.test(value)) {
    return value.replace(/\)$/, ".");
  }
  return "\u2022";
}

function listDepthForIndent(value: string) {
  const spaces = value.replace(/\t/g, "    ").length;
  if (spaces === 0) {
    return 0;
  }
  return Math.min(6, Math.max(1, Math.floor((spaces + 2) / 4)));
}

function parseBareJsonBlockAt(
  lines: string[],
  startIndex: number,
): { text: string; nextLineIndex: number } | null {
  const firstLine = lines[startIndex]?.trim();
  if (!looksLikeJsonContainerStart(firstLine)) {
    return null;
  }

  let charCount = 0;
  const collected: string[] = [];
  const maxLineIndex = Math.min(lines.length, startIndex + MAX_BARE_JSON_LINES);
  for (let lineIndex = startIndex; lineIndex < maxLineIndex; lineIndex += 1) {
    const line = lines[lineIndex];
    collected.push(line);
    charCount += line.length + 1;
    if (charCount > MAX_BARE_JSON_CHARS) {
      return null;
    }

    const candidate = collected.join("\n").trim();
    if (!looksLikeCompleteJsonContainer(candidate)) {
      continue;
    }

    const parsed = parseJsonContainer(candidate);
    if (!parsed.ok || !isProbablyStandaloneJson(candidate)) {
      continue;
    }

    return {
      text: candidate,
      nextLineIndex: lineIndex + 1,
    };
  }

  return null;
}

function looksLikeJsonContainerStart(value: string | undefined) {
  if (!value) {
    return false;
  }
  return value.startsWith("{") || value.startsWith("[");
}

function looksLikeCompleteJsonContainer(value: string) {
  if (value.startsWith("{")) {
    return value.endsWith("}");
  }
  if (value.startsWith("[")) {
    return value.endsWith("]");
  }
  return false;
}

function parseJsonContainer(
  value: string,
): { ok: true; value: unknown } | { ok: false } {
  try {
    const parsed: unknown = JSON.parse(value);
    if (parsed && typeof parsed === "object") {
      return { ok: true, value: parsed };
    }
  } catch {
    return { ok: false };
  }
  return { ok: false };
}

function isProbablyStandaloneJson(value: string) {
  if (value === "{}" || value === "[]") {
    return true;
  }
  if (value.startsWith("{")) {
    return value.includes(":");
  }
  if (value.startsWith("[")) {
    return /[,:{\[]/.test(value.slice(1, -1));
  }
  return false;
}

function parseTableAt(
  lines: string[],
  startIndex: number,
): { block: MessageTableBlock; nextLineIndex: number } | null {
  if (startIndex + 1 >= lines.length) {
    return null;
  }

  const header = splitTableRow(lines[startIndex]);
  const separator = splitTableRow(lines[startIndex + 1]);
  if (!header || !separator) {
    return null;
  }

  const alignments = separator.cells.map(parseTableAlignment);
  if (alignments.some((alignment) => alignment === null)) {
    return null;
  }

  const columnCount = Math.max(header.cells.length, separator.cells.length);
  if (columnCount === 0) {
    return null;
  }

  const rows: string[][] = [];
  let lineIndex = startIndex + 2;
  while (lineIndex < lines.length) {
    const row = splitTableRow(lines[lineIndex]);
    if (!row) {
      break;
    }
    rows.push(normalizeTableCells(row.cells, columnCount));
    lineIndex += 1;
  }

  return {
    block: {
      type: "table",
      headers: normalizeTableCells(header.cells, columnCount),
      alignments: normalizeTableAlignments(
        alignments.filter(
          (alignment): alignment is MessageTableAlignment => alignment !== null,
        ),
        columnCount,
      ),
      rows,
    },
    nextLineIndex: lineIndex,
  };
}

function splitTableRow(rawLine: string): { cells: string[] } | null {
  const line = rawLine.trim();
  if (!line || !hasTableBoundary(line)) {
    return null;
  }

  const body = trimTableBoundary(line);
  const cells: string[] = [];
  let cell = "";
  let inCodeSpan = false;

  for (let index = 0; index < body.length; index += 1) {
    const char = body[index];
    const next = body[index + 1];

    if (char === "\\" && next === "|") {
      cell += "|";
      index += 1;
      continue;
    }

    if (char === "`") {
      inCodeSpan = !inCodeSpan;
      cell += char;
      continue;
    }

    if (char === "|" && !inCodeSpan) {
      cells.push(normalizeTableCell(cell));
      cell = "";
      continue;
    }

    cell += char;
  }
  cells.push(normalizeTableCell(cell));

  return cells.length > 0 ? { cells } : null;
}

function hasTableBoundary(value: string) {
  if (value.startsWith("|") || value.endsWith("|")) {
    return true;
  }

  let inCodeSpan = false;
  for (let index = 0; index < value.length; index += 1) {
    const char = value[index];
    if (char === "`") {
      inCodeSpan = !inCodeSpan;
      continue;
    }
    if (char === "|" && !inCodeSpan && value[index - 1] !== "\\") {
      return true;
    }
  }
  return false;
}

function trimTableBoundary(value: string) {
  let trimmed = value.trim();
  if (trimmed.startsWith("|")) {
    trimmed = trimmed.slice(1);
  }
  if (trimmed.endsWith("|") && trimmed[trimmed.length - 2] !== "\\") {
    trimmed = trimmed.slice(0, -1);
  }
  return trimmed;
}

function normalizeTableCell(value: string) {
  return normalizeProseText(value.trim());
}

function normalizeTableCells(cells: string[], columnCount: number) {
  const normalized = cells.slice(0, columnCount).map(normalizeTableCell);
  while (normalized.length < columnCount) {
    normalized.push("");
  }
  return normalized;
}

function parseTableAlignment(value: string): MessageTableAlignment | null {
  const normalized = value.replace(/\s+/g, "");
  if (!/^:?-{3,}:?$/.test(normalized)) {
    return null;
  }
  if (normalized.startsWith(":") && normalized.endsWith(":")) {
    return "center";
  }
  if (normalized.endsWith(":")) {
    return "right";
  }
  return "left";
}

function normalizeTableAlignments(
  alignments: MessageTableAlignment[],
  columnCount: number,
) {
  const normalized = alignments.slice(0, columnCount);
  while (normalized.length < columnCount) {
    normalized.push("left");
  }
  return normalized;
}

function normalizeCodeFenceLanguage(value: string): string | undefined {
  const token = value
    .trim()
    .split(/\s+/)[0]
    ?.replace(/^language-/, "")
    .replace(/[{}]/g, "")
    .replace(/^\./, "")
    .trim();

  return token ? token.toLowerCase() : undefined;
}

function escapeRegex(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function normalizeProseText(value: string) {
  return value
    .replace(/[\u2018\u2019\u02BC]/g, "'")
    .replace(/[\u201C\u201D]/g, '"');
}

export function tokenizeInlineMessage(text: string): InlineMessagePart[] {
  const pattern =
    /(`[^`]+`|~~[^~]+~~|\*\*[^*]+\*\*|__[^_]+__|\*[^*\s][^*]*\*|\[[^\]]+\]\([^)]+\))/g;
  const parts: InlineMessagePart[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(text)) !== null) {
    if (match.index > lastIndex) {
      parts.push({ text: text.slice(lastIndex, match.index) });
    }
    const token = match[0];
    if (token.startsWith("`")) {
      parts.push({ kind: "code", text: token.slice(1, -1) });
    } else if (token.startsWith("~~")) {
      parts.push({ kind: "strike", text: token.slice(2, -2) });
    } else if (token.startsWith("**")) {
      parts.push({ kind: "bold", text: token.slice(2, -2) });
    } else if (token.startsWith("__")) {
      parts.push({ kind: "bold", text: token.slice(2, -2) });
    } else if (token.startsWith("*")) {
      parts.push({ kind: "italic", text: token.slice(1, -1) });
    } else {
      const link = parseMarkdownLinkToken(token);
      parts.push({
        kind: "link",
        text: link?.text || token,
        url: link?.url,
      });
    }
    lastIndex = match.index + token.length;
  }

  if (lastIndex < text.length) {
    parts.push({ text: text.slice(lastIndex) });
  }
  return parts;
}
