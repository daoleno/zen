import type { MarkdownStyle } from "react-native-enriched-markdown";

/**
 * Presentation truth for Skill file previews rendered as Markdown.
 *
 * The Skills inspector shows provider-authored SKILL.md documents inside a
 * mobile sheet. Their headings must never compete with the inspector chrome:
 * the whole ramp stays within a few points of body copy and decreases
 * monotonically by level, so document structure reads as stable hierarchy
 * instead of oversized display type.
 */
export const SKILLS_MARKDOWN_BODY_FONT_SIZE = 15;
export const SKILLS_MARKDOWN_MAX_HEADING_FONT_SIZE = 19;

export interface SkillsMarkdownPalette {
  textPrimary: string;
  textSecondary: string;
  textTertiary: string;
  accent: string;
  surfaceSubtle: string;
  borderSubtle: string;
}

export interface SkillsMarkdownFonts {
  body: string;
  bodyMedium: string;
  mono: string;
}

interface HeadingRampEntry {
  fontSize: number;
  lineHeight: number;
  marginTop: number;
  marginBottom: number;
}

/**
 * One entry per heading level. Sizes decrease monotonically and every level
 * keeps an explicit line height above its font size.
 */
export const SKILLS_MARKDOWN_HEADING_RAMP: readonly HeadingRampEntry[] = [
  { fontSize: 19, lineHeight: 27, marginTop: 18, marginBottom: 7 },
  { fontSize: 17, lineHeight: 25, marginTop: 16, marginBottom: 6 },
  { fontSize: 16, lineHeight: 24, marginTop: 14, marginBottom: 5 },
  { fontSize: 15, lineHeight: 23, marginTop: 12, marginBottom: 4 },
  { fontSize: 15, lineHeight: 23, marginTop: 12, marginBottom: 4 },
  { fontSize: 14, lineHeight: 21, marginTop: 12, marginBottom: 4 },
];

export function buildSkillsMarkdownStyle(
  palette: SkillsMarkdownPalette,
  fonts: SkillsMarkdownFonts,
): MarkdownStyle {
  const heading = (index: number) => {
    const level = SKILLS_MARKDOWN_HEADING_RAMP[index]!;
    return {
      color: palette.textPrimary,
      fontFamily: fonts.bodyMedium,
      fontWeight: "500",
      fontSize: level.fontSize,
      lineHeight: level.lineHeight,
      marginTop: level.marginTop,
      marginBottom: level.marginBottom,
    };
  };
  return {
    paragraph: {
      color: palette.textSecondary,
      fontFamily: fonts.body,
      fontSize: SKILLS_MARKDOWN_BODY_FONT_SIZE,
      lineHeight: 23,
      marginTop: 0,
      marginBottom: 10,
    },
    h1: heading(0),
    h2: heading(1),
    h3: heading(2),
    h4: heading(3),
    h5: heading(4),
    h6: heading(5),
    blockquote: {
      color: palette.textTertiary,
      fontFamily: fonts.body,
      fontSize: SKILLS_MARKDOWN_BODY_FONT_SIZE,
      lineHeight: 22,
      borderColor: palette.borderSubtle,
      borderWidth: 2,
      backgroundColor: "transparent",
    },
    list: {
      color: palette.textSecondary,
      fontFamily: fonts.body,
      fontSize: SKILLS_MARKDOWN_BODY_FONT_SIZE,
      lineHeight: 23,
      bulletColor: palette.textTertiary,
      markerColor: palette.textTertiary,
    },
    link: {
      color: palette.accent,
      underline: false,
    },
    strong: {
      fontFamily: fonts.bodyMedium,
      fontWeight: "normal",
      color: palette.textPrimary,
    },
    code: {
      fontFamily: fonts.mono,
      fontSize: 13,
      color: palette.textPrimary,
      backgroundColor: palette.surfaceSubtle,
    },
    codeBlock: {
      color: palette.textPrimary,
      fontFamily: fonts.mono,
      fontSize: 13,
      lineHeight: 20,
      backgroundColor: palette.surfaceSubtle,
      borderColor: palette.borderSubtle,
      borderWidth: 1,
      borderRadius: 6,
      padding: 10,
    },
    thematicBreak: {
      color: palette.borderSubtle,
      height: 1,
      marginTop: 14,
      marginBottom: 14,
    },
    table: {
      color: palette.textSecondary,
      fontFamily: fonts.body,
      fontSize: 13,
      lineHeight: 19,
      borderColor: palette.borderSubtle,
      borderWidth: 1,
      headerFontFamily: fonts.bodyMedium,
      headerBackgroundColor: palette.surfaceSubtle,
      headerTextColor: palette.textPrimary,
    },
    taskList: {
      checkedColor: palette.accent,
      checkmarkColor: palette.accent,
      borderColor: palette.borderSubtle,
    },
  };
}
