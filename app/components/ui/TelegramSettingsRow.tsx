import React from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import type { ComponentProps } from 'react';
import { Colors, Typography, useAppColors } from '../../constants/tokens';
import { AnimatedPressable } from './AnimatedPressable';

type IoniconName = ComponentProps<typeof Ionicons>['name'];

interface TelegramSettingsRowProps {
  icon: IoniconName;
  iconColor: string;
  title: string;
  subtitle?: string;
  onPress?: () => void;
  showChevron?: boolean;
  disabled?: boolean;
  isLast?: boolean;
}

export function TelegramSettingsRow({
  icon,
  iconColor,
  title,
  subtitle,
  onPress,
  showChevron = Boolean(onPress),
  disabled = false,
  isLast = false,
}: TelegramSettingsRowProps) {
  const colors = useAppColors();
  const styles = createStyles(colors);

  const content = (
    <>
      <View style={[styles.iconWrap, { backgroundColor: iconColor }]}>
        <Ionicons name={icon} size={18} color={colors.textOnAccent} />
      </View>
      <View style={styles.copy}>
        <Text style={styles.title}>{title}</Text>
        {subtitle ? (
          <Text style={styles.subtitle} numberOfLines={1}>
            {subtitle}
          </Text>
        ) : null}
      </View>
      {showChevron ? (
        <Ionicons name="chevron-forward" size={16} color={colors.textTertiary} />
      ) : null}
    </>
  );

  if (!onPress) {
    return (
      <View style={[styles.row, !isLast && styles.rowBorder]}>
        {content}
      </View>
    );
  }

  return (
    <AnimatedPressable
      style={[styles.row, !isLast && styles.rowBorder]}
      preset="press"
      scale={0.99}
      onPress={onPress}
      disabled={disabled}
    >
      {content}
    </AnimatedPressable>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 14,
      minHeight: 56,
      paddingHorizontal: 16,
      paddingVertical: 10,
    },
    rowBorder: {
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    iconWrap: {
      width: 34,
      height: 34,
      borderRadius: 17,
      alignItems: 'center',
      justifyContent: 'center',
    },
    copy: {
      flex: 1,
      minWidth: 0,
      gap: 1,
    },
    title: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 15.5,
      lineHeight: 20,
    },
    subtitle: {
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      lineHeight: 17,
    },
  });
}