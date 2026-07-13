import { parseMarkdownLinkToken } from "../markdown/markdownLinks";

export type MarkdownInlinePart = {
  text: string;
  kind?: "bold" | "code" | "link";
  url?: string;
};

export function tokenizeMarkdownInline(text: string): MarkdownInlinePart[] {
  const pattern = /(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\))/g;
  const parts: MarkdownInlinePart[] = [];
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
