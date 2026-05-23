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
  children: React.ReactNode;
}

export function CodexChatKeyboardFrame({
  enabled,
  keyboardVerticalOffset,
  children,
}: CodexChatKeyboardFrameProps) {
  if (Platform.OS === "android") {
    return <View style={styles.chatBody}>{children}</View>;
  }

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
