export type MessageBlock =
  | { type: "heading"; level: number; text: string }
  | { type: "paragraph"; text: string }
  | { type: "list"; items: MessageListItem[] }
  | { type: "code"; text: string; language?: string }
  | { type: "quote"; text: string };

export type MessageListItem = {
  marker: string;
  text: string;
};

export type InlineMessagePart = {
  text: string;
  kind?: "bold" | "code" | "link";
};

export function parseMessageBlocks(value: string): MessageBlock[] {
  const lines = value.replace(/<!--[\s\S]*?-->/g, "").replace(/\r\n/g, "\n").split("\n");
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

  for (const rawLine of lines) {
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

    const heading = /^(#{1,4})\s+(.+)$/.exec(trimmed);
    if (heading) {
      flushOpenBlocks();
      blocks.push({
        type: "heading",
        level: heading[1].length,
        text: normalizeProseText(heading[2]).trim(),
      });
      continue;
    }

    const listItem = /^((?:[-*+])|\d+[.)])\s+(.+)$/.exec(trimmed);
    if (listItem) {
      flushParagraph();
      flushQuote();
      list.push({
        marker: /^\d/.test(listItem[1]) ? listItem[1] : "\u2022",
        text: normalizeProseText(listItem[2]).trim(),
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

function normalizeCodeFenceLanguage(value: string): string | undefined {
  const token = value
    .trim()
    .split(/\s+/)[0]
    ?.replace(/^language-/, "")
    .replace(/[{}]/g, "")
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
  const pattern = /(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\))/g;
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
    } else if (token.startsWith("**")) {
      parts.push({ kind: "bold", text: token.slice(2, -2) });
    } else {
      const label = /^\[([^\]]+)\]/.exec(token)?.[1] || token;
      parts.push({ kind: "link", text: label });
    }
    lastIndex = match.index + token.length;
  }

  if (lastIndex < text.length) {
    parts.push({ text: text.slice(lastIndex) });
  }
  return parts;
}
