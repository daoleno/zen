import { useMemo } from 'react';
import { Ionicons, MaterialCommunityIcons } from '@expo/vector-icons';
import FontAwesome5 from '@expo/vector-icons/FontAwesome5';
import FontAwesome6 from '@expo/vector-icons/FontAwesome6';
import type { ComponentProps } from 'react';
import { StyleSheet, View } from 'react-native';
import { Claude, Codex, Grok } from '@lobehub/icons-rn';
import { useAppTheme } from '../../constants/tokens';
import type { ResolvedZenTheme } from '../../theme';
import { surfacesFromTheme } from '../../constants/themedSurfaces';
import type { AgentKind } from '../../services/agentPresentation';
import type { TerminalFlavor } from '../../services/terminalFlavor';
import { FlavorLetterBadge } from './FlavorLetterBadge';
import { CursorMark } from '../icons/CursorMark';

type MaterialIconName = ComponentProps<typeof MaterialCommunityIcons>['name'];
type FontAwesome5Name = ComponentProps<typeof FontAwesome5>['name'];
type FontAwesome6Name = ComponentProps<typeof FontAwesome6>['name'];

interface AgentKindIconProps {
  kind: AgentKind;
  flavor?: TerminalFlavor;
  size?: number;
  variant?: 'compact' | 'avatar';
}

type FlavorPresentation =
  | {
      family: 'material';
      icon: MaterialIconName;
      tint: string;
    }
  | {
      family: 'fa5';
      icon: FontAwesome5Name;
      tint: string;
    }
  | {
      family: 'fa6';
      icon: FontAwesome6Name;
      tint: string;
    }
  | {
      family: 'letter';
      letter: string;
      backgroundColor: string;
      textColor: string;
    };

const FLAVOR_PRESENTATION: Record<Exclude<TerminalFlavor, 'shell'>, FlavorPresentation> = {
  ssh: { family: 'material', icon: 'ssh', tint: '#4F8EF7' },
  go: { family: 'fa6', icon: 'golang', tint: '#00ADD8' },
  rust: { family: 'fa5', icon: 'rust', tint: '#000000' },
  python: { family: 'fa5', icon: 'python', tint: '#3776AB' },
  node: { family: 'fa5', icon: 'node', tint: '#339933' },
  typescript: { family: 'material', icon: 'language-typescript', tint: '#3178C6' },
  bun: {
    family: 'letter',
    letter: 'B',
    backgroundColor: '#141414',
    textColor: '#F9F1E1',
  },
  docker: { family: 'fa5', icon: 'docker', tint: '#2496ED' },
  kubernetes: { family: 'material', icon: 'kubernetes', tint: '#326CE5' },
  postgres: { family: 'material', icon: 'elephant', tint: '#336791' },
  redis: {
    family: 'letter',
    letter: 'R',
    backgroundColor: '#DC382D',
    textColor: '#FFFFFF',
  },
  nginx: {
    family: 'letter',
    letter: 'N',
    backgroundColor: '#009639',
    textColor: '#FFFFFF',
  },
  git: { family: 'fa5', icon: 'git', tint: '#F05032' },
  java: { family: 'fa5', icon: 'java', tint: '#E76F00' },
  ruby: { family: 'material', icon: 'language-ruby', tint: '#CC342D' },
  php: { family: 'fa5', icon: 'php', tint: '#777BB4' },
  terraform: { family: 'material', icon: 'terraform', tint: '#844FBA' },
  aws: { family: 'fa5', icon: 'aws', tint: '#FF9900' },
  bash: { family: 'material', icon: 'bash', tint: '#4EAA25' },
  linux: { family: 'fa5', icon: 'linux', tint: '#FCC624' },
};

export function AgentKindIcon({
  kind,
  flavor = 'shell',
  size = 16,
  variant = 'compact',
}: AgentKindIconProps) {
  const { theme } = useAppTheme();
  const styles = useMemo(() => createStyles(theme), [theme]);
  const avatarSize = variant === 'avatar' ? 40 : null;
  const iconSize = avatarSize ? 24 : size;
  const frameSize = avatarSize ?? size + 8;

  const content = renderContent({
    flavor,
    iconSize,
    kind,
    styles,
    theme,
    variant,
  });

  if (variant === 'avatar') {
    return (
      <View style={[styles.avatarSlot, { width: avatarSize, height: avatarSize }]}>
        {content}
      </View>
    );
  }

  if (kind === 'claude' || kind === 'codex' || kind === 'cursor' || kind === 'grok') {
    return content;
  }

  return (
    <View style={[styles.frame, styles.terminalFrame, { width: frameSize, height: frameSize }]}>
      {content}
    </View>
  );
}

