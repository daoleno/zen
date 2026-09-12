import { Alert } from "react-native";

export function desktopEnableMessage(serverName: string): string {
  return `Allow "${serverName}" to view and control this computer's desktop over the encrypted Zen connection? Terminal access stays the same, and you can revoke this phone later in Settings.`;
}

export function confirmDesktopEnable(serverName: string): Promise<boolean> {
  return new Promise((resolve) => {
    Alert.alert("Enable remote desktop?", desktopEnableMessage(serverName),
      [{ text: "Cancel", style: "cancel", onPress: () => resolve(false) },
        { text: "Enable remote desktop", onPress: () => resolve(true) }],
      { cancelable: true, onDismiss: () => resolve(false) });
  });
}
