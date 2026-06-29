import React from 'react';
import { StyleSheet, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import { useAppTheme } from '../../constants/tokens';

const PATTERN_ICONS: Array<React.ComponentProps<typeof Ionicons>['name']> = [
  'chatbubble-outline',
  'pencil-outline',
  'document-text-outline',
  'code-slash-outline',
  'hardware-chip-outline',
  'terminal-outline',
  'folder-outline',
  'git-branch-outline',
];

const PATTERN_LAYOUT = [
  { left: '6%', top: '4%', size: 18, rotate: '-12deg' },
  { left: '28%', top: '9%', size: 14, rotate: '8deg' },
  { left: '52%', top: '3%', size: 16, rotate: '-6deg' },
  { left: '76%', top: '8%', size: 15, rotate: '14deg' },
  { left: '14%', top: '22%', size: 13, rotate: '10deg' },
  { left: '40%', top: '18%', size: 17, rotate: '-10deg' },
  { left: '64%', top: '24%', size: 14, rotate: '6deg' },
  { left: '86%', top: '20%', size: 12, rotate: '-14deg' },
  { left: '8%', top: '40%', size: 15, rotate: '-8deg' },
  { left: '32%', top: '36%', size: 13, rotate: '12deg' },
  { left: '56%', top: '42%', size: 16, rotate: '-4deg' },
  { left: '80%', top: '38%', size: 14, rotate: '9deg' },
  { left: '18%', top: '58%', size: 14, rotate: '7deg' },
  { left: '44%', top: '54%', size: 12, rotate: '-11deg' },
  { left: '68%', top: '60%', size: 17, rotate: '5deg' },
  { left: '90%', top: '56%', size: 13, rotate: '-9deg' },
  { left: '10%', top: '76%', size: 16, rotate: '-7deg' },
  { left: '36%', top: '72%', size: 14, rotate: '11deg' },
  { left: '60%', top: '78%', size: 15, rotate: '-5deg' },
  { left: '82%', top: '74%', size: 13, rotate: '8deg' },
  { left: '22%', top: '92%', size: 12, rotate: '-13deg' },
  { left: '50%', top: '90%', size: 14, rotate: '6deg' },
  { left: '74%', top: '94%', size: 16, rotate: '-8deg' },
] as const;

type ChatWallpaperProps = {
  style?: object;
};

export function ChatWallpaper({ style }: ChatWallpaperProps) {
  const { theme } = useAppTheme();
  const { chat } = theme;

  return (
    <View
      pointerEvents="none"
      style={[styles.root, { backgroundColor: chat.background }, style]}
    >
      {PATTERN_LAYOUT.map((slot, index) => (
        <View
          key={`${slot.left}-${slot.top}`}
          style={[
            styles.iconSlot,
            {
              left: slot.left,
              top: slot.top,
              transform: [{ rotate: slot.rotate }],
            },
          ]}
        >
          <Ionicons
            name={PATTERN_ICONS[index % PATTERN_ICONS.length]}
            size={slot.size}
            color={chat.patternIcon}
          />
        </View>
      ))}
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    ...StyleSheet.absoluteFill,
    overflow: 'hidden',
  },
  iconSlot: {
    position: 'absolute',
  },
});