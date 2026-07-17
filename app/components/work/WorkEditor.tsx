import React, { useMemo } from "react";
import { StyleSheet, TextInput } from "react-native";
import { Colors, Spacing, Typography, useAppColors } from "../../constants/tokens";

type Props = {
  value: string;
  onChange: (text: string) => void;
  onBlur?: () => void;
};

export function WorkEditor({ value, onChange, onBlur }: Props) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);

  return (
    <TextInput
      value={value}
      onChangeText={onChange}
      onBlur={onBlur}
      multiline
      placeholder="# Work title\n\n## Context\n\n## Outcome\n\n## Notes\n\n"
      placeholderTextColor={colors.textSecondary}
      style={styles.input}
      textAlignVertical="top"
      autoCapitalize="none"
      autoCorrect={false}
      scrollEnabled
    />
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    input: {
      flex: 1,
      paddingHorizontal: 18,
      paddingVertical: Spacing.lg,
      color: colors.textPrimary,
      fontFamily: Typography.terminalFont,
      fontSize: 15,
      lineHeight: 22,
      backgroundColor: "transparent",
    },
  });
}
