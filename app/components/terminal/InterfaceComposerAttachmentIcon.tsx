import React from "react";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

interface InterfaceComposerAttachmentIconProps {
  fileName: string;
  chrome: TerminalThemeChrome;
}

export function InterfaceComposerAttachmentIcon({
  fileName,
  chrome,
}: InterfaceComposerAttachmentIconProps) {
  return (
    <Ionicons
      name={
        looksLikeImagePath(fileName)
          ? "image-outline"
          : "document-attach-outline"
      }
      size={14}
      color={chrome.textMuted}
    />
  );
}

function looksLikeImagePath(value: string) {
  return /\.(png|jpe?g|gif|webp|bmp)$/i.test(value.trim());
}
