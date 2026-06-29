import React from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Typography } from '../../constants/tokens';

interface FlavorLetterBadgeProps {
  letter: string;
  backgroundColor: string;
  textColor: string;
  size: number;
}

export function FlavorLetterBadge({
  letter,
  backgroundColor,
  textColor,
  size,
}: FlavorLetterBadgeProps) {
  const frameSize = size + 10;
  const fontSize = Math.round(frameSize * 0.42);

  return (
    <View
      style={[
        styles.root,
        {
          width: frameSize,
          height: frameSize,
          borderRadius: Math.round(frameSize * 0.28),
          backgroundColor,
        },
      ]}
    >
      <Text
        style={[
          styles.letter,
          {
            color: textColor,
            fontSize,
            lineHeight: fontSize + 2,
          },
        ]}
      >
        {letter}
      </Text>
    </View>
  );
}

const styles = StyleSheet.create({
  root: {
    alignItems: 'center',
    justifyContent: 'center',
  },
  letter: {
    fontFamily: Typography.uiFontMedium,
    includeFontPadding: false,
  },
});