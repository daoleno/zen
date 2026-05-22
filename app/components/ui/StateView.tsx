import React from "react";
import { ActivityIndicator, type StyleProp, StyleSheet, View, type ViewStyle } from "react-native";
import { useAppColors } from "../../constants/tokens";
import { AppText } from "./AppText";

interface StateViewProps {
  title?: string;
  detail?: string | null;
  loading?: boolean;
  danger?: boolean;
  style?: StyleProp<ViewStyle>;
}

export function StateView({ title, detail, loading = false, danger = false, style }: StateViewProps) {
  const colors = useAppColors();
  return (
    <View style={[styles.root, style]}>
      {loading ? <ActivityIndicator color={colors.textSecondary} /> : null}
      {title ? (
        <AppText variant="label" tone={danger ? "danger" : "secondary"} style={styles.title}>
          {title}
        </AppText>
      ) : null}
      {detail ? (
        <AppText variant="caption" tone={danger ? "danger" : "secondary"} style={styles.detail}>
          {detail}
        </AppText>
      ) : null}
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    minHeight: 120,
    alignItems: "center",
    justifyContent: "center",
    paddingHorizontal: 18,
  },
  title: {
    textAlign: "center",
  },
  detail: {
    marginTop: 6,
    textAlign: "center",
  },
});
