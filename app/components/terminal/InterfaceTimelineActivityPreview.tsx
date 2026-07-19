import React from "react";
import { ActivityIndicator, Image, StyleSheet, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";

export function ActivityPreview({
  uri,
  failed,
  chrome,
}: {
  uri: string | null;
  failed: boolean;
  chrome: TerminalThemeChrome;
}) {
  if (uri) {
    return (
      <Image
        source={{ uri }}
        style={[styles.image, { borderColor: chrome.border }]}
        resizeMode="cover"
      />
    );
  }
  return (
    <View style={[styles.imagePlaceholder, { borderColor: chrome.border }]}>
      {failed ? (
        <Ionicons name="image-outline" size={16} color={chrome.textMuted} />
      ) : (
        <ActivityIndicator size="small" color={chrome.textMuted} />
      )}
    </View>
  );
}

const styles = StyleSheet.create({
  image: {
    width: "100%",
    height: 150,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
  },
  imagePlaceholder: {
    height: 96,
    borderRadius: 8,
    borderWidth: StyleSheet.hairlineWidth,
    alignItems: "center",
    justifyContent: "center",
  },
});
