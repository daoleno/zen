import React from 'react';
import { StyleSheet, Text, TouchableOpacity, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { Colors, Typography } from '../../constants/tokens';
import type { Skill } from '../../store/tasks';

const ICON_MAP: Record<string, React.ComponentProps<typeof Ionicons>['name']> = {
  eye: 'eye-outline',
  wrench: 'build-outline',
  book: 'book-outline',
  code: 'code-slash-outline',
  bug: 'bug-outline',
  rocket: 'rocket-outline',
};

interface Props {
  skill: Skill;
  onPress: () => void;
}

export function SkillCard({ skill, onPress }: Props) {
  const iconName = ICON_MAP[skill.icon] || 'flash-outline';

  return (
    <TouchableOpacity style={styles.card} onPress={onPress} activeOpacity={0.82}>
      <View style={styles.iconWrap}>
        <Ionicons name={iconName} size={18} color={Colors.accent} />
      </View>
      <Text style={styles.name} numberOfLines={1}>{skill.name}</Text>
    </TouchableOpacity>
  );
}

const styles = StyleSheet.create({
  card: {
    alignItems: 'center',
    gap: 6,
    width: 80,
    paddingVertical: 10,
  },
  iconWrap: {
    width: 44,
    height: 44,
    borderRadius: 12,
    backgroundColor: 'rgba(91,157,255,0.1)',
    alignItems: 'center',
    justifyContent: 'center',
  },
  name: {
    color: Colors.textPrimary,
    fontSize: 11,
    fontFamily: Typography.uiFont,
    textAlign: 'center',
  },
});
