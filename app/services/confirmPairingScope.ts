import { Alert } from "react-native";
import { PAIRING_SCOPE_COPY } from "./pairingScope";

export function confirmPairingScope(): Promise<boolean> {
  return new Promise((resolve) => {
    Alert.alert("Pair this computer?", PAIRING_SCOPE_COPY,
      [{ text: "Cancel", style: "cancel", onPress: () => resolve(false) },
        { text: "Pair and grant access", onPress: () => resolve(true) }],
      { cancelable: true, onDismiss: () => resolve(false) });
  });
}
