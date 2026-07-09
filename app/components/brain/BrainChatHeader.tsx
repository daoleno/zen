import React, { useMemo } from "react";
import { StyleSheet, View } from "react-native";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TelegramChatHeader } from "../terminal/TelegramChatHeader";
import { BrainAdapterIcon } from "./BrainAdapterIcon";
import { brainStatusLine } from "./brainPresentation";
import type { BrainAdapterRef } from "../../store/brain";

interface BrainChatHeaderProps {
  chrome: TerminalThemeChrome;
  adapter?: BrainAdapterRef | null;
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
  canSwitchAdapter,
  newChatLoading,
  canNewChat,
  canOpenTerminal,
  canOpenWorkspace,
  onOpenAdapterSheet,
  onOpenMenu,
  onNewChat,
}: BrainChatHeaderProps) {
  const statusLine = brainStatusLine({ adapter });

  // Keep new-chat + overflow in one circular actions chip so chrome matches
  // the identity pill language (no bare icons next to a framed title).
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
    width: 30,
    height: 30,
    alignItems: "center",
    justifyContent: "center",
  },
});
