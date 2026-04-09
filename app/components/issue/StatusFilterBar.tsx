import React from 'react';
import { ScrollView, StyleSheet, Text, TouchableOpacity } from 'react-native';
import { Colors, Typography } from '../../constants/tokens';

export type IssueFilter = 'active' | 'backlog' | 'done' | 'all';

const FILTERS: { key: IssueFilter; label: string }[] = [
  { key: 'active', label: 'Active' },
  { key: 'backlog', label: 'Backlog' },
  { key: 'done', label: 'Done' },
  { key: 'all', label: 'All' },
];

interface Props {
  selected: IssueFilter;
  onSelect: (filter: IssueFilter) => void;
}

export function StatusFilterBar({ selected, onSelect }: Props) {
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      style={styles.container}
      contentContainerStyle={styles.content}
    >
      {FILTERS.map(f => (
        <TouchableOpacity
          key={f.key}
          style={[styles.chip, f.key === selected && styles.chipActive]}
          onPress={() => onSelect(f.key)}
          activeOpacity={0.82}
        >
          <Text style={[styles.chipText, f.key === selected && styles.chipTextActive]}>
            {f.label}
          </Text>
        </TouchableOpacity>
      ))}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: {
    maxHeight: 36,
    marginBottom: 8,
  },
  content: {
    paddingHorizontal: 20,
    gap: 8,
  },
  chip: {
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 8,
    backgroundColor: 'rgba(255,255,255,0.04)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
  },
  chipActive: {
    backgroundColor: 'rgba(91,157,255,0.15)',
    borderColor: Colors.accent,
  },
  chipText: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  chipTextActive: {
    color: Colors.accent,
  },
});
