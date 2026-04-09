import React from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Colors, Typography, priorityColor } from '../../constants/tokens';
import type { IssuePriority } from '../../constants/tokens';

const PRIORITIES: { value: IssuePriority; label: string; icon: string }[] = [
  { value: 0, label: 'None', icon: '—' },
  { value: 4, label: 'Low', icon: '!' },
  { value: 3, label: 'Medium', icon: '!!' },
  { value: 2, label: 'High', icon: '!!!' },
  { value: 1, label: 'Urgent', icon: '!!!!' },
];

interface Props {
  value: IssuePriority;
  onChange: (priority: IssuePriority) => void;
}

export function PriorityPicker({ value, onChange }: Props) {
  return (
    <View style={styles.row}>
      {PRIORITIES.map(p => {
        const active = p.value === value;
        const color = p.value === 0 ? Colors.textSecondary : priorityColor(p.value);
        return (
          <TouchableOpacity
            key={p.value}
            style={[styles.chip, active && { backgroundColor: color + '22', borderColor: color }]}
            onPress={() => onChange(p.value)}
            activeOpacity={0.82}
          >
            <Text style={[styles.chipText, active && { color }]}>
              {p.label}
            </Text>
          </TouchableOpacity>
        );
      })}
    </View>
  );
}

const styles = StyleSheet.create({
  row: {
    flexDirection: 'row',
    gap: 6,
    flexWrap: 'wrap',
  },
  chip: {
    paddingHorizontal: 10,
    paddingVertical: 5,
    borderRadius: 8,
    backgroundColor: 'rgba(255,255,255,0.04)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
  },
  chipText: {
    color: Colors.textSecondary,
    fontSize: 12,
    fontFamily: Typography.uiFontMedium,
  },
});
