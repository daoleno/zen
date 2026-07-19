import remend, { type RemendOptions } from "remend";

/** Shared Android+iOS product path. Fallback is error-boundary only, not a platform fork. */
export const INTERFACE_MARKDOWN_MOBILE_RENDERER = "enriched" as const;

const STREAMING_REMEND_OPTIONS: RemendOptions = {
  images: true,
  inlineKatex: false,
  linkMode: "text-only",
};

export function prepareInterfaceMarkdown(value: string, streaming: boolean) {
  let markdown = value
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\r\n/g, "\n")
    .trim();
  if (!markdown) {
    return "";
  }
  if (streaming) {
    markdown = remend(markdown, STREAMING_REMEND_OPTIONS);
  }
  return stripMarkdownImages(markdown);
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