function renderContent({
  kind,
  flavor,
  iconSize,
  styles,
  theme,
  variant,
}: {
  kind: AgentKind;
  flavor: TerminalFlavor;
  iconSize: number;
  styles: ReturnType<typeof createStyles>;
  theme: ResolvedZenTheme;
  variant: 'compact' | 'avatar';
}) {
  if (kind === 'claude') {
    return <Claude.Color size={iconSize} />;
  }

  if (kind === 'codex') {
    return <Codex.Color size={iconSize} />;
  }

  if (kind === 'cursor') {
    return <CursorMark size={iconSize} color={theme.isLight ? '#000' : '#fff'} />;
  }

  if (kind === 'grok') {
    const grokFrameSize = iconSize + 8;
    return (
      <View
        style={[
          styles.frame,
          styles.grokFrame,
          { width: grokFrameSize, height: grokFrameSize },
        ]}
      >
        <Grok size={iconSize} color={theme.colors.textPrimary} />
      </View>
    );
  }

  if (flavor !== 'shell') {
    const presentation = FLAVOR_PRESENTATION[flavor];

    if (presentation.family === 'letter') {
      return (
        <FlavorLetterBadge
          letter={presentation.letter}
          backgroundColor={presentation.backgroundColor}
          textColor={presentation.textColor}
          size={iconSize}
        />
      );
    }

    const flavorFrameSize = iconSize + 10;
    return (
      <View
        style={[
          styles.frame,
          styles.flavorFrame,
          {
            width: flavorFrameSize,
            height: flavorFrameSize,
            backgroundColor: withAlpha(presentation.tint, 0.14),
            borderColor: withAlpha(presentation.tint, 0.28),
          },
        ]}
      >
        {renderFlavorIcon(presentation, iconSize)}
      </View>
    );
  }

  const shellIcon = (
    <Ionicons
      name="terminal-outline"
      size={iconSize}
      color={theme.colors.textSecondary}
    />
  );

  if (variant === 'avatar') {
    return (
      <View
        style={[
          styles.frame,
          styles.terminalFrame,
          { width: iconSize + 10, height: iconSize + 10 },
        ]}
      >
        {shellIcon}
      </View>
    );
  }

  return shellIcon;
}

function renderFlavorIcon(presentation: Exclude<FlavorPresentation, { family: 'letter' }>, iconSize: number) {
  switch (presentation.family) {
    case 'material':
      return (
        <MaterialCommunityIcons
          name={presentation.icon}
          size={iconSize}
          color={presentation.tint}
        />
      );
    case 'fa5':
      return (
        <FontAwesome5
          name={presentation.icon}
          brand
          size={iconSize}
          color={presentation.tint}
        />
      );
    case 'fa6':
      return (
        <FontAwesome6
          name={presentation.icon}
          brand
          size={iconSize}
          color={presentation.tint}
        />
      );
  }
}

function withAlpha(hex: string, alpha: number): string {
  const normalized = hex.replace('#', '');
  if (normalized.length !== 6) {
    return hex;
  }
  const value = Math.round(alpha * 255)
    .toString(16)
    .padStart(2, '0');
  return `#${normalized}${value}`;
}

function createStyles(theme: ResolvedZenTheme) {
  const { subtle: themedSubtle, border: themedBorder } = surfacesFromTheme(theme);

  return StyleSheet.create({
    avatarSlot: {
      alignItems: 'center',
      justifyContent: 'center',
      overflow: 'hidden',
    },
    frame: {
      borderRadius: 12,
      alignItems: 'center',
      justifyContent: 'center',
    },
    terminalFrame: {
      backgroundColor: themedSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: themedBorder,
    },
    grokFrame: {
      backgroundColor: themedSubtle,
      borderWidth: StyleSheet.hairlineWidth,
      borderColor: themedBorder,
    },
    flavorFrame: {
      borderWidth: StyleSheet.hairlineWidth,
    },
  });
}
