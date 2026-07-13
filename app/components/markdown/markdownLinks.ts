export type MarkdownUrlOpener = (url: string) => Promise<unknown>;

export type MarkdownLinkPart = {
  text: string;
  url?: string;
};

export function parseMarkdownLinkToken(token: string) {
  const match = /^\[([^\]]+)\]\(\s*(\S+?)(?:\s+["'][^"']*["'])?\s*\)$/.exec(token);
  if (!match) {
    return null;
  }
  return { text: match[1], url: match[2] };
}

export function tokenizeMarkdownLinks(value: string): MarkdownLinkPart[] {
  const parts: MarkdownLinkPart[] = [];
  const pattern = /\[[^\]]+\]\([^)]+\)/g;
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(value)) !== null) {
    if (match.index > lastIndex) {
      parts.push({ text: value.slice(lastIndex, match.index) });
    }
    const link = parseMarkdownLinkToken(match[0]);
    parts.push(link || { text: match[0] });
    lastIndex = match.index + match[0].length;
  }

  if (lastIndex < value.length) {
    parts.push({ text: value.slice(lastIndex) });
  }
  return parts;
}

export function isSafeMarkdownUrl(value: string) {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

export async function openSafeMarkdownUrl(
  value: string,
  openUrl: MarkdownUrlOpener,
) {
  const url = value.trim();
  if (!isSafeMarkdownUrl(url)) {
    return false;
  }

  try {
    await openUrl(url);
    return true;
  } catch {
    return false;
  }
}
