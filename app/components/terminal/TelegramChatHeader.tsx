import React, { useMemo } from 'react';
import { StyleSheet, Text, View } from 'react-native';
import { Ionicons } from '@expo/vector-icons';
import * as Haptics from 'expo-haptics';
import type { TerminalThemeChrome } from '../../constants/terminalThemes';
import {
  Radii,
  TypeScale,
  Typography,
  UiTextMetrics,
  useAppColors,
  type AppColors,
  type WorkerStatus,
} from '../../constants/tokens';
import { GlassSurface } from '../ui/GlassSurface';
import { workerStatusLabel } from '../../services/workerStatusPresentation';
import { AnimatedPressable } from '../ui/AnimatedPressable';
import type { AgentKind } from '../../services/workerPresentation';
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
  flat?: boolean;
  /** Live Session state shown as a dot on the avatar. */
  status?: WorkerStatus;
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
  flat = false,
  status,
}: TelegramChatHeaderProps) {
  const colors = useAppColors();
  const styles = useMemo(
    () => createStyles(colors, chrome),
    [chrome, colors],
  );
  const avatarText = avatarLabel ?? title;
  const avatarKey = avatarSeed ?? title;
  const statusColor = status ? headerStatusColor(status, colors) : null;
  const glass = flat ? null : styles.glass;
  // Capsules keep the chat canvas' own contrast logic; GlassSurface only
  // supplies the material, hairline and lit edge.
  const Capsule = flat ? View : GlassSurface;
  const capsuleProps = flat
    ? {}
    : {
        material: 'chrome' as const,
        radius: CHAT_HEADER_HEIGHT / 2,
        elevation: 'card' as const,
        fill: glass?.backgroundColor,
        stroke: glass?.borderColor,
      };

  return (
    <View style={[styles.outer, flat ? styles.outerFlat : null]}>
      <View style={[styles.row, flat ? styles.rowFlat : null]}>
        {onBack ? (
          <Capsule
            {...capsuleProps}
            style={[
              styles.chip,
              styles.circleChip,
              flat ? styles.flatChrome : null,
            ]}
          >
            <AnimatedPressable
              accessibilityRole="button"
              accessibilityLabel="Back"
              style={[styles.iconButton, flat ? styles.iconButtonFlat : null]}
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
          </Capsule>
        ) : null}

        <Capsule
          {...capsuleProps}
          style={[
            styles.chip,
            styles.identityCapsule,
            flat ? styles.identityFlat : null,
          ]}
        >
        <AnimatedPressable
          accessibilityRole="button"
          accessibilityLabel={[
            title,
            status ? workerStatusLabel(status) : null,
            onPressTitle ? 'Session details and resource usage' : null,
          ]
            .filter(Boolean)
            .join(', ')}
          disabled={!onPressTitle}
          style={styles.identityPill}
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
          <View>
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
            {statusColor ? (
              <View
                pointerEvents="none"
                accessibilityElementsHidden
                importantForAccessibility="no-hide-descendants"
                style={[
                  styles.statusBadge,
                  {
                    backgroundColor: statusColor,
                    borderColor: styles.glass.backgroundColor,
                  },
                ]}
              />
            ) : null}
          </View>
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
        </Capsule>

        {rightActions.length > 0 ? (
          <View
            ref={menuAnchorRef}
            collapsable={false}
            style={styles.actionsAnchor}
          >
          <Capsule
            {...capsuleProps}
            style={[
              styles.chip,
              styles.actionsChip,
              flat ? styles.actionsFlat : null,
            ]}
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
                  flat ? styles.iconButtonFlat : null,
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
          </Capsule>
          </View>
        ) : null}
      </View>
    </View>
  );
}

function headerStatusColor(status: WorkerStatus, colors: AppColors): string | null {
  switch (status) {
    case 'running':
      return colors.statusRunning;
    case 'blocked':
      return colors.statusBlocked;
    case 'failed':
      return colors.statusFailed;
    default:
      return null;
  }
}

function resolveChipSurface(
  colors: AppColors,
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

function createStyles(colors: AppColors, chrome?: TerminalThemeChrome) {
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
    outerFlat: {
      paddingTop: 0,
      paddingBottom: 0,
      backgroundColor: chrome?.appBackground ?? colors.bgPrimary,
      borderBottomWidth: StyleSheet.hairlineWidth,
      borderBottomColor: borderColor,
    },
    row: {
      flexDirection: 'row',
      alignItems: 'center',
      height: CHAT_HEADER_HEIGHT,
      gap: 8,
    },
    rowFlat: {
      height: 52,
    },
    glass: {
      backgroundColor: chipSurface,
      borderColor,
    },
    chip: {
      height: CHAT_HEADER_HEIGHT,
      flexDirection: 'row',
      alignItems: 'center',
      overflow: 'hidden',
    },
    actionsAnchor: {
      flexShrink: 0,
    },
    identityCapsule: {
      flex: 1,
      minWidth: 0,
    },
    statusBadge: {
      position: 'absolute',
      right: -1,
      bottom: -1,
      width: 12,
      height: 12,
      borderRadius: 6,
      borderWidth: 2,
    },
    circleChip: {
      width: CHAT_HEADER_HEIGHT,
      justifyContent: 'center',
    },
    flatChrome: {
      height: 52,
      backgroundColor: 'transparent',
      borderWidth: 0,
      borderRadius: 0,
    },
    identityPill: {
      flex: 1,
      minWidth: 0,
      alignSelf: 'stretch',
      flexDirection: 'row',
      alignItems: 'center',
      gap: 10,
      paddingLeft: 4,
      paddingRight: 14,
      opacity: 1,
    },
    identityFlat: {
      height: 52,
      paddingLeft: 2,
      paddingRight: 8,
      backgroundColor: 'transparent',
      borderWidth: 0,
      borderRadius: 0,
    },
    actionsChip: {
      flexShrink: 0,
      paddingHorizontal: 2,
    },
    actionsFlat: {
      height: 52,
      paddingHorizontal: 0,
      backgroundColor: 'transparent',
      borderWidth: 0,
      borderRadius: 0,
    },
    iconButton: {
      width: 44,
      height: 44,
      borderRadius: Radii.pill,
      alignItems: 'center',
      justifyContent: 'center',
      opacity: 1,
    },
    iconButtonFlat: {
      width: 44,
      height: 44,
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
      ...TypeScale.body,
      color: titleColor,
      fontFamily: Typography.uiFontMedium,
    },
    subtitle: {
      ...UiTextMetrics,
      ...TypeScale.micro,
      color: subtitleColor,
      fontFamily: Typography.uiFont,
      fontWeight: '400',
    },
  });
}
