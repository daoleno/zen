import React from "react";
import {
  Platform,
  StyleSheet,
  View,
} from "react-native";
import { KeyboardAvoidingView } from "react-native-keyboard-controller";

interface CodexChatKeyboardFrameProps {
  enabled: boolean;
  keyboardVerticalOffset: number;
  automaticOffset?: boolean;
  children: React.ReactNode;
}

export function CodexChatKeyboardFrame({
  enabled,
  keyboardVerticalOffset,
  automaticOffset,
  children,
}: CodexChatKeyboardFrameProps) {
  return (
    <KeyboardAvoidingView
      behavior={Platform.OS === "android" ? "height" : "padding"}
      collapsable={false}
      enabled={enabled}
      keyboardVerticalOffset={keyboardVerticalOffset}
      automaticOffset={automaticOffset}
      style={styles.chatBody}
    >
      <View collapsable={false} style={styles.chatContent}>
        {children}
      </View>
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  chatBody: {
    flex: 1,
    minHeight: 0,
  },
  chatContent: {
    flex: 1,
    minHeight: 0,
    overflow: "visible",
  },
});
