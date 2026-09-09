const FRONTMATTER_PATTERN = /^---\r?\n[\s\S]*?\r?\n---\r?\n/;
const INIT_DIRECTIVE_PATTERN = /%%\{[\s\S]*?\}%%/g;
const CLICK_LINE_PATTERN = /^\s*click\s+\S+.*$/gim;
const HTML_TAG_PATTERN = /<\/?([a-zA-Z][\w:-]*)\b[^>]*>/g;
const ALLOWED_HTML_TAGS = new Set(["br"]);
const UNSAFE_URL_PATTERN = /(?:javascript|data|vbscript|file):/i;
const CALLBACK_PATTERN = /\b(?:call\s+[A-Za-z_]|bindFunctions|window\.|document\.|eval\s*\()/i;

export function stripMermaidOverrides(source: string) {
  return source
    .replace(/\r\n/g, "\n")
    .replace(FRONTMATTER_PATTERN, "")
    .replace(INIT_DIRECTIVE_PATTERN, "")
    .replace(CLICK_LINE_PATTERN, "")
    .trim();
}

export function sanitizeMermaidLabelHtml(source: string) {
  return source.replace(HTML_TAG_PATTERN, (full, tag: string) => {
    if (ALLOWED_HTML_TAGS.has(tag.toLowerCase())) {
      return "<br/>";
    }
    return "";
  });
}

export function mermaidSourceLooksUnsafe(source: string) {
  return UNSAFE_URL_PATTERN.test(source) || CALLBACK_PATTERN.test(source);
}

export function prepareTrustedMermaidSource(raw: string) {
  const stripped = stripMermaidOverrides(raw);
  const sanitized = sanitizeMermaidLabelHtml(stripped);
  return sanitized.replace(/\n{3,}/g, "\n\n").trim();
}

export function sanitizeMermaidSvg(svg: string) {
  return svg
    .replace(/<script\b[\s\S]*?<\/script>/gi, "")
    .replace(/\son[a-z]+\s*=\s*(['"]).*?\1/gi, "")
    .replace(/\s(?:href|xlink:href)\s*=\s*(['"])\s*(?:javascript|data|vbscript):[\s\S]*?\1/gi, "")
    .replace(/<foreignObject\b[\s\S]*?<\/foreignObject>/gi, (block) =>
      block.replace(/<script\b[\s\S]*?<\/script>/gi, ""),
    );
}
