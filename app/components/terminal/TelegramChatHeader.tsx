import React, { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import type { TerminalThemeChrome } from '../../constants/terminalThemes';
import {
  Colors,
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppColors,
} from '../../constants/tokens';
import { AnimatedPressable } from '../ui/AnimatedPressable';
import type { AgentKind } from '../../services/agentPresentation';
import type { TerminalFlavor } from '../../services/terminalFlavor';
import { AgentKindIcon } from './AgentKindIcon';
import { SessionAvatar } from '../ui/SessionAvatar';

interface TelegramChatHeaderAction {
  key: string;
  icon: React.ComponentProps<typeof Ionicons>['name'];
  accessibilityLabel: string;
  disabled?: boolean;
  onPress: () => void;
}

interface TelegramChatHeaderProps {
  /** When set, header shares the chat canvas instead of app shell colors. */
  chrome?: TerminalThemeChrome;
  title: string;
  subtitle?: string;
  avatarLabel?: string;
  avatarSeed?: string;
  agentKind?: AgentKind;
  terminalFlavor?: TerminalFlavor;
  avatar?: React.ReactNode;
  onBack?: () => void;
  onPressTitle?: () => void;
  rightActions?: TelegramChatHeaderAction[];
  menuAnchorRef?: React.RefObject<View | null>;
}

export function TelegramChatHeader({
  chrome,
  title,
  subtitle,
  avatarLabel,
  avatarSeed,
  agentKind,
  terminalFlavor,
  avatar,
  onBack,
  onPressTitle,
  rightActions = [],
  menuAnchorRef,
}: TelegramChatHeaderProps) {
  const colors = useAppColors();
  const styles = useMemo(
    () => createStyles(colors, chrome),
    [chrome, colors],
  );
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
        {avatar ? (
          avatar
        ) : agentKind ? (
          <AgentKindIcon
            kind={agentKind}
            flavor={terminalFlavor}
            variant="avatar"
          />
        ) : (
          <SessionAvatar label={avatarText} seed={avatarKey} size={40} />
        )}
        <View style={styles.copy}>
          <Text style={styles.title} numberOfLines={1}>
            {title}
          </Text>
          {subtitle ? (
            <Text
              style={styles.subtitle}
              numberOfLines={1}
              ellipsizeMode="head"
            >
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

function createStyles(
  colors: typeof Colors,
  chrome?: TerminalThemeChrome,
) {
  const onChatCanvas = Boolean(chrome);
  return StyleSheet.create({
    root: {
      flexDirection: 'row',
      alignItems: 'center',
      paddingHorizontal: 8,
      paddingTop: 4,
      paddingBottom: 8,
      gap: 2,
      backgroundColor: chrome?.appBackground ?? colors.bgSurface,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: chrome?.border ?? colors.borderSubtle,
    },
    backButton: {
      width: 40,
      height: 40,
      borderRadius: 20,
      alignItems: 'center',
      justifyContent: 'center',
      backgroundColor: onChatCanvas ? 'transparent' : colors.surfaceSubtle,
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
      ...UiTextMetrics,
      color: colors.textPrimary,
      fontFamily: Typography.uiFontMedium,
      fontSize: 16,
      lineHeight: uiLineHeight(16),
    },
    subtitle: {
      ...UiTextMetrics,
      color: colors.textSecondary,
      fontFamily: Typography.uiFont,
      fontSize: 13,
      lineHeight: uiLineHeight(13),
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