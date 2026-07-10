import { StyleSheet, Text, View } from 'react-native';
import { Typography, useAppColors, useAppTheme } from '../../constants/tokens';
import {
  avatarColorForSeed,
  initialsFromLabel,
} from '../../constants/telegramPresentation';

interface SessionAvatarProps {
  label: string;
  seed?: string;
  size?: number;
}

export function SessionAvatar({
  label,
  seed,
  size = 48,
}: SessionAvatarProps) {
  const colors = useAppColors();
  const { theme } = useAppTheme();
  const backgroundColor = avatarColorForSeed(seed ?? label, theme.avatarColors);
  const initials = initialsFromLabel(label);

  return (
    <View
      style={[
        styles.root,
        {
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor,
        },
      ]}
    >
      <Text
        style={[
          styles.initials,
          {
            color: colors.textOnAccent,
            fontSize: size * 0.36,
            lineHeight: size * 0.42,
          },
        ]}
        numberOfLines={1}
      >
        {initials}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    alignItems: 'center',
    justifyContent: 'center',
    overflow: 'hidden',
  },
  initials: {
    fontFamily: Typography.uiFontMedium,
    letterSpacing: 0.2,
    includeFontPadding: false,
  },
});
