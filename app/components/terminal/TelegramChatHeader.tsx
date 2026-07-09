import React, { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import type { TerminalThemeChrome } from '../../constants/terminalThemes';
import {
  Colors,
  Radii,
  Typography,
  UiTextMetrics,
  uiLineHeight,
  useAppColors,
} from '../../constants/tokens';
import { AnimatedPressable } from '../ui/AnimatedPressable';
import type { AgentKind } from '../../services/agentPresentation';
import type { TerminalFlavor } from '../../services/terminalFlavor';
import { AgentKindIcon } from './AgentKindIcon';
import {
  CHAT_CHROME_HORIZONTAL_INSET,
  CHAT_HEADER_HEIGHT,
  CHAT_HEADER_OUTER_GAP,
} from './chatChromeMetrics';
import { SessionAvatar } from '../ui/SessionAvatar';
import { relativeLuminance } from '../../theme/colorUtils';

interface TelegramChatHeaderAction {
  key: string;
  icon: React.ComponentProps<typeof Ionicons>['name'];
  accessibilityLabel: string;
  disabled?: boolean;
  /** Optional override; defaults to muted header icon color. */
  iconColor?: string;
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
    <View style={styles.outer}>
      <View style={styles.row}>
        {onBack ? (
          <View style={[styles.chip, styles.circleChip]}>
            <AnimatedPressable
              accessibilityRole="button"
              accessibilityLabel="Back"
              style={styles.iconButton}
              preset="press"
              scale={0.92}
              onPress={onBack}
            >
              <Ionicons
                name="chevron-back"
                size={22}
                color={styles.iconColor.color}
              />
            </AnimatedPressable>
          </View>
        ) : null}

        <AnimatedPressable
          accessibilityRole="button"
          accessibilityLabel={onPressTitle ? `${title}, open session` : title}
          disabled={!onPressTitle}
          style={[styles.chip, styles.identityPill]}
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
            <SessionAvatar label={avatarText} seed={avatarKey} size={30} />
          )}
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

        {rightActions.length > 0 ? (
          <View
            ref={menuAnchorRef}
            collapsable={false}
            style={[styles.chip, styles.actionsChip]}
          >
            {rightActions.map((action) => (
              <AnimatedPressable
                key={action.key}
                accessibilityRole="button"
                accessibilityLabel={action.accessibilityLabel}
                accessibilityState={{ disabled: action.disabled }}
                disabled={action.disabled}
                style={[
                  styles.iconButton,
                  action.disabled && styles.actionDisabled,
                ]}
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
                  color={
                    action.disabled
                      ? colors.disabledText
                      : action.iconColor ?? styles.iconMuted.color
                  }
                />
              </AnimatedPressable>
            ))}
          </View>
        ) : null}
      </View>
    </View>
  );
}

function resolveChipSurface(
  colors: typeof Colors,
  chrome?: TerminalThemeChrome,
): string {
  if (!chrome) {
    return colors.bgSurface;
  }
  const canvas = chrome.appBackground;
  const candidates = [chrome.composerInput, chrome.surface, colors.bgSurface];
  for (const candidate of candidates) {
    if (!candidate.startsWith('#') || !canvas.startsWith('#')) {
      continue;
    }
    if (
      Math.abs(relativeLuminance(candidate) - relativeLuminance(canvas)) >= 0.04
    ) {
      return candidate;
    }
  }
  return colors.bgSurface;
}

function createStyles(colors: typeof Colors, chrome?: TerminalThemeChrome) {
  const chipSurface = resolveChipSurface(colors, chrome);
  const titleColor = chrome?.text ?? colors.textPrimary;
  const subtitleColor = chrome?.textMuted ?? colors.textSecondary;
  const iconColor = chrome?.text ?? colors.textPrimary;
  const iconMuted = chrome?.textMuted ?? colors.textSecondary;
  const borderColor = chrome?.border ?? colors.borderSubtle;

  return StyleSheet.create({
    outer: {
      paddingHorizontal: CHAT_CHROME_HORIZONTAL_INSET,
      paddingTop: CHAT_HEADER_OUTER_GAP,
      paddingBottom: CHAT_HEADER_OUTER_GAP,
      backgroundColor: 'transparent',
      zIndex: 3,
    },
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      height: CHAT_HEADER_HEIGHT,
      gap: 8,
    },
    chip: {
      height: CHAT_HEADER_HEIGHT,
      flexDirection: 'row',
      alignItems: 'center',
      backgroundColor: chipSurface,
      borderRadius: Radii.pill,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor,
      overflow: 'hidden',
    },
    circleChip: {
      width: CHAT_HEADER_HEIGHT,
      justifyContent: 'center',
    },
    identityPill: {
      flex: 1,
      minWidth: 0,
      gap: 8,
      paddingLeft: 6,
      paddingRight: 14,
    },
    actionsChip: {
      flexShrink: 0,
      paddingHorizontal: 2,
    },
    iconButton: {
      width: 40,
      height: 40,
      borderRadius: Radii.pill,
      alignItems: 'center',
      justifyContent: 'center',
    },
    iconColor: {
      color: iconColor,
    },
    iconMuted: {
      color: iconMuted,
    },
    copy: {
      flex: 1,
      minWidth: 0,
      justifyContent: 'center',
      gap: 0,
    },
    title: {
      ...UiTextMetrics,
      color: titleColor,
      fontFamily: Typography.uiFontMedium,
      fontSize: 15,
      lineHeight: uiLineHeight(15),
    },
    subtitle: {
      ...UiTextMetrics,
      color: subtitleColor,
      fontFamily: Typography.uiFont,
      fontSize: 11,
      lineHeight: uiLineHeight(11),
    },
    actionDisabled: {
      opacity: 0.45,
    },
  });
}
