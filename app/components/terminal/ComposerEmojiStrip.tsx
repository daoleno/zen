import React, { useMemo } from "react";
import {
  ScrollView,
  StyleSheet,
  Text,
  TouchableOpacity,
  View,
} from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { Typography } from "../../constants/tokens";

const QUICK_EMOJIS = [
  "👍", "❤️", "😂", "🔥", "👀", "🙏", "✅", "🎉",
  "😅", "🤔", "💯", "⚡", "🚀", "👋", "😎", "🫡",
] as const;

interface ComposerEmojiStripProps {
  chrome: TerminalThemeChrome;
  onPick(emoji: string): void;
}

export function ComposerEmojiStrip({
  chrome,
  onPick,
}: ComposerEmojiStripProps) {
  const styles = useMemo(() => createStyles(chrome), [chrome]);

  return (
    <View style={styles.root}>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={styles.content}
        keyboardShouldPersistTaps="handled"
      >
        {QUICK_EMOJIS.map((emoji) => (
          <TouchableOpacity
            key={emoji}
            accessibilityLabel={`Insert ${emoji}`}
            accessibilityRole="button"
            style={styles.chip}
            onPress={() => onPick(emoji)}
            activeOpacity={0.78}
          >
            <Text style={styles.emoji}>{emoji}</Text>
          </TouchableOpacity>
        ))}
      </ScrollView>
    </View>
  );
}

function createStyles(chrome: TerminalThemeChrome) {
  return StyleSheet.create({
    root: {
      marginBottom: 6,
    },
    content: {
      gap: 4,
      paddingHorizontal: 2,
    },
    chip: {
      width: 38,
      height: 38,
      borderRadius: 19,
      alignItems: "center",
      justifyContent: "center",
      backgroundColor: chrome.surface,
    },
    emoji: {
      fontSize: 22,
      lineHeight: 26,
      textAlign: "center",
      fontFamily: Typography.uiFont,
    },
  });
}