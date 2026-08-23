import React, { useEffect, useMemo, useRef, useState } from "react";
import { ActivityIndicator, Pressable, StyleSheet, Text, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { TypeScale, Typography, useAppColors } from "../../constants/tokens";
import type {
  InstalledSkill,
  PackageDetail,
} from "../../services/skillsManagement";
import type { PluginSkillEntry } from "../../services/pluginSkillsDirectory";
import { SkillFileBrowser } from "../skills/SkillFileBrowser";

interface DirectoryEntryState {
  status: "loading" | "ready" | "error";
  detail?: PackageDetail;
  error?: string;
}

/**
 * Expandable Skills directory inside one Plugin inspector.
 *
 * Each entry is one daemon-reported Skill component of the inspected Plugin
 * copy. Expanding an entry loads read-only detail for exactly the matched
 * inventory copy; entries without an inventory match stay listed but say so
 * instead of guessing a Skill identity.
 */
export function PluginSkillsDirectory({
  entries,
  onInspectCopy,
}: {
  entries: PluginSkillEntry[];
  onInspectCopy(copy: InstalledSkill, path?: string): Promise<PackageDetail>;
}) {
  const colors = useAppColors();
  const [openKey, setOpenKey] = useState<string | null>(null);
  const [states, setStates] = useState<Record<string, DirectoryEntryState>>({});
  const requestToken = useRef(0);
  const entryKeys = useMemo(
    () => entries.map((entry) => entry.key).join("\u0000"),
    [entries],
  );
  useEffect(() => {
    // A different Plugin copy or refreshed inventory invalidates every
    // loaded detail; nothing may leak across copies.
    setOpenKey(null);
    setStates({});
    requestToken.current += 1;
  }, [entryKeys]);

  const load = async (entry: PluginSkillEntry, path?: string) => {
    if (!entry.copy) return;
    const token = ++requestToken.current;
    setStates((current) => ({
      ...current,
      [entry.key]: {
        status: "loading",
        detail: current[entry.key]?.detail,
      },
    }));
    try {
      const detail = await onInspectCopy(entry.copy, path);
      if (requestToken.current !== token) return;
      setStates((current) => ({
        ...current,
        [entry.key]: { status: "ready", detail },
      }));
    } catch (error) {
      if (requestToken.current !== token) return;
      setStates((current) => ({
        ...current,
        [entry.key]: {
          status: "error",
          detail: current[entry.key]?.detail,
          error:
            error instanceof Error
              ? error.message
              : "Could not inspect this Skill.",
        },
      }));
    }
  };

  const toggle = (entry: PluginSkillEntry) => {
    if (openKey === entry.key) {
      setOpenKey(null);
      return;
    }
    setOpenKey(entry.key);
    void load(entry);
  };

  return (
    <View style={styles.directory}>
      {entries.map((entry) => {
        const open = openKey === entry.key;
        const state = states[entry.key];
        return (
          <View key={entry.key}>
            <Pressable
              accessibilityRole="button"
              accessibilityLabel={`Toggle ${entry.name} Skill details`}
              accessibilityState={{ expanded: open }}
              onPress={() => toggle(entry)}
              style={[styles.row, open && { paddingBottom: 4 }]}
            >
              <Ionicons
                name={open ? "chevron-down" : "chevron-forward"}
                size={15}
                color={colors.textTertiary}
              />
              <Ionicons
                name="sparkles-outline"
                size={17}
                color={colors.warning}
              />
              <View style={styles.rowBody}>
                <Text
                  numberOfLines={1}
                  style={[styles.rowTitle, { color: colors.textSecondary }]}
                >
                  {entry.name}
                </Text>
                {entry.path ? (
                  <Text
                    numberOfLines={1}
                    style={[styles.rowPath, { color: colors.textTertiary }]}
                  >
                    {entry.path}
                  </Text>
                ) : null}
              </View>
              {!entry.copy ? (
                <Ionicons
                  name="help-circle-outline"
                  size={16}
                  color={colors.textTertiary}
                />
              ) : null}
            </Pressable>
            {open ? (
              <View style={styles.detail}>
                {state?.detail ? (
                  <SkillFileBrowser
                    detail={state.detail}
                    loading={state.status === "loading"}
                    error={state.status === "error" ? state.error : undefined}
                    onSelectFile={(path) => void load(entry, path)}
                  />
                ) : state?.status === "error" ? (
                  <Text style={[styles.stateText, { color: colors.warning }]}>
                    {state.error}
                  </Text>
                ) : !entry.copy ? (
                  <Text
                    style={[styles.stateText, { color: colors.textTertiary }]}
                  >
                    This Skill copy was not found in the current Skills
                    inventory, so its files cannot be shown here.
                  </Text>
                ) : (
                  <View style={styles.loadingRow}>
                    <ActivityIndicator
                      size="small"
                      color={colors.accent}
                    />
                    <Text
                      style={[styles.stateText, { color: colors.textSecondary }]}
                    >
                      Loading Skill
                    </Text>
                  </View>
                )}
              </View>
            ) : null}
          </View>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  directory: { gap: 2 },
  row: {
    minHeight: 40,
    flexDirection: "row",
    alignItems: "center",
    gap: 7,
    paddingVertical: 4,
  },
  rowBody: { flex: 1, minWidth: 0 },
  rowTitle: { ...TypeScale.compact, fontFamily: Typography.uiFontMedium },
  rowPath: { fontFamily: Typography.terminalFont, fontSize: 11, marginTop: 1 },
  detail: {
    marginLeft: 22,
    marginBottom: 8,
  },
  stateText: { ...TypeScale.compact },
  loadingRow: {
    flexDirection: "row",
    alignItems: "center",
    gap: 8,
    paddingVertical: 8,
  },
});
