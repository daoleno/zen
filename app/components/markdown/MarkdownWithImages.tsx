import React, { useMemo } from "react";
import { Text, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { imageReference } from "../../services/imageSource";
import { ZenImage } from "../terminal/ZenImage";
import { splitMarkdownImages } from "./markdownImages";

export function MarkdownWithImages({ markdown, chrome, renderMarkdown }: {
  markdown: string; chrome: TerminalThemeChrome; renderMarkdown(value: string): React.ReactNode;
}) {
  const segments = useMemo(() => splitMarkdownImages(markdown), [markdown]);
  const gallery = useMemo(() => segments.flatMap((segment) => segment.type === "image" ? [imageReference(segment.path, segment.alt || "Image")] : []), [segments]);
  if (!gallery.length) return renderMarkdown(markdown);
  return <View style={{ gap: 8 }}>{segments.map((segment, index) => segment.type === "markdown"
    ? <React.Fragment key={index}>{renderMarkdown(segment.text)}</React.Fragment>
    : <View key={index}><ZenImage source={imageReference(segment.path, segment.alt || "Image")} gallery={gallery} chrome={chrome} />
      {segment.alt || segment.title ? <Text selectable style={{ color: chrome.textMuted, marginTop: 4 }}>{segment.title || segment.alt}</Text> : null}
    </View>)}</View>;
}
