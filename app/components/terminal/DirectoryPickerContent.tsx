import React, { useMemo } from "react";
import { FlatList, StyleSheet, TouchableOpacity, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { Colors, useAppColors } from "../../constants/tokens";
import { displayPathSubtitle } from "../../services/pathDisplay";
import { AppButton, AppText, IconButton, StateView } from "../ui";
import type { DirectoryPickerEntry } from "./directoryPickerState";

export type { DirectoryPickerEntry };

interface DirectoryPickerContentProps {
  currentPath: string;
  entries: DirectoryPickerEntry[];
  loading: boolean;
  error: string | null;
  onGoUp(): void;
  onOpenDirectory(path: string): void;
  onSelectCurrent(): void;
  onClose(): void;
}

export function DirectoryPickerContent({
  currentPath,
  entries,
  loading,
  error,
  onGoUp,
  onOpenDirectory,
  onSelectCurrent,
  onClose,
}: DirectoryPickerContentProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);

  return (
    <>
      <View style={styles.header}>
        <AppText variant="title">Select Directory</AppText>
      </View>

      <View style={styles.pathRow}>
        <IconButton
          icon="arrow-up"
          size={32}
          onPress={onGoUp}
          accessibilityLabel="Go up one directory"
        />
        <AppText
          variant="mono"
          tone="secondary"
          style={styles.pathText}
          numberOfLines={1}
          ellipsizeMode="head"
        >
          {displayPathSubtitle(currentPath)}
        </AppText>
      </View>

      {loading ? (
        <StateView loading />
      ) : error ? (
        <StateView detail={error} danger />
      ) : (
        <FlatList
          data={entries}
          keyExtractor={(item) => item.path}
          style={styles.list}
          renderItem={({ item }) => (
            <TouchableOpacity
              style={styles.dirRow}
              onPress={() => onOpenDirectory(item.path)}
              activeOpacity={0.7}
              accessibilityRole="button"
              accessibilityLabel={`Open directory ${item.name}`}
            >
              <Ionicons name="folder" size={18} color={colors.promptYellow} />
              <AppText variant="body" style={styles.dirName} numberOfLines={1}>
                {item.name}
              </AppText>
              <Ionicons
                name="chevron-forward"
                size={14}
                color={colors.textSecondary}
              />
            </TouchableOpacity>
          )}
          ListEmptyComponent={<StateView detail="No subdirectories" />}
        />
      )}

      <View style={styles.actions}>
        <AppButton
          label="Select This Directory"
          variant="primary"
          onPress={onSelectCurrent}
          disabled={loading || !currentPath}
        />
        <AppButton label="Cancel" variant="secondary" onPress={onClose} />
      </View>
    </>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    header: {
      marginBottom: 12,
    },
    pathRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 8,
      marginBottom: 12,
      paddingHorizontal: 4,
    },
    pathText: {
      flex: 1,
    },
    list: {
      maxHeight: 300,
    },
    dirRow: {
      flexDirection: "row",
      alignItems: "center",
      gap: 12,
      paddingVertical: 12,
      paddingHorizontal: 12,
      borderRadius: 12,
      marginBottom: 4,
      backgroundColor: colors.bgElevated,
    },
    dirName: {
      flex: 1,
    },
    actions: {
      marginTop: 14,
      gap: 8,
    },
  });
}
