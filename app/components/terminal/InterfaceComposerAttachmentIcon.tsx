import React from "react";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface InterfaceComposerAttachmentIconProps {
  fileName: string;
  chrome: TerminalThemeChrome;
  color?: string;
  size?: number;
}

export function InterfaceComposerAttachmentIcon({
  fileName,
  chrome,
  color,
  size = 17,
}: InterfaceComposerAttachmentIconProps) {
  return (
    <Ionicons
      name={
        looksLikeImagePath(fileName)
          ? "image-outline"
          : "document-text-outline"
      }
      size={size}
      color={color ?? chrome.textMuted}
    />
  );
}

function looksLikeImagePath(value: string) {
  return /\.(png|jpe?g|gif|webp|bmp)$/i.test(value.trim());
}
