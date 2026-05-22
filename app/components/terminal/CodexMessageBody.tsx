import React, {
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import { Linking, StyleSheet, Text, View } from "react-native";
import {
  EnrichedMarkdownText,
  type LinkPressEvent,
  type MarkdownStyle,
} from "react-native-enriched-markdown";
import remend, { type RemendOptions } from "remend";
import { Typography } from "../../constants/tokens";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";

const USE_NATIVE_MARKDOWN_BODY = true;
const STREAMING_REMEND_OPTIONS: RemendOptions = {
  images: true,
  inlineKatex: false,
  linkMode: "text-only",
};

export const TimelineTextSelectableContext = React.createContext(true);

type MarkdownErrorBoundaryProps = {
  fallback: React.ReactNode;
  children: React.ReactNode;
  resetKey: string;
};

type MarkdownErrorBoundaryState = {
  failed: boolean;
};

class MarkdownErrorBoundary extends React.Component<
  MarkdownErrorBoundaryProps,
  MarkdownErrorBoundaryState
> {
  state: MarkdownErrorBoundaryState = { failed: false };

  static getDerivedStateFromError() {
    return { failed: true };
  }

  componentDidUpdate(previousProps: MarkdownErrorBoundaryProps) {
    if (previousProps.resetKey !== this.props.resetKey && this.state.failed) {
      this.setState({ failed: false });
    }
  }

  componentDidCatch(error: unknown) {
    console.warn("[codex] native markdown renderer failed", error);
  }

  render() {
    if (this.state.failed) {
      return this.props.fallback;
    }
    return this.props.children;
  }
}

type MessageBlock =
  | { type: "heading"; level: number; text: string }
  | { type: "paragraph"; text: string }
  | { type: "list"; items: string[] }
  | { type: "code"; text: string }
  | { type: "quote"; text: string };

type InlinePart = {
  text: string;
  kind?: "bold" | "code" | "link";
};

export function MessageBody({
  value,
  chrome,
  theme,
  compact = false,
}: {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
}) {
  const textSelectable = useContext(TimelineTextSelectableContext);
  const blocks = useMemo(() => parseMessageBlocks(value), [value]);
  if (blocks.length === 0) {
    return null;
  }
  return (
    <View style={styles.messageBody}>
      {blocks.map((block, index) => {
        const isLast = index === blocks.length - 1;
        switch (block.type) {
          case "heading":
            return (
              <Text
                key={index}
                selectable={textSelectable}
                style={[
                  styles.messageHeading,
                  block.level <= 2 ? styles.messageHeadingLarge : null,
                  { color: chrome.text },
                  isLast ? styles.messageBlockLast : null,
                ]}
              >
                {renderInlineMessage(block.text, chrome, theme)}
              </Text>
            );
          case "list":
            return (
              <View
                key={index}
                style={[styles.messageList, isLast ? styles.messageBlockLast : null]}
              >
                {block.items.map((item, itemIndex) => (
                  <View key={itemIndex} style={styles.messageListItem}>
                    <Text
                      selectable={textSelectable}
                      style={[styles.messageBullet, { color: chrome.textSubtle }]}
                    >
                      {"\u2022"}
                    </Text>
                    <Text
                      selectable={textSelectable}
                      style={[
                        styles.messageText,
                        styles.messageBlockLast,
                        { color: chrome.text },
                      ]}
                    >
                      {renderInlineMessage(item, chrome, theme)}
                    </Text>
                  </View>
                ))}
              </View>
            );
          case "code":
            return (
              <Text
                key={index}
                selectable={textSelectable}
                style={[
                  styles.messageCodeBlock,
                  {
                    color: chrome.text,
                    backgroundColor: compact ? chrome.surface : theme.black,
                    borderColor: chrome.border,
                  },
                  isLast ? styles.messageBlockLast : null,
                ]}
              >
                {block.text}
              </Text>
            );
          case "quote":
            return (
              <View
                key={index}
                style={[
                  styles.messageQuote,
                  { borderLeftColor: chrome.borderStrong },
                  isLast ? styles.messageBlockLast : null,
                ]}
              >
                <Text
                  selectable={textSelectable}
                  style={[styles.messageQuoteText, { color: chrome.textMuted }]}
                >
                  {renderInlineMessage(block.text, chrome, theme)}
                </Text>
              </View>
            );
          case "paragraph":
          default:
            return (
              <Text
                key={index}
                selectable={textSelectable}
                style={[
                  styles.messageText,
                  { color: chrome.text },
                  isLast ? styles.messageBlockLast : null,
                ]}
              >
                {renderInlineMessage(block.text, chrome, theme)}
              </Text>
            );
        }
      })}
    </View>
  );
}

export function StreamingMessageBody({
  value,
  chrome,
  theme,
  stream,
}: {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  stream: boolean;
}) {
  const [visibleChars, setVisibleChars] = useState(stream ? 0 : value.length);

  useEffect(() => {
    if (!stream) {
      setVisibleChars(value.length);
      return;
    }
    setVisibleChars((current) => Math.min(current, value.length));
  }, [stream, value.length]);

  useEffect(() => {
    if (!stream || visibleChars >= value.length) {
      return;
    }
    const timer = setTimeout(() => {
      setVisibleChars((current) => Math.min(value.length, current + 18));
    }, 24);
    return () => clearTimeout(timer);
  }, [stream, value.length, visibleChars]);

  const renderedValue = stream ? value.slice(0, visibleChars) : value;
  return (
    <View style={styles.zenAssistantContent}>
      <CodexMarkdownBody
        value={renderedValue}
        chrome={chrome}
        theme={theme}
        streaming={stream && visibleChars < value.length}
      />
      {stream && visibleChars < value.length ? (
        <View style={[styles.zenStreamCursor, { backgroundColor: chrome.accent }]} />
      ) : null}
    </View>
  );
}

function CodexMarkdownBody({
  value,
  chrome,
  theme,
  compact = false,
  streaming = false,
}: {
  value: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  compact?: boolean;
  streaming?: boolean;
}) {
  const textSelectable = useContext(TimelineTextSelectableContext);
  const markdown = useMemo(() => prepareCodexMarkdown(value, streaming), [streaming, value]);
  const markdownStyle = useMemo(
    () => codexMarkdownStyle(chrome, theme, compact),
    [chrome, compact, theme],
  );
  const fallback = (
    <MessageBody value={markdown || value} chrome={chrome} theme={theme} compact={compact} />
  );
  const handleLinkPress = useCallback((event: LinkPressEvent) => {
    const url = event.url.trim();
    if (!isSafeMarkdownUrl(url)) {
      return;
    }
    void Linking.openURL(url).catch(() => undefined);
  }, []);

  if (!USE_NATIVE_MARKDOWN_BODY || !markdown) {
    return fallback;
  }

  return (
    <MarkdownErrorBoundary fallback={fallback} resetKey={markdown}>
      <EnrichedMarkdownText
        markdown={markdown}
        markdownStyle={markdownStyle}
        containerStyle={styles.messageBody}
        flavor="github"
        selectable={textSelectable}
        allowFontScaling={false}
        allowTrailingMargin={false}
        enableLinkPreview={false}
        md4cFlags={{ latexMath: false, underline: false }}
        onLinkPress={handleLinkPress}
        streamingAnimation={streaming}
        spoilerOverlay="solid"
      />
    </MarkdownErrorBoundary>
  );
}

function parseMessageBlocks(value: string): MessageBlock[] {
  const lines = value.replace(/<!--[\s\S]*?-->/g, "").replace(/\r\n/g, "\n").split("\n");
  const blocks: MessageBlock[] = [];
  let paragraph: string[] = [];
  let list: string[] = [];
  let quote: string[] = [];
  let code: string[] | null = null;

  const flushParagraph = () => {
    const text = paragraph.join(" ").trim();
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
    const text = quote.join(" ").trim();
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
      if (/^```/.test(trimmed)) {
        blocks.push({ type: "code", text: code.join("\n").replace(/\n+$/, "") });
        code = null;
      } else {
        code.push(rawLine);
      }
      continue;
    }

    if (/^```/.test(trimmed)) {
      flushOpenBlocks();
      code = [];
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
        text: heading[2].trim(),
      });
      continue;
    }

    const listItem = /^(?:[-*]|\d+\.)\s+(.+)$/.exec(trimmed);
    if (listItem) {
      flushParagraph();
      flushQuote();
      list.push(listItem[1].trim());
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
    blocks.push({ type: "code", text: code.join("\n").replace(/\n+$/, "") });
  }
  flushOpenBlocks();
  return blocks;
}

