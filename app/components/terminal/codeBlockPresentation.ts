const PLAIN_CODE_LANGUAGES = new Set([
  "plain",
  "text",
  "txt",
  "ascii",
  "art",
  "plaintext",
]);

const ASCII_ART_SYMBOLS = /[.|/_\\*~^'()\-+:=<>[\]{}`"!@#%&]/g;

export function isPlainCodeFenceLanguage(language: string | undefined): boolean {
  const normalized = language?.trim().toLowerCase();
  return !normalized || PLAIN_CODE_LANGUAGES.has(normalized);
}

export function looksLikeAsciiArt(text: string): boolean {
  const lines = text.replace(/\r\n/g, "\n").split("\n");
  const meaningfulLines = lines.filter((line) => line.trim().length > 0);
  if (meaningfulLines.length < 3) {
    return false;
  }

  let artLikeLines = 0;
  for (const line of meaningfulLines) {
    const symbolMatches = line.match(ASCII_ART_SYMBOLS)?.length ?? 0;
    const hasLeadingSpaces = /^\s+\S/.test(line);
    const mostlySymbols =
      symbolMatches >= 3 &&
      symbolMatches / Math.max(line.trim().length, 1) >= 0.2;
    const spacedDrawing = hasLeadingSpaces && symbolMatches >= 2;
    if (mostlySymbols || spacedDrawing) {
      artLikeLines += 1;
    }
  }

  return artLikeLines >= Math.max(3, Math.ceil(meaningfulLines.length * 0.55));
}

export function shouldRenderPlainMonospaceCodeBlock(
  text: string,
  language: string | undefined,
): boolean {
  if (isPlainCodeFenceLanguage(language)) {
    return true;
  }
  return looksLikeAsciiArt(text);
}