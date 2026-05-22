import React from "react";
import { StyleSheet } from "react-native";
import { KeyboardAvoidingView } from "react-native-keyboard-controller";

interface CodexChatKeyboardFrameProps {
  enabled: boolean;
  keyboardVerticalOffset: number;
  children: React.ReactNode;
}

export function CodexChatKeyboardFrame({
  enabled,
  keyboardVerticalOffset,
  children,
}: CodexChatKeyboardFrameProps) {
  return (
    <KeyboardAvoidingView
      behavior="padding"
      enabled={enabled}
      keyboardVerticalOffset={keyboardVerticalOffset}
      style={styles.chatBody}
    >
      {children}
    </KeyboardAvoidingView>
  );
}

const styles = StyleSheet.create({
  chatBody: {
    flex: 1,
    minHeight: 0,
  },
});
