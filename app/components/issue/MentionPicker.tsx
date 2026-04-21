import React, { useMemo } from "react";
import { FlatList, Pressable, StyleSheet, Text, View } from "react-native";
import { Colors, Typography } from "../../constants/tokens";

export type MentionCandidate =
  | { kind: "role"; name: string }
  | { kind: "session"; role: string; sessionId: string; project: string };

export function mentionCandidateValue(candidate: MentionCandidate) {
  return candidate.kind === "role"
    ? `@${candidate.name}`
    : `@${candidate.role}#${candidate.sessionId}`;
}

export function MentionPicker({
  candidates,
  query,
  onSelect,
}: {
  candidates: MentionCandidate[];
  query: string;
  onSelect: (candidate: MentionCandidate) => void;
}) {
  const filtered = useMemo(() => {
    const needle = query.toLowerCase();
    return candidates.filter((candidate) => {
      const haystack = candidate.kind === "role" ? candidate.name : candidate.sessionId;
      return haystack.toLowerCase().startsWith(needle);
    });
  }, [candidates, query]);

  if (filtered.length === 0) {
    return null;
  }

  return (
    <View style={styles.container}>
      <FlatList
        data={filtered}
        keyExtractor={(item) =>
          item.kind === "role" ? `role:${item.name}` : `session:${item.sessionId}`
        }
        keyboardShouldPersistTaps="always"
        renderItem={({ item }) => (
          <Pressable onPress={() => onSelect(item)} style={styles.row}>
            <Text style={styles.label}>{mentionCandidateValue(item)}</Text>
            {item.kind === "session" ? (
              <Text style={styles.project}>{item.project}</Text>
            ) : null}
          </Pressable>
        )}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    maxHeight: 220,
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: Colors.bgElevated,
    backgroundColor: Colors.bgSurface,
  },
  row: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "space-between",
    paddingHorizontal: 14,
    paddingVertical: 11,
  },
  label: {
    color: Colors.textPrimary,
    fontFamily: Typography.terminalFont,
    fontSize: 13,
  },
  project: {
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 12,
  },
});
