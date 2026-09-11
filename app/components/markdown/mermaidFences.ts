export type MarkdownMermaidSegment =
  | { type: "markdown"; text: string }
  | { type: "mermaid"; source: string; closed: boolean };

export function isMermaidFenceLanguage(language: string | undefined) {
  const value = language?.trim().toLowerCase();
  return value === "mermaid";
}

export function splitMarkdownMermaidSegments(
  value: string,
): MarkdownMermaidSegment[] {
  const lines = value.replace(/\r\n/g, "\n").split("\n");
  const segments: MarkdownMermaidSegment[] = [];
  let markdown: string[] = [];
  let fence: {
    marker: string;
    mermaid: boolean;
    lines: string[];
  } | null = null;

  const flushMarkdown = () => {
    if (markdown.length === 0) {
      return;
    }
    segments.push({ type: "markdown", text: markdown.join("\n") });
    markdown = [];
  };

  for (const rawLine of lines) {
    const trimmed = rawLine.trim();
    if (fence) {
      if (new RegExp(`^${escapeRegex(fence.marker)}\\s*$`).test(trimmed)) {
        if (fence.mermaid) {
          flushMarkdown();
          segments.push({
            type: "mermaid",
            source: fence.lines.join("\n").replace(/\n+$/, ""),
            closed: true,
          });
        } else {
          markdown.push(rawLine);
        }
        fence = null;
        continue;
      }
      if (fence.mermaid) {
        fence.lines.push(rawLine);
      } else {
        markdown.push(rawLine);
      }
      continue;
    }

    const open = /^(```|~~~)\s*(.*)$/.exec(trimmed);
    if (open) {
      const language = normalizeFenceLanguage(open[2]);
      if (isMermaidFenceLanguage(language)) {
        fence = { marker: open[1], mermaid: true, lines: [] };
      } else {
        fence = { marker: open[1], mermaid: false, lines: [] };
        markdown.push(rawLine);
      }
      continue;
    }

    markdown.push(rawLine);
  }

  if (fence?.mermaid) {
    flushMarkdown();
    segments.push({
      type: "mermaid",
      source: fence.lines.join("\n").replace(/\n+$/, ""),
      closed: false,
    });
  } else if (fence) {
    markdown.push("");
  }
  flushMarkdown();
  return segments.filter((segment) =>
    segment.type === "mermaid" ? true : segment.text.length > 0,
  );
}

export function markdownHasMermaidFence(value: string) {
  return splitMarkdownMermaidSegments(value).some(
    (segment) => segment.type === "mermaid",
  );
}

function normalizeFenceLanguage(value: string): string | undefined {
  const token = value
    .trim()
    .split(/\s+/)[0]
    ?.replace(/^language-/, "")
    .replace(/[{}]/g, "")
    .replace(/^\./, "")
    .trim();
  return token ? token.toLowerCase() : undefined;
}

function escapeRegex(value: string) {
  return value.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}
