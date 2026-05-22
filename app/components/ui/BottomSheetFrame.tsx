import React from "react";
import {
  KeyboardAvoidingView,
  Modal,
  Platform,
  Pressable,
  type StyleProp,
  StyleSheet,
  View,
  type ViewStyle,
} from "react-native";
import { useAppColors } from "../../constants/tokens";

interface BottomSheetFrameProps {
  visible: boolean;
  children: React.ReactNode;
  maxHeight?: `${number}%` | number;
  cardStyle?: StyleProp<ViewStyle>;
  contentStyle?: StyleProp<ViewStyle>;
  keyboardAvoiding?: boolean;
  onClose(): void;
}

export function BottomSheetFrame({
  visible,
  children,
  maxHeight = "75%",
  cardStyle,
  contentStyle,
  keyboardAvoiding = false,
  onClose,
}: BottomSheetFrameProps) {
  const colors = useAppColors();
  const body = (
    <>
      <Pressable style={[styles.backdrop, { backgroundColor: colors.modalBackdrop }]} onPress={onClose} />
      <View
        style={[
          styles.card,
          {
            maxHeight,
            backgroundColor: colors.modalSurface,
            borderColor: colors.borderSubtle,
          },
          cardStyle,
        ]}
      >
        <View style={[styles.handle, { backgroundColor: colors.borderStrong }]} />
        <View style={contentStyle}>{children}</View>
      </View>
    </>
  );

  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onClose}>
      {keyboardAvoiding && Platform.OS === "ios" ? (
        <KeyboardAvoidingView style={styles.root} behavior="padding">
          {body}
        </KeyboardAvoidingView>
      ) : (
        <View style={styles.root}>{body}</View>
      )}
    </Modal>
  );
}

const styles = StyleSheet.create({
  root: {
    flex: 1,
    justifyContent: "flex-end",
  },
  backdrop: {
    ...StyleSheet.absoluteFill,
  },
  card: {
    borderTopLeftRadius: 28,
    borderTopRightRadius: 28,
    borderTopWidth: StyleSheet.hairlineWidth,
    paddingHorizontal: 18,
    paddingTop: 12,
    paddingBottom: 28,
  },
  handle: {
    alignSelf: "center",
    width: 42,
    height: 4,
    borderRadius: 2,
    marginBottom: 14,
  },
});
