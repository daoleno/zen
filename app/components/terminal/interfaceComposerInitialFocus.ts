export type InterfaceComposerInitialFocusGrant = string | null;

export function isInterfaceComposerInitialFocusRouteGrant(value: string) {
  return value === "1";
}

export function reconcileInterfaceComposerInitialFocusGrant(
  currentGrant: InterfaceComposerInitialFocusGrant,
  input: {
    sessionKey: string | null;
    requested: boolean;
  },
): InterfaceComposerInitialFocusGrant {
  if (input.requested && input.sessionKey) {
    return input.sessionKey;
  }
  if (currentGrant && currentGrant !== input.sessionKey) {
    return null;
  }
  return currentGrant;
}

export function consumeInterfaceComposerInitialFocusGrant(
  currentGrant: InterfaceComposerInitialFocusGrant,
  sessionKey: string | null,
): InterfaceComposerInitialFocusGrant {
  return currentGrant === sessionKey ? null : currentGrant;
}

export type InterfaceComposerInitialFocusEffect =
  "ignore" | "wait" | "deliver" | "drop";

export function resolveInterfaceComposerInitialFocusEffect({
  grant,
  handledGrant,
  sessionKey,
  screenActive,
  appActive,
  connected,
}: {
  grant: InterfaceComposerInitialFocusGrant;
  handledGrant: InterfaceComposerInitialFocusGrant;
  sessionKey: string | null;
  screenActive: boolean;
  appActive: boolean;
  connected: boolean;
}): InterfaceComposerInitialFocusEffect {
  if (!grant || grant !== sessionKey || handledGrant === grant) {
    return "ignore";
  }
  if (!appActive) {
    return "drop";
  }
  if (!screenActive || !connected) {
    return "wait";
  }
  return "deliver";
}
