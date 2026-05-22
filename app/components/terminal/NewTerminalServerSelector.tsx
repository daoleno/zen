import React, { useMemo } from "react";
import {
  ScrollView,
  StyleSheet,
  TouchableOpacity,
  View,
} from "react-native";
import { Colors, useAppColors } from "../../constants/tokens";
import { AppText } from "../ui";

export type NewTerminalServerOption = {
  id: string;
  name: string;
};

interface NewTerminalServerSelectorProps {
  serverOptions: NewTerminalServerOption[];
  selectedServerId?: string | null;
  onSelectServer?(serverId: string): void;
}

export function NewTerminalServerSelector({
  serverOptions,
  selectedServerId,
  onSelectServer,
}: NewTerminalServerSelectorProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);

  if (serverOptions.length <= 1) {
    return null;
  }

  return (
    <View style={styles.serverSection}>
      <ScrollView horizontal showsHorizontalScrollIndicator={false}>
        <View style={styles.serverRow}>
          {serverOptions.map((server) => {
            const active = server.id === selectedServerId;
            return (
              <TouchableOpacity
                key={server.id}
                style={[styles.serverChip, active && styles.serverChipActive]}
                onPress={() => onSelectServer?.(server.id)}
                activeOpacity={0.84}
              >
                <AppText variant="label" tone={active ? "primary" : "secondary"}>
                  {server.name}
                </AppText>
              </TouchableOpacity>
            );
          })}
        </View>
      </ScrollView>
    </View>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    serverSection: {
      marginBottom: 8,
    },
    serverRow: {
      flexDirection: "row",
      gap: 8,
    },
    serverChip: {
      paddingHorizontal: 10,
      height: 28,
      borderRadius: 10,
      justifyContent: "center",
      backgroundColor: colors.bgElevated,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: colors.borderSubtle,
    },
    serverChipActive: {
      backgroundColor: colors.surfaceActive,
      borderColor: colors.accent,
    },
  });
}
