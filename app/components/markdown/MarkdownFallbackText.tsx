import React, { useCallback, useMemo } from "react";
import {
  Linking,
  Text,
  type StyleProp,
  type TextStyle,
} from "react-native";
import {
  isSafeMarkdownUrl,
  openSafeMarkdownUrl,
  tokenizeMarkdownLinks,
} from "./markdownLinks";

interface MarkdownFallbackTextProps {
  value: string;
  style?: StyleProp<TextStyle>;
  linkStyle?: StyleProp<TextStyle>;
}

export function MarkdownFallbackText({
  value,
  style,
  linkStyle,
}: MarkdownFallbackTextProps) {
  const parts = useMemo(() => tokenizeMarkdownLinks(value), [value]);
  const handleLinkPress = useCallback((url: string) => {
    void openSafeMarkdownUrl(url, (safeUrl) => Linking.openURL(safeUrl));
  }, []);

  return (
    <Text selectable style={style}>
      {parts.map((part, index) => {
        const url = part.url;
        if (!url || !isSafeMarkdownUrl(url)) {
          return part.text;
        }
        return (
          <Text
            accessibilityRole="link"
            key={index}
            onPress={() => handleLinkPress(url)}
            style={linkStyle}
          >
            {part.text}
          </Text>
        );
      })}
    </Text>
  );
}
