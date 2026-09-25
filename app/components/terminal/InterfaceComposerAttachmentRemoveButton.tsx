import React from "react";
import {
  StyleSheet,
  TouchableOpacity,
  View,
  type StyleProp,
  type ViewStyle,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TouchTarget } from "../../constants/tokens";
import { composerNeutralFill } from "./composerMaterial";

interface InterfaceComposerAttachmentRemoveButtonProps {
  attachmentName: string;
  chrome: TerminalThemeChrome;
  onPress(): void;
  /**
   * `inline` trails a file tile on the tile's own surface; `overlay` floats
   * over an image thumbnail with a scrim badge that stays legible on any
   * photo.
   */
  placement?: "inline" | "overlay";
  style?: StyleProp<ViewStyle>;
}

/** Scrim for the badge floated over arbitrary image content. */
const OVERLAY_SCRIM = "rgba(0, 0, 0, 0.56)";
const OVERLAY_GLYPH = "#FFFFFF";

/**
 * The platform touch-target box (44 pt / 48 dp) is the hit area and always sits inside the owning
 * tile (Android does not deliver touches outside a parent's bounds); only
 * the 22 pt badge is drawn.
 */
export function InterfaceComposerAttachmentRemoveButton({
  attachmentName,
  chrome,
  onPress,
  placement = "inline",
  style,
}: InterfaceComposerAttachmentRemoveButtonProps) {
  const overlay = placement === "overlay";
  return (
    <TouchableOpacity
      accessibilityLabel={`Remove ${attachmentName}`}
      accessibilityRole="button"
      style={[styles.remove, overlay ? styles.removeOverlay : null, style]}
      onPress={onPress}
      activeOpacity={0.64}
    >
      <View
        style={[
          styles.badge,
          {
            backgroundColor: overlay
              ? OVERLAY_SCRIM
              : composerNeutralFill(chrome, "strong"),
          },
        ]}
      >
        <Ionicons
          name="close"
          size={13}
          color={overlay ? OVERLAY_GLYPH : chrome.text}
        />
      </View>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  remove: {
    width: TouchTarget,
    height: TouchTarget,
    alignItems: "center",
    justifyContent: "center",
  },
  removeOverlay: {
    alignItems: "flex-end",
    justifyContent: "flex-start",
    padding: 6,
  },
  badge: {
    width: 22,
    height: 22,
    borderRadius: 11,
    alignItems: "center",
    justifyContent: "center",
  },
});
