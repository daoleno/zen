import { Ionicons } from "@expo/vector-icons";
import React from "react";
import {
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import { ContinuousCorners, Typography } from "../../constants/tokens";
import { useElapsedDurationLabels } from "./useElapsedDurationLabel";
import { ComposerLoadingDots } from "./ComposerLoadingDots";
import { COMPOSER_ACTION_HORIZONTAL_PADDING } from "./composerActionSlot";
import {
  COMPOSER_CONTROL_DISC_SIZE,
  composerNeutralFill,
} from "./composerMaterial";

type IoniconName = React.ComponentProps<typeof Ionicons>["name"];

interface ComposerSendButtonProps {
  icon: IoniconName;
  accessibilityLabel: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  enabled: boolean;
  loading: boolean;
  running: boolean;
  elapsedStartedAt?: string;
  variant?: "default" | "chatgpt";
  fixedWidth?: number;
  onPress(): void;
}

/** Elapsed text stays legible without outgrowing the fixed trailing slot. */
const ELAPSED_LABEL_MAX_FONT_SCALE = 1.3;
/** (44 pt target - 34 pt disc) / 2: same inset the leading Plus disc gets. */
const DISC_TRAILING_INSET = 5;

/**
 * Trailing Composer action. The whole 44 pt slot is the touch target; the
 * visible control is a 34 pt disc inside it:
 * - ready: filled accent disc, arrow in textOnAccent;
 * - disabled: quiet neutral disc, subtle glyph;
 * - sending: neutral disc with the loading mark;
 * - running: neutral disc with a stop square, widening into a capsule when
 *   the elapsed label is shown (the label is also spoken in the a11y label).
 */
export function ComposerSendButton({
  icon,
  accessibilityLabel,
  chrome,
  enabled,
  loading,
  running,
  elapsedStartedAt,
  variant = "default",
  fixedWidth,
  onPress,
}: ComposerSendButtonProps) {
  const elapsedLabels = useElapsedDurationLabels(
    elapsedStartedAt,
    Boolean(elapsedStartedAt),
  );
  const elapsedLabel = running ? elapsedLabels.visual : "";
  const standalone = variant === "chatgpt";
  const animated = loading || running;
  const ready = enabled && !loading && !running;
  const foreground = running
    ? chrome.text
    : ready
      ? chrome.textOnAccent
      : chrome.textSubtle;
  const discColor = ready
    ? chrome.accent
    : running
      ? composerNeutralFill(chrome, "strong")
      : composerNeutralFill(chrome);
  const borderColor = standalone
    ? running
      ? chrome.border
      : ready
        ? chrome.accent
        : "transparent"
    : "transparent";

  const content = loading ? (
    <ComposerLoadingDots color={chrome.textSubtle} size={7} />
  ) : running ? (
    <View style={styles.runningContent}>
      <View style={[styles.stopGlyph, { backgroundColor: foreground }]} />
      {elapsedLabel ? (
        <Text
          numberOfLines={1}
          maxFontSizeMultiplier={ELAPSED_LABEL_MAX_FONT_SCALE}
          style={[styles.elapsedLabel, { color: chrome.text }]}
        >
          {elapsedLabel}
        </Text>
      ) : null}
    </View>
  ) : (
    <Ionicons name={icon} size={18} color={foreground} />
  );

  return (
    <TouchableOpacity
      accessibilityLabel={
        running && elapsedLabels.accessibility
          ? `${accessibilityLabel}, ${elapsedLabels.accessibility}`
          : accessibilityLabel
      }
      accessibilityRole="button"
      accessibilityState={{ disabled: !enabled, busy: animated }}
      style={[
        styles.button,
        standalone ? styles.buttonStandalone : styles.discSlot,
        elapsedLabel
          ? standalone
            ? styles.buttonWithLabel
            : styles.buttonWithDiscLabel
          : null,
        fixedWidth ? { width: fixedWidth, minWidth: fixedWidth, maxWidth: fixedWidth } : null,
        standalone
          ? { backgroundColor: ready ? chrome.accent : chrome.composerInput, borderColor }
          : null,
      ]}
      onPress={onPress}
      activeOpacity={0.72}
      disabled={!enabled}
    >
      {standalone ? (
        content
      ) : (
        <View
          style={[
            styles.disc,
            elapsedLabel ? styles.discWithLabel : null,
            { backgroundColor: discColor },
            // A Stop that cannot act yet reads as dormant, not as live.
            running && !enabled ? styles.discDormant : null,
          ]}
        >
          {content}
        </View>
      )}
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  button: {
    width: 44,
    height: 44,
    borderRadius: 22,
    alignItems: "center",
    justifyContent: "center",
  },
  buttonStandalone: {
    borderWidth: StyleSheet.hairlineWidth,
  },
  buttonWithLabel: {
    minWidth: 66,
    paddingHorizontal: COMPOSER_ACTION_HORIZONTAL_PADDING,
    width: "auto",
  },
  // The whole (fixed-width) slot stays the touch target, but the disc hugs
  // its trailing edge so it sits concentric with the capsule corner, 11 pt
  // in from both the bottom and the trailing edge, mirroring the Plus disc.
  discSlot: {
    alignItems: "flex-end",
    paddingRight: DISC_TRAILING_INSET,
  },
  buttonWithDiscLabel: {
    width: "auto",
    minWidth: 44,
  },
  disc: {
    width: COMPOSER_CONTROL_DISC_SIZE,
    height: COMPOSER_CONTROL_DISC_SIZE,
    borderRadius: COMPOSER_CONTROL_DISC_SIZE / 2,
    ...ContinuousCorners,
    alignItems: "center",
    justifyContent: "center",
  },
  discWithLabel: {
    width: "auto",
    minWidth: COMPOSER_CONTROL_DISC_SIZE,
    maxWidth: "100%",
    paddingHorizontal: 7,
  },
  discDormant: {
    opacity: 0.5,
  },
  runningContent: {
    alignItems: "center",
    flexDirection: "row",
    maxWidth: "100%",
  },
  stopGlyph: {
    width: 10,
    height: 10,
    borderRadius: 2.5,
  },
  elapsedLabel: {
    fontFamily: Typography.chatMonoFont,
    fontSize: 11,
    lineHeight: 15,
    marginLeft: 5,
    flexShrink: 1,
    fontVariant: ["tabular-nums"],
  },
});
