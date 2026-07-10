import React, { useMemo } from "react";
import { StyleSheet, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { compactPathLabel } from "../../services/pathDisplay";
import { TelegramChatHeader } from "../terminal/TelegramChatHeader";
import { BrainAdapterIcon } from "./BrainAdapterIcon";
import { brainAdapterLabel } from "./brainPresentation";
import type { BrainAdapterRef } from "../../store/brain";

interface BrainChatHeaderProps {
  chrome: TerminalThemeChrome;
  adapter?: BrainAdapterRef | null;
  sessionName?: string;
  workspace?: string;
  canSwitchAdapter: boolean;
  newChatLoading: boolean;
  canNewChat: boolean;
  canOpenTerminal: boolean;
  canOpenWorkspace: boolean;
  onOpenAdapterSheet: () => void;
  onOpenMenu: () => void;
  onNewChat: () => void;
}

export function BrainChatHeader({
  chrome,
  adapter,
  sessionName,
  workspace,
  canSwitchAdapter,
  newChatLoading,
  canNewChat,
  canOpenTerminal,
  canOpenWorkspace,
  onOpenAdapterSheet,
  onOpenMenu,
  onNewChat,
}: BrainChatHeaderProps) {
  const title = sessionName?.trim() || "Brain";
  const statusLine = [
    brainAdapterLabel(adapter),
    workspace
      ? compactPathLabel(workspace, { tailSegments: 2, showFullUpTo: 2 })
      : "",
  ]
    .filter(Boolean)
    .join(" · ") || "Waiting for connection";

  const rightActions = useMemo(
    () => [
      {
        key: "new-chat",
        icon: newChatLoading
          ? ("hourglass-outline" as const)
          : ("create-outline" as const),
        accessibilityLabel: "New Brain chat",
        disabled: !canNewChat || newChatLoading,
        onPress: onNewChat,
      },
      {
        key: "menu",
        icon: "ellipsis-vertical" as const,
        accessibilityLabel: "Brain actions",
        disabled: !canOpenTerminal && !canOpenWorkspace && !canSwitchAdapter,
        onPress: onOpenMenu,
      },
    ],
    [
      canNewChat,
      canOpenTerminal,
      canOpenWorkspace,
      canSwitchAdapter,
      newChatLoading,
      onNewChat,
      onOpenMenu,
    ],
  );

  const headerAvatar = adapter ? (
    <View style={styles.avatarSlot}>
      <BrainAdapterIcon adapter={adapter} size={22} />
    </View>
  ) : undefined;

  return (
    <TelegramChatHeader
      chrome={chrome}
      title={title}
      subtitle={statusLine}
      avatar={headerAvatar}
      onPressTitle={canSwitchAdapter ? onOpenAdapterSheet : undefined}
      rightActions={rightActions}
      flat
    />
  );
}

const styles = StyleSheet.create({
  avatarSlot: {
    width: 30,
    height: 30,
    alignItems: "center",
    justifyContent: "center",
  },
});
