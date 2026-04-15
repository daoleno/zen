import React, { useMemo } from 'react';
import { ScrollView, StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Colors, Typography } from '../../constants/tokens';
import { buildDueDateOptions, formatDueDateLong, getDueDateState } from '../../services/dueDate';

interface Props {
  value?: string | null;
  onChange: (dueDate: string | null) => void;
  days?: number;
}

export function DueDatePicker({ value, onChange, days = 21 }: Props) {
  const options = useMemo(() => buildDueDateOptions(days), [days]);
  const dueState = getDueDateState(value);

  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      contentContainerStyle={styles.row}
    >
      <TouchableOpacity
        style={[styles.chip, !value && styles.chipActive]}
        onPress={() => onChange(null)}
        activeOpacity={0.82}
      >
        <Text style={[styles.chipLabel, !value && styles.chipLabelActive]}>No date</Text>
        <Text style={[styles.chipMeta, !value && styles.chipMetaActive]}>Clear</Text>
      </TouchableOpacity>

      {options.map(option => {
        const active = option.value === value;

        return (
          <TouchableOpacity
            key={option.value}
            style={[
              styles.chip,
              active && styles.chipActive,
              dueState?.isOverdue && active && styles.chipOverdue,
            ]}
            onPress={() => onChange(option.value)}
            activeOpacity={0.82}
          >
            <Text style={[styles.chipLabel, active && styles.chipLabelActive]}>
              {option.label}
            </Text>
            <Text style={[styles.chipMeta, active && styles.chipMetaActive]}>
              {option.meta}
            </Text>
          </TouchableOpacity>
        );
      })}

      {value && !options.some(option => option.value === value) ? (
        <View style={[styles.chip, styles.chipPassive]}>
          <Text style={styles.chipLabel}>{formatDueDateLong(value)}</Text>
          <Text style={styles.chipMeta}>Saved</Text>
        </View>
      ) : null}
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  row: {
    gap: 8,
  },
  chip: {
    minWidth: 82,
    minHeight: 58,
    paddingHorizontal: 12,
    paddingVertical: 10,
    borderRadius: 16,
    justifyContent: 'center',
    backgroundColor: 'rgba(255,255,255,0.04)',
    borderWidth: StyleSheet.hairlineWidth,
    borderColor: 'rgba(255,255,255,0.08)',
  },
  chipActive: {
    backgroundColor: 'rgba(91,157,255,0.12)',
    borderColor: 'rgba(91,157,255,0.42)',
  },
  chipOverdue: {
    borderColor: 'rgba(255,82,82,0.42)',
    backgroundColor: 'rgba(255,82,82,0.12)',
  },
  chipPassive: {
    minWidth: 110,
  },
  chipLabel: {
    color: Colors.textPrimary,
    fontSize: 12,
    lineHeight: 16,
    fontFamily: Typography.uiFontMedium,
  },
  chipLabelActive: {
    color: Colors.textPrimary,
  },
  chipMeta: {
    color: Colors.textSecondary,
    fontSize: 11,
    lineHeight: 15,
    fontFamily: Typography.uiFont,
    marginTop: 3,
  },
  chipMetaActive: {
    color: Colors.textSecondary,
  },
});
