import React, { forwardRef, type ReactNode } from "react";
import {
  StyleSheet,
  Text,
  TextInput,
  View,
  type StyleProp,
  type TextInputProps,
  type TextStyle,
  type ViewStyle,
} from "react-native";
import { TypeScale, UiTextMetrics, useAppColors } from "../../constants/tokens";
import {
  MOBILE_SINGLE_LINE_INPUT_LAYOUT,
  mobileSingleLineTextInsets,
} from "./mobileSingleLineInputModel";

export interface MobileSingleLineInputProps extends TextInputProps {
  containerStyle?: StyleProp<ViewStyle>;
  inputStyle?: StyleProp<TextStyle>;
  leading?: ReactNode;
  trailing?: ReactNode;
}

/**
 * The single shared owner for single-line inputs. Placeholder, entered text,
 * secure text, font scaling, and Android/iOS vertical centering all live
 * inside one stable control height.
 *
 * Android draws the native placeholder top-aligned inside a fixed-height
 * EditText and clips it at the top edge, so controlled inputs drop the native
 * placeholder and render it as a centered overlay instead — the same pattern
 * the chat composer uses. Uncontrolled callers keep the native placeholder.
 */
export const MobileSingleLineInput = forwardRef<
  TextInput,
  MobileSingleLineInputProps
>(function MobileSingleLineInput(
  {
    containerStyle,
    inputStyle,
    leading,
    maxFontSizeMultiplier = MOBILE_SINGLE_LINE_INPUT_LAYOUT.maximumFontSizeMultiplier,
    placeholder,
    placeholderTextColor,
    style,
    trailing,
    ...inputProps
  },
  ref,
) {
  const colors = useAppColors();
  const controlled = "value" in inputProps;
  const showPlaceholderOverlay = Boolean(
    placeholder &&
      controlled &&
      (inputProps.value == null || inputProps.value.length === 0),
  );
  const insets = mobileSingleLineTextInsets(Boolean(leading), Boolean(trailing));

  return (
    <View style={[styles.frame, containerStyle]}>
      <TextInput
        {...inputProps}
        ref={ref}
        maxFontSizeMultiplier={maxFontSizeMultiplier}
        placeholder={controlled ? undefined : placeholder}
        placeholderTextColor={placeholderTextColor}
        selectionColor={colors.selectionBackground}
        cursorColor={colors.accentStrong}
        style={[
          styles.input,
          { color: colors.textPrimary },
          leading ? styles.inputWithLeading : null,
          trailing ? styles.inputWithTrailing : null,
          style,
          inputStyle,
        ]}
      />
      {showPlaceholderOverlay ? (
        <View
          pointerEvents="none"
          style={[
            styles.placeholderSlot,
            { left: insets.left, right: insets.right },
          ]}
        >
          <Text
            numberOfLines={1}
            maxFontSizeMultiplier={maxFontSizeMultiplier}
            style={[
              styles.placeholderText,
              { color: placeholderTextColor ?? colors.textTertiary },
            ]}
          >
            {placeholder}
          </Text>
        </View>
      ) : null}
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
  placeholderSlot: {
    position: "absolute",
    top: 0,
    bottom: 0,
    justifyContent: "center",
  },
  placeholderText: {
    ...TypeScale.body,
    ...UiTextMetrics,
    fontSize: MOBILE_SINGLE_LINE_INPUT_LAYOUT.fontSize,
    lineHeight: MOBILE_SINGLE_LINE_INPUT_LAYOUT.lineHeight,
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
