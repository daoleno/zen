import { useEffect } from "react";
import { Platform } from "react-native";
import {
  AndroidSoftInputModes,
  KeyboardController,
} from "react-native-keyboard-controller";

let activeStructuredChatLeases = 0;

function applyStructuredChatWindowMode() {
  KeyboardController.setInputMode(
    AndroidSoftInputModes.SOFT_INPUT_ADJUST_NOTHING,
  );
}

function restoreWindowModeAfterEffectsSettle() {
  queueMicrotask(() => {
    if (activeStructuredChatLeases > 0) {
      applyStructuredChatWindowMode();
      return;
    }
    KeyboardController.setDefaultMode();
  });
}

/**
 * Android's manifest keeps adjustResize for forms and terminal input. A focused
 * structured-chat canvas leases adjustNothing so only its overlay follows the
 * IME. The deferred release wins over nested keyboard-controller cleanup and
 * preserves another focused chat lease during host replacement.
 */
export function useStructuredChatWindowMode(enabled: boolean) {
  useEffect(() => {
    if (Platform.OS !== "android" || !enabled) {
      return;
    }

    activeStructuredChatLeases += 1;
    applyStructuredChatWindowMode();

    return () => {
      activeStructuredChatLeases = Math.max(
        0,
        activeStructuredChatLeases - 1,
      );
      restoreWindowModeAfterEffectsSettle();
    };
  }, [enabled]);
}
