import React from "react";
import { type StyleProp, StyleSheet, View, type ViewStyle } from "react-native";
import ExpoSegmentedControl, {
  type NativeSegmentedControlChangeEvent,
} from "@expo/ui/community/segmented-control";
import { useAppColors } from "../../constants/tokens";

interface NativeSegmentedControlProps<T extends string> {
  options: Array<{ value: T; label: string }>;
  selectedValue: T;
  tintColor?: string;
  appearance?: "dark" | "light";
  style?: StyleProp<ViewStyle>;
  controlStyle?: StyleProp<ViewStyle>;
  onSelect(value: T): void;
}

export function NativeSegmentedControl<T extends string>({
  options,
  selectedValue,
  tintColor,
  appearance = "dark",
  style,
  controlStyle,
  onSelect,
}: NativeSegmentedControlProps<T>) {
  const colors = useAppColors();
  const selectedIndex = Math.max(
    0,
    options.findIndex((option) => option.value === selectedValue),
  );
  const labels = React.useMemo(() => options.map((option) => option.label), [options]);
  const handleChange = React.useCallback(
    (event: NativeSegmentedControlChangeEvent) => {
      const next = options[event.nativeEvent.selectedSegmentIndex];
      if (next) {
        onSelect(next.value);
      }
    },
    [onSelect, options],
  );

  return (
    <View
      style={[
        styles.frame,
        {
          backgroundColor: colors.bgSurface,
          borderColor: colors.borderSubtle,
        },
        style,
      ]}
    >
      <ExpoSegmentedControl
        values={labels}
        selectedIndex={selectedIndex}
        onChange={handleChange}
        tintColor={tintColor ?? colors.accent}
        appearance={appearance}
        style={[styles.control, controlStyle]}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  frame: {
    minHeight: 34,
    borderRadius: 12,
    borderWidth: StyleSheet.hairlineWidth,
    padding: 3,
  },
  control: {
    minHeight: 28,
    width: "100%",
  },
});
