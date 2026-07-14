import React from "react";
import {
  Platform,
  StyleSheet,
  View,
} from "react-native";
import {
  KeyboardAvoidingView,
  useKeyboardState,
} from "react-native-keyboard-controller";
import {
  keyboardAvoidanceResetStyle,
  shouldAvoidKeyboard,
} from "./keyboardAvoidancePolicy";

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
  const keyboardFrameRef = React.useRef<View>(null);
  const keyboardVisible = useKeyboardState((state) => state.isVisible);
  const avoidanceEnabled = shouldAvoidKeyboard(enabled, keyboardVisible);

  React.useLayoutEffect(() => {
    if (avoidanceEnabled) {
      return;
    }

    const resetAvoidanceLayout = () => {
      keyboardFrameRef.current?.setNativeProps({
        style: keyboardAvoidanceResetStyle(Platform.OS),
      });
    };

    // The controller's animated style returns an empty object when disabled,
    // but Reanimated does not unset keys that disappear from an update. Reset
    // once now and once after the native keyboard end event has been applied.
    resetAvoidanceLayout();
    const resetFrame = requestAnimationFrame(resetAvoidanceLayout);

    return () => cancelAnimationFrame(resetFrame);
  }, [avoidanceEnabled]);

  return (
    <KeyboardAvoidingView
      ref={keyboardFrameRef}
      behavior={Platform.OS === "android" ? "height" : "padding"}
      collapsable={false}
      enabled={avoidanceEnabled}
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
