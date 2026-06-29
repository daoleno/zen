import React, { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import { Colors, Typography, useAppColors } from '../../constants/tokens';
import { AnimatedPressable } from '../ui/AnimatedPressable';
import { SessionAvatar } from '../ui/SessionAvatar';

interface TelegramChatHeaderAction {
  key: string;
  icon: React.ComponentProps<typeof Ionicons>['name'];
  accessibilityLabel: string;
  disabled?: boolean;
  onPress: () => void;
}

interface TelegramChatHeaderProps {
  title: string;
  subtitle?: string;
  avatarLabel?: string;
  avatarSeed?: string;
  onBack?: () => void;
  onPressTitle?: () => void;
  rightActions?: TelegramChatHeaderAction[];
  menuAnchorRef?: React.RefObject<View | null>;
}

export function TelegramChatHeader({
  title,
  subtitle,
  avatarLabel,
  avatarSeed,
  onBack,
  onPressTitle,
  rightActions = [],
  menuAnchorRef,
}: TelegramChatHeaderProps) {
  const colors = useAppColors();
  const styles = useMemo(() => createStyles(colors), [colors]);
  const avatarText = avatarLabel ?? title;
  const avatarKey = avatarSeed ?? title;

  return (
    <View style={styles.root}>
      {onBack ? (
        <AnimatedPressable
          accessibilityRole="button"
          accessibilityLabel="Back"
          style={styles.backButton}
          preset="press"
          scale={0.92}
          onPress={onBack}
        >
          <Ionicons name="chevron-back" size={22} color={colors.textPrimary} />
        </AnimatedPressable>
      ) : (
        <View style={styles.backSpacer} />
      )}

      <AnimatedPressable
        accessibilityRole="button"
        accessibilityLabel={onPressTitle ? `${title}, open session` : title}
        disabled={!onPressTitle}
        style={styles.identity}
        preset="press"
        scale={0.99}
        onPress={() => {
          if (!onPressTitle) {
            return;
          }
          Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
          onPressTitle();
        }}
      >
        <SessionAvatar label={avatarText} seed={avatarKey} size={40} />
        <View style={styles.copy}>
          <Text style={styles.title} numberOfLines={1}>
            {title}
          </Text>
          {subtitle ? (
            <Text style={styles.subtitle} numberOfLines={1}>
              {subtitle}
            </Text>
          ) : null}
        </View>
      </AnimatedPressable>

      <View style={styles.actions}>
        {rightActions.map((action) => {
          const button = (
            <AnimatedPressable
              accessibilityRole="button"
              accessibilityLabel={action.accessibilityLabel}
              accessibilityState={{ disabled: action.disabled }}
              disabled={action.disabled}
              style={[styles.actionButton, action.disabled && styles.actionDisabled]}
              preset="press"
              scale={0.9}
              onPress={() => {
                if (action.disabled) {
                  return;
                }
                Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light);
                action.onPress();
              }}
            >
              <Ionicons
                name={action.icon}
                size={20}
                color={action.disabled ? colors.disabledText : colors.textSecondary}
              />
            </AnimatedPressable>
          );

          if (action.key === 'menu' && menuAnchorRef) {
            return (
              <View key={action.key} ref={menuAnchorRef} collapsable={false}>
                {button}
              </View>
            );
          }

          return <React.Fragment key={action.key}>{button}</React.Fragment>;
        })}
      </View>
    </View>
  );
}

function createStyles(colors: typeof Colors) {
  return StyleSheet.create({
    root: {
      flexDirection: 'row',
      alignItems: 'center',
      paddingHorizontal: 8,
      paddingTop: 4,
      paddingBottom: 8,
      gap: 2,
      backgroundColor: colors.bgSurface,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: colors.borderSubtle,
    },
    backButton: {
      width: 40,
      height: 40,
      borderRadius: 20,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: colors.surfaceSubtle,
    },
    backSpacer: {
      width: 8,
    },
    identity: {
      flex: 1,
      minWidth: 0,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 10,
      paddingVertical: 2,
      paddingRight: 4,
    },
    copy: {
      flex: 1,
      minWidth: 0,
      justifyContent: 'center',
      gap: 1,
    },
    title: {
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 16,
      lineHeight: 20,
    },
    subtitle: {
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      lineHeight: 17,
    },
    actions: {
      flexDirection: 'row',
      alignItems: 'center',
      gap: 0,
    },
    actionButton: {
      width: 40,
      height: 40,
      borderRadius: 20,
      alignItems: 'center',
      justifyContent: 'center',
    },
    actionDisabled: {
      opacity: 0.45,
    },
  });
}