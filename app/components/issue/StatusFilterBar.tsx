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
    // Outer View with explicit height is the key — ScrollView alone can expand on Android
    <View style={styles.container}>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        style={styles.scroll}
        contentContainerStyle={styles.content}
      >
        {FILTERS.map(filter => {
          const count = counts?.[filter.key];
          const active = filter.key === selected;
          const shouldShowCount = typeof count === 'number' && count > 0;

          return (
            <TouchableOpacity
              key={filter.key}
              style={styles.tab}
              onPress={() => onSelect(filter.key)}
              activeOpacity={0.82}
            >
              <View style={styles.tabInner}>
                <Text style={[styles.tabLabel, active && styles.tabLabelActive]}>
                  {filter.label}
                </Text>
                {shouldShowCount ? (
                  <Text style={[styles.tabCount, active && styles.tabCountActive]}>
                    {count}
                  </Text>
                ) : null}
              </View>
              {active ? <View style={styles.tabIndicator} /> : null}
            </TouchableOpacity>
          );
        })}
      </ScrollView>
    </View>
  );
}

const styles = StyleSheet.create({
  container: {
    height: 38,
    borderBottomWidth: StyleSheet.hairlineWidth,
    borderBottomColor: 'rgba(255,255,255,0.07)',
    overflow: 'hidden',
  },
  scroll: {
    flex: 1,
  },
  content: {
    paddingHorizontal: 8,
    alignItems: 'center',
  },
  tab: {
    paddingHorizontal: 12,
    paddingTop: 2,
    paddingBottom: 0,
    position: 'relative',
  },
  tabInner: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 6,
    paddingBottom: 10,
  },
  tabLabel: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
  tabLabelActive: {
    color: Colors.textPrimary,
  },
  tabCount: {
    color: Colors.textSecondary,
    fontSize: 11,
    fontFamily: Typography.terminalFont,
  },
  tabCountActive: {
    color: Colors.accent,
  },
  tabIndicator: {
    position: 'absolute',
    bottom: 0,
    left: 12,
    right: 12,
    height: 1.5,
    borderRadius: 1,
    backgroundColor: Colors.accent,
  },
});
