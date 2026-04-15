import React from 'react';
import { ScrollView, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
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
  counts?: Partial<Record<IssueFilter, number>>;
}

export function StatusFilterBar({ selected, onSelect, counts }: Props) {
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      style={styles.container}
      contentContainerStyle={styles.content}
    >
      {FILTERS.map(filter => {
        const count = counts?.[filter.key];
        const active = filter.key === selected;
        const shouldShowCount = typeof count === 'number' && count > 0;

        return (
          <TouchableOpacity
            key={filter.key}
            style={[styles.chip, active && styles.chipActive]}
            onPress={() => onSelect(filter.key)}
            activeOpacity={0.82}
          >
            <Text style={[styles.chipText, active && styles.chipTextActive]}>
              {filter.label}
            </Text>
            {shouldShowCount ? (
              <View style={[styles.countPill, active && styles.countPillActive]}>
                <Text style={[styles.countText, active && styles.countTextActive]}>
                  {count}
                </Text>
              </View>
            ) : null}
          </TouchableOpacity>
        );
      })}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  container: {
    maxHeight: 42,
    marginBottom: 10,
  },
  content: {
    paddingHorizontal: 20,
    gap: 8,
  },
  chip: {
    minHeight: 36,
    paddingLeft: 13,
    paddingRight: 8,
    borderRadius: 999,
    flexDirection: 'row',
    alignItems: 'center',
    gap: 8,
    backgroundColor: 'rgba(255,255,255,0.04)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
  },
  chipActive: {
    backgroundColor: 'rgba(91,157,255,0.12)',
    borderColor: 'rgba(91,157,255,0.4)',
  },
  chipText: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  chipTextActive: {
    color: Colors.textPrimary,
  },
  countPill: {
    minWidth: 22,
    height: 22,
    paddingHorizontal: 6,
    borderRadius: 11,
    alignItems: 'center',
    justifyContent: 'center',
    backgroundColor: 'rgba(255,255,255,0.06)',
  },
  countPillActive: {
    backgroundColor: 'rgba(91,157,255,0.18)',
  },
  countText: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
  },
  countTextActive: {
    color: Colors.accent,
  },
});
