import { describe, expect, test } from "bun:test";
import {
  buildSkillsMarkdownStyle,
  SKILLS_MARKDOWN_BODY_FONT_SIZE,
  SKILLS_MARKDOWN_HEADING_RAMP,
  SKILLS_MARKDOWN_MAX_HEADING_FONT_SIZE,
} from "./skillsMarkdownPresentation";
import type { MarkdownStyle } from "react-native-enriched-markdown";

const palette = {
  textPrimary: "#primary",
  textSecondary: "#secondary",
  textTertiary: "#tertiary",
  accent: "#accent",
  surfaceSubtle: "#subtle",
  borderSubtle: "#border",
};
const fonts = { body: "Body", bodyMedium: "BodyMedium", mono: "Mono" };

describe("Skills Markdown presentation", () => {
  test("heading ramp is restrained and monotonically stable by level", () => {
    const sizes = SKILLS_MARKDOWN_HEADING_RAMP.map((level) => level.fontSize);
    for (let index = 1; index < sizes.length; index += 1) {
      expect(sizes[index]!).toBeLessThanOrEqual(sizes[index - 1]!);
    }
    expect(sizes.every((size) => size <= SKILLS_MARKDOWN_MAX_HEADING_FONT_SIZE))
      .toBe(true);
    for (const level of SKILLS_MARKDOWN_HEADING_RAMP) {
      expect(level.lineHeight).toBeGreaterThan(level.fontSize);
      expect(level.marginTop).toBeGreaterThan(0);
      expect(level.marginBottom).toBeGreaterThan(0);
    }
  });

  test("headings never outweigh the inspector title chrome", () => {
    // The inspector title renders at 20pt (TypeScale.title); document
    // headings must stay below it so chrome outranks content.
    expect(SKILLS_MARKDOWN_MAX_HEADING_FONT_SIZE).toBeLessThan(20);
  });

  test("body copy, code, quotes, lists, and links keep one calm system", () => {
    const style: MarkdownStyle = buildSkillsMarkdownStyle(palette, fonts);
    expect(style.paragraph?.fontSize).toBe(SKILLS_MARKDOWN_BODY_FONT_SIZE);
    expect(style.paragraph?.fontFamily).toBe(fonts.body);
    expect(style.paragraph?.color).toBe(palette.textSecondary);

    for (const heading of [
      style.h1,
      style.h2,
      style.h3,
      style.h4,
      style.h5,
      style.h6,
    ]) {
      expect(heading?.fontFamily).toBe(fonts.bodyMedium);
      expect(heading?.color).toBe(palette.textPrimary);
    }

    expect(style.code?.fontFamily).toBe(fonts.mono);
    expect(style.code?.backgroundColor).toBe(palette.surfaceSubtle);
    expect(style.codeBlock?.fontFamily).toBe(fonts.mono);
    expect(style.codeBlock?.backgroundColor).toBe(palette.surfaceSubtle);
    expect(style.codeBlock?.borderColor).toBe(palette.borderSubtle);
    expect(style.blockquote?.borderColor).toBe(palette.borderSubtle);
    expect((style.blockquote?.borderWidth ?? 0)).toBeGreaterThan(0);
    expect(style.blockquote?.color).toBe(palette.textTertiary);
    expect(style.list?.markerColor).toBe(palette.textTertiary);
    expect(style.list?.color).toBe(palette.textSecondary);
    expect(style.link?.color).toBe(palette.accent);
    expect(style.link?.underline).toBe(false);
    expect(style.strong?.fontFamily).toBe(fonts.bodyMedium);
    expect(style.thematicBreak?.color).toBe(palette.borderSubtle);
    expect(style.table?.borderColor).toBe(palette.borderSubtle);
    expect(style.table?.headerBackgroundColor).toBe(palette.surfaceSubtle);
  });
});
