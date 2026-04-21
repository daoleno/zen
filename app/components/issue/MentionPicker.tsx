import React, { useMemo } from "react";
import { FlatList, Pressable, StyleSheet, Text, View } from "react-native";
import { Colors, Radii, Spacing, Typography } from "../../constants/tokens";

export type MentionCandidate =
  | { kind: "role"; name: string }
  | { kind: "session"; role: string; sessionId: string; project: string };

export function mentionCandidateValue(candidate: MentionCandidate) {
  return candidate.kind === "role"
    ? `@${candidate.name}`
    : `@${candidate.role}#${candidate.sessionId}`;
}

function candidateKey(candidate: MentionCandidate) {
  return candidate.kind === "role"
    ? `role:${candidate.name}`
    : `session:${candidate.sessionId}`;
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
    if (!needle) {
      return candidates;
    }
    return candidates.filter((candidate) => {
      const haystack =
        candidate.kind === "role" ? candidate.name : candidate.sessionId;
      return haystack.toLowerCase().startsWith(needle);
    });
  }, [candidates, query]);

  if (filtered.length === 0) {
    return null;
  }

  return (
    <View style={styles.bar}>
      <FlatList
        horizontal
        showsHorizontalScrollIndicator={false}
        keyboardShouldPersistTaps="always"
        data={filtered}
        keyExtractor={candidateKey}
        contentContainerStyle={styles.content}
        ItemSeparatorComponent={() => <View style={styles.separator} />}
        renderItem={({ item }) => (
          <Pressable
            onPress={() => onSelect(item)}
            style={({ pressed }) => [styles.chip, pressed && styles.chipPressed]}
          >
            <Text style={styles.chipText}>{mentionCandidateValue(item)}</Text>
            {item.kind === "session" ? (
              <Text style={styles.chipProject}>{item.project}</Text>
            ) : null}
          </Pressable>
        )}
      />
    </View>
  );
}

const styles = StyleSheet.create({
  bar: {
    borderTopWidth: StyleSheet.hairlineWidth,
    borderTopColor: Colors.bgElevated,
    backgroundColor: Colors.bgSurface,
  },
  content: {
    paddingHorizontal: Spacing.lg,
    paddingVertical: Spacing.sm,
  },
  separator: {
    width: Spacing.sm,
  },
  chip: {
    flexDirection: "row",
    alignItems: "center",
    gap: Spacing.sm,
    height: 36,
    paddingHorizontal: Spacing.md,
    borderRadius: Radii.pill,
    backgroundColor: Colors.bgElevated,
  },
  chipPressed: {
    opacity: 0.7,
  },
  chipText: {
    color: Colors.textPrimary,
    fontFamily: Typography.terminalFont,
    fontSize: 13,
  },
  chipProject: {
    color: Colors.textSecondary,
    fontFamily: Typography.uiFont,
    fontSize: 11,
  },
});
