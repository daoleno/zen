import { Lexer, type Token } from "marked";

export type MarkdownImageSegment = { type: "markdown"; text: string } | { type: "image"; path: string; alt: string; title?: string };

/** Positions come from Markdown tokens, so code, escaped syntax and plain URLs stay text. */
export function splitMarkdownImages(markdown: string): MarkdownImageSegment[] {
  const images: { start: number; end: number; path: string; alt: string; title?: string }[] = [];
  function visit(tokens: Token[], parent: string, base: number) {
    let cursor = 0;
    for (const token of tokens) {
      const offset = parent.indexOf(token.raw, cursor);
      if (offset < 0 || !token.raw) continue;
      cursor = offset + token.raw.length;
      const start = base + offset;
      if (token.type === "image") {
        images.push({ start, end: start + token.raw.length, path: token.href, alt: token.text, title: token.title || undefined });
      } else if (token.type === "list") {
        visit(token.items, token.raw, start);
      } else if ("tokens" in token && Array.isArray(token.tokens)) {
        visit(token.tokens, token.raw, start);
      } else if (token.type === "table") {
        const cells = [...token.header, ...token.rows.flat()];
        visit(cells.flatMap((cell) => cell.tokens), token.raw, start);
      }
    }
  }
  visit(Lexer.lex(markdown), markdown, 0);
  const result: MarkdownImageSegment[] = [];
  let cursor = 0;
  for (const image of images.sort((left, right) => left.start - right.start)) {
    if (image.start < cursor) continue;
    if (image.start > cursor) result.push({ type: "markdown", text: markdown.slice(cursor, image.start) });
    result.push({ type: "image", path: image.path, alt: image.alt, title: image.title });
    cursor = image.end;
  }
  if (cursor < markdown.length) result.push({ type: "markdown", text: markdown.slice(cursor) });
  return result;
}
