import React, { useMemo } from "react";
import { StyleSheet, Text, View } from "react-native";
import { Colors, Spacing, Typography, useAppColors } from "../../constants/tokens";

type Block =
  | { type: "heading"; level: number; text: string }
  | { type: "paragraph"; text: string }
  | { type: "list"; items: string[] }
  | { type: "code"; text: string }
  | { type: "quote"; text: string }
  | { type: "rule" };

type InlinePart = {
  text: string;
  kind?: "bold" | "code" | "link";
};

export function MarkdownView({ value }: { value: string }) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const blocks = useMemo(() => parseMarkdown(value), [value]);

  return (
    <View style={styles.root}>
      {blocks.map((block, index) => {
        switch (block.type) {
          case "heading":
            return (
              <Text
                key={index}
                selectable
                style={[
                  styles.heading,
                  block.level <= 1
                    ? styles.headingOne
                    : block.level === 2
                      ? styles.headingTwo
                      : block.level === 3
                        ? styles.headingThree
                        : styles.headingFour,
                ]}
              >
                {renderInline(block.text, styles)}
              </Text>
            );
          case "list":
            return (
              <View key={index} style={styles.list}>
                {block.items.map((item, itemIndex) => (
                  <View key={itemIndex} style={styles.listItem}>
                    <Text selectable style={styles.bullet}>
                      •
                    </Text>
                    <Text selectable style={styles.listBody}>
                      {renderInline(item, styles)}
                    </Text>
                  </View>
                ))}
              </View>
            );
          case "code":
            return (
              <Text key={index} selectable style={styles.codeBlock}>
                {block.text}
              </Text>
            );
          case "quote":
            return (
              <View key={index} style={styles.quote}>
                <Text selectable style={styles.quoteText}>
                  {renderInline(block.text, styles)}
                </Text>
              </View>
            );
          case "rule":
            return <View key={index} style={styles.rule} />;
          case "paragraph":
          default:
            return (
              <Text key={index} selectable style={styles.body}>
                {renderInline(block.text, styles)}
              </Text>
            );
        }
      })}
    </View>
  );
}

function parseMarkdown(value: string): Block[] {
  const withoutComments = value.replace(/<!--[\s\S]*?-->/g, "");
  const lines = withoutComments.replace(/\r\n/g, "\n").split("\n");
  const blocks: Block[] = [];
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

    const heading = /^(#{1,6})\s+(.+)$/.exec(trimmed);
    if (heading) {
      flushOpenBlocks();
      blocks.push({
        type: "heading",
        level: heading[1].length,
        text: heading[2].trim(),
      });
      continue;
    }

    if (/^(-{3,}|\*{3,})$/.test(trimmed)) {
      flushOpenBlocks();
      blocks.push({ type: "rule" });
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

function renderInline(text: string, styles: ReturnType<typeof createStyles>) {
  return tokenizeInline(text).map((part, index) => {
    if (part.kind === "bold") {
      return (
        <Text key={index} style={styles.bold}>
          {part.text}
        </Text>
      );
    }
    if (part.kind === "code") {
      return (
        <Text key={index} style={styles.inlineCode}>
          {part.text}
        </Text>
      );
    }
    if (part.kind === "link") {
      return (
        <Text key={index} style={styles.link}>
          {part.text}
        </Text>
      );
    }
    return part.text;
  });
}

function tokenizeInline(text: string): InlinePart[] {
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

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    root: {
      paddingHorizontal: 18,
      paddingTop: 10,
      paddingBottom: 26,
    },
    heading: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      letterSpacing: 0,
    },
    headingOne: {
      marginBottom: 14,
      fontSize: 22,
      lineHeight: 28,
    },
    headingTwo: {
      marginTop: 20,
      marginBottom: 12,
      paddingBottom: 7,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
      fontSize: 17,
      lineHeight: 23,
    },
    headingThree: {
      marginTop: 14,
      marginBottom: 6,
      fontSize: 15,
      lineHeight: 20,
    },
    headingFour: {
      marginTop: 10,
      marginBottom: 8,
      color: colors.textSecondary,
      fontSize: 12,
      lineHeight: 16,
      textTransform: "uppercase",
      opacity: 0.76,
    },
    body: {
      marginBottom: 8,
      color: colors.textPrimary,
      fontFamily: Typography.uiFont,
      fontSize: 14,
      lineHeight: 21,
      opacity: 0.88,
    },
    bold: {
      fontFamily: Typography.uiFontMedium,
      color: colors.textPrimary,
    },
    inlineCode: {
      color: colors.promptGreen,
      fontFamily: Typography.terminalFont,
      fontSize: 13,
    },
    link: {
      color: colors.accent,
      fontFamily: Typography.uiFontMedium,
    },
    list: {
      marginBottom: 10,
      gap: 5,
    },
    listItem: {
      flexDirection: "row",
      alignItems: "flex-start",
      gap: 9,
    },
    bullet: {
      width: 10,
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 14,
      lineHeight: 21,
      opacity: 0.7,
    },
    listBody: {
      flex: 1,
      color: colors.textPrimary,
      fontFamily: Typography.uiFont,
      fontSize: 14,
      lineHeight: 21,
      opacity: 0.88,
    },
    codeBlock: {
      marginTop: 5,
      marginBottom: 12,
      paddingHorizontal: 12,
      paddingVertical: 10,
      borderRadius: 8,
      overflow: "hidden",
      color: colors.textPrimary,
      backgroundColor: colors.bgElevated,
      fontFamily: Typography.terminalFont,
      fontSize: 12,
      lineHeight: 18,
    },
    quote: {
      marginBottom: 12,
      paddingLeft: 12,
      borderLeftWidth: 2,
      borderLeftColor: colors.border,
    },
    quoteText: {
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 14,
      lineHeight: 21,
    },
    rule: {
      height: StyleSheet.hairlineWidth,
      backgroundColor: colors.borderSubtle,
      marginVertical: Spacing.md,
    },
  });
}
