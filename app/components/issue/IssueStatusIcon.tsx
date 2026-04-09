import React from 'react';
import { View, StyleSheet } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { issueStatusColor } from '../../constants/tokens';
import type { IssueStatus } from '../../constants/tokens';

interface Props {
  status: IssueStatus;
  size?: number;
}

export function IssueStatusIcon({ status, size = 16 }: Props) {
  const color = issueStatusColor(status);

  switch (status) {
    case 'done':
      return <Ionicons name="checkmark-circle" size={size} color={color} />;
    case 'cancelled':
      return <Ionicons name="close-circle" size={size} color={color} />;
    case 'in_progress':
      return <Ionicons name="ellipse" size={size} color={color} />;
    case 'todo':
      return <Ionicons name="radio-button-off" size={size} color={color} />;
    case 'backlog':
      return <Ionicons name="ellipse-outline" size={size} color={color} />;
    default:
      return <Ionicons name="ellipse-outline" size={size} color={color} />;
  }
}
