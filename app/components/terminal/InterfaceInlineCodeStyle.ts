import type { TerminalThemePalette } from "../../constants/terminalThemes";

/** Must match Typography.chatMonoFont — kept local so bun tests avoid RN tokens. */
const INLINE_CODE_MONO_FONT = "MapleMono-CN-Regular";

/**
 * Shared Session/Brain inline-code treatment: mono + cyan, no fill/border.
 * Opaque per-fragment backgrounds tile when long spans wrap.
 */
export function interfaceInlineCodeStyle(
  theme: TerminalThemePalette,
  compact: boolean,
) {
  return {
    color: theme.cyan,
    backgroundColor: "transparent" as const,
    borderColor: "transparent" as const,
    fontFamily: INLINE_CODE_MONO_FONT,
    fontSize: compact ? 12 : 13,
    lineHeight: compact ? 18 : 20,
    letterSpacing: 0 as const,
  };
}
