import React, { forwardRef, type ReactNode } from "react";
import {
  StyleSheet,
  TextInput,
  View,
  type StyleProp,
  type TextInputProps,
  type TextStyle,
  type ViewStyle,
} from "react-native";
import { TypeScale, UiTextMetrics } from "../../constants/tokens";
import { MOBILE_SINGLE_LINE_INPUT_LAYOUT } from "./mobileSingleLineInputModel";

export interface MobileSingleLineInputProps extends TextInputProps {
  containerStyle?: StyleProp<ViewStyle>;
  inputStyle?: StyleProp<TextStyle>;
  leading?: ReactNode;
  trailing?: ReactNode;
}

export const MobileSingleLineInput = forwardRef<
  TextInput,
  MobileSingleLineInputProps
>(function MobileSingleLineInput(
  {
    containerStyle,
    inputStyle,
    leading,
    maxFontSizeMultiplier = MOBILE_SINGLE_LINE_INPUT_LAYOUT.maximumFontSizeMultiplier,
    style,
    trailing,
    ...inputProps
  },
  ref,
) {
  return (
    <View style={[styles.frame, containerStyle]}>
      <TextInput
        {...inputProps}
        ref={ref}
        maxFontSizeMultiplier={maxFontSizeMultiplier}
        style={[
          styles.input,
          leading ? styles.inputWithLeading : null,
          trailing ? styles.inputWithTrailing : null,
          style,
          inputStyle,
        ]}
      />
      {leading ? (
        <View pointerEvents="none" style={[styles.accessory, styles.leading]}>
          {leading}
        </View>
      ) : null}
      {trailing ? (
        <View style={[styles.accessory, styles.trailing]}>{trailing}</View>
      ) : null}
    </View>
  );
});

const styles = StyleSheet.create({
  frame: {
    height: MOBILE_SINGLE_LINE_INPUT_LAYOUT.controlHeight,
    position: "relative",
    justifyContent: "center",
  },
  input: {
    ...TypeScale.body,
    ...UiTextMetrics,
    fontSize: MOBILE_SINGLE_LINE_INPUT_LAYOUT.fontSize,
    lineHeight: MOBILE_SINGLE_LINE_INPUT_LAYOUT.lineHeight,
    width: "100%",
    height: MOBILE_SINGLE_LINE_INPUT_LAYOUT.controlHeight,
    paddingHorizontal: MOBILE_SINGLE_LINE_INPUT_LAYOUT.horizontalPadding,
    paddingVertical: 0,
    textAlignVertical: "center",
  },
  inputWithLeading: {
    paddingLeft:
      MOBILE_SINGLE_LINE_INPUT_LAYOUT.accessoryLaneWidth +
      MOBILE_SINGLE_LINE_INPUT_LAYOUT.horizontalPadding,
  },
  inputWithTrailing: {
    paddingRight: MOBILE_SINGLE_LINE_INPUT_LAYOUT.accessoryLaneWidth,
  },
  accessory: {
    position: "absolute",
    top: 0,
    bottom: 0,
    width: MOBILE_SINGLE_LINE_INPUT_LAYOUT.accessoryLaneWidth,
    alignItems: "center",
    justifyContent: "center",
  },
  leading: { left: 0 },
  trailing: { right: 0 },
});
