import React, { useMemo } from "react";
import { StyleSheet, View } from "react-native";
import { TelegramChatHeader } from "../terminal/TelegramChatHeader";
import { BrainAdapterIcon } from "./BrainAdapterIcon";
import { brainStatusLine } from "./brainPresentation";
import type { BrainAdapterRef } from "../../store/brain";

interface BrainChatHeaderProps {
  adapter?: BrainAdapterRef | null;
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
  adapter,
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
  const statusLine = brainStatusLine({ adapter, workspace });

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
      title="Brain"
      subtitle={statusLine}
      avatar={headerAvatar}
      onPressTitle={canSwitchAdapter ? onOpenAdapterSheet : undefined}
      rightActions={rightActions}
    />
  );
}

const styles = StyleSheet.create({
  avatarSlot: {
    width: 40,
    height: 40,
    alignItems: "center",
    justifyContent: "center",
  },
});