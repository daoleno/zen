import React from 'react';
import { View, StyleSheet } from 'react-native';
import { priorityColor } from '../../constants/tokens';
import type { IssuePriority } from '../../constants/tokens';

interface Props {
  priority: IssuePriority;
}

export function PriorityBar({ priority }: Props) {
  if (priority === 0) return <View style={styles.spacer} />;

  return (
    <View style={[styles.bar, { backgroundColor: priorityColor(priority) }]} />
  );
}

const styles = StyleSheet.create({
  bar: {
    width: 3,
    borderRadius: 1.5,
    alignSelf: 'stretch',
  },
  spacer: {
    width: 3,
  },
});
