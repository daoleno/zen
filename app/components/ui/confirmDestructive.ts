import { Alert } from "react-native";
import * as Haptics from "expo-haptics";

interface ConfirmDestructiveOptions {
  title: string;
  message?: string;
  /** Destructive button label, e.g. "Remove" or "Terminate". */
  confirmLabel: string;
  onConfirm(): void | Promise<void>;
}

/**
 * The one confirmation for irreversible actions: a native alert whose
 * destructive button is never the default, preceded by a warning haptic.
 */
export function confirmDestructive({
  title,
  message,
  confirmLabel,
  onConfirm,
}: ConfirmDestructiveOptions): void {
  void Haptics.notificationAsync(Haptics.NotificationFeedbackType.Warning);
  Alert.alert(title, message, [
    { text: "Cancel", style: "cancel" },
    {
      text: confirmLabel,
      style: "destructive",
      onPress: () => {
        void onConfirm();
      },
    },
  ]);
}