function renderInlineMessage(
  text: string,
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
) {
  return tokenizeInlineMessage(text).map((part, index) => {
    if (part.kind === "bold") {
      return (
        <Text key={index} style={[styles.messageBold, { color: chrome.text }]}>
          {part.text}
        </Text>
      );
    }
    if (part.kind === "code") {
      return (
        <Text
          key={index}
          style={[
            styles.messageInlineCode,
            { color: theme.green, backgroundColor: chrome.surfaceMuted },
          ]}
        >
          {part.text}
        </Text>
      );
    }
    if (part.kind === "link") {
      return (
        <Text key={index} style={[styles.messageLink, { color: chrome.accent }]}>
          {part.text}
        </Text>
      );
    }
    return part.text;
  });
}

function prepareCodexMarkdown(value: string, streaming: boolean) {
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
  return value.replace(/!\[([^\]]*)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g, (_match, alt, url) => {
    const label = String(alt || "").trim();
    const href = String(url || "").trim();
    if (!href) {
      return label;
    }
    return label ? `[${label}](${href})` : href;
  });
}

function isSafeMarkdownUrl(value: string) {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

function codexMarkdownStyle(
  chrome: TerminalThemeChrome,
  theme: TerminalThemePalette,
  compact: boolean,
): MarkdownStyle {
  const text = {
    color: chrome.text,
    fontFamily: Typography.uiFont,
    fontSize: 14,
    lineHeight: 20,
    marginTop: 0,
    marginBottom: compact ? 6 : 7,
  };
  const heading = {
    color: chrome.text,
    fontFamily: Typography.uiFontMedium,
    lineHeight: 20,
    marginTop: 0,
    marginBottom: 7,
  };
  return {
    paragraph: text,
    h1: { ...heading, fontSize: 16, lineHeight: 22 },
    h2: { ...heading, fontSize: 15, lineHeight: 21 },
    h3: { ...heading, fontSize: 14, lineHeight: 20 },
    h4: { ...heading, fontSize: 14, lineHeight: 20 },
    h5: { ...heading, fontSize: 13, lineHeight: 19 },
    h6: { ...heading, fontSize: 13, lineHeight: 19, color: chrome.textMuted },
    strong: {
      color: chrome.text,
      fontFamily: Typography.uiFontMedium,
      fontWeight: "normal",
    },
    em: {
      color: chrome.text,
      fontFamily: Typography.uiFont,
      fontStyle: "italic",
    },
    link: {
      color: chrome.accent,
      fontFamily: Typography.uiFontMedium,
      underline: false,
    },
    code: {
      color: theme.green,
      backgroundColor: chrome.surfaceMuted,
      borderColor: chrome.border,
      fontFamily: Typography.terminalFont,
      fontSize: 13,
    },
    codeBlock: {
      color: chrome.text,
      backgroundColor: compact ? chrome.surface : theme.black,
      borderColor: chrome.border,
      borderRadius: 7,
      borderWidth: StyleSheet.hairlineWidth,
      fontFamily: Typography.terminalFont,
      fontSize: 12,
      lineHeight: 17,
      marginTop: 2,
      marginBottom: 9,
      padding: 10,
    },
    blockquote: {
      color: chrome.textMuted,
      backgroundColor: "transparent",
      borderColor: chrome.borderStrong,
      borderWidth: 2,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      gapWidth: 9,
      lineHeight: 19,
      marginTop: 0,
      marginBottom: 8,
    },
    list: {
      color: chrome.text,
      bulletColor: chrome.textSubtle,
      markerColor: chrome.textSubtle,
      markerFontWeight: "normal",
      fontFamily: Typography.uiFont,
      fontSize: 14,
      gapWidth: 7,
      lineHeight: 20,
      marginLeft: 0,
      marginTop: 0,
      marginBottom: 8,
    },
    table: {
      color: chrome.text,
      borderColor: chrome.border,
      borderRadius: 7,
      borderWidth: StyleSheet.hairlineWidth,
      cellPaddingHorizontal: 8,
      cellPaddingVertical: 6,
      fontFamily: Typography.uiFont,
      fontSize: 12,
      headerBackgroundColor: chrome.surfaceMuted,
      headerFontFamily: Typography.uiFontMedium,
      headerTextColor: chrome.text,
      lineHeight: 17,
      marginTop: 2,
      marginBottom: 9,
      rowEvenBackgroundColor: chrome.surface,
      rowOddBackgroundColor: chrome.surfaceMuted,
    },
    taskList: {
      borderColor: chrome.borderStrong,
      checkboxBorderRadius: 4,
      checkboxSize: 15,
      checkedColor: theme.green,
      checkedStrikethrough: true,
      checkedTextColor: chrome.textMuted,
      checkmarkColor: theme.background,
    },
    thematicBreak: {
      color: chrome.border,
      height: StyleSheet.hairlineWidth,
      marginTop: 8,
      marginBottom: 10,
    },
    math: {
      color: chrome.text,
      backgroundColor: chrome.surfaceMuted,
      fontSize: 13,
      marginTop: 4,
      marginBottom: 8,
      padding: 8,
      textAlign: "left",
    },
    inlineMath: {
      color: theme.cyan,
    },
    spoiler: {
      color: chrome.surfaceMuted,
      solid: { borderRadius: 4 },
    },
  };
}

function tokenizeInlineMessage(text: string): InlinePart[] {
  const pattern = /(`[^`]+`|\*\*[^*]+\*\*|\[[^\]]+\]\([^)]+\))/g;
  const parts: InlinePart[] = [];
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

const styles = StyleSheet.create({
  zenAssistantContent: {
    minWidth: 0,
  },
  zenStreamCursor: {
    width: 6,
    height: 16,
    borderRadius: 3,
    opacity: 0.65,
  },
  messageBody: {
    minWidth: 0,
  },
  messageText: {
    marginBottom: 7,
    fontSize: 14,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  messageHeading: {
    marginBottom: 7,
    fontSize: 14,
    lineHeight: 19,
    fontFamily: Typography.uiFontMedium,
  },
  messageHeadingLarge: {
    fontSize: 15,
    lineHeight: 20,
  },
  messageList: {
    marginBottom: 8,
    gap: 4,
  },
  messageListItem: {
    flexDirection: "row",
    alignItems: "flex-start",
    gap: 7,
  },
  messageBullet: {
    width: 9,
    fontSize: 13,
    lineHeight: 20,
    fontFamily: Typography.uiFont,
  },
  messageCodeBlock: {
    marginTop: 2,
    marginBottom: 9,
    borderRadius: 7,
    borderWidth: StyleSheet.hairlineWidth,
    overflow: "hidden",
    paddingHorizontal: 10,
    paddingVertical: 8,
    fontSize: 12,
    lineHeight: 17,
    fontFamily: Typography.terminalFont,
  },
  messageQuote: {
    marginBottom: 8,
    borderLeftWidth: 2,
    paddingLeft: 9,
  },
  messageQuoteText: {
    fontSize: 13,
    lineHeight: 19,
    fontFamily: Typography.uiFont,
  },
  messageBold: {
    fontFamily: Typography.uiFontMedium,
  },
  messageInlineCode: {
    fontFamily: Typography.terminalFont,
    fontSize: 13,
    lineHeight: 18,
  },
  messageLink: {
    fontFamily: Typography.uiFontMedium,
  },
  messageBlockLast: {
    marginBottom: 0,
  },
});
