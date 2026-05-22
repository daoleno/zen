import React from "react";
import type { TerminalThemeChrome } from "../../constants/terminalThemes";
import { TerminalAccessoryShortcutButton } from "./TerminalAccessoryShortcutButton";
import { TERMINAL_ACCESSORY_SHORTCUT_KEYS } from "./TerminalAccessoryShortcutKeys";

interface TerminalAccessoryShortcutListProps {
  chrome: TerminalThemeChrome;
  ctrlArmed: boolean;
  onCtrlToggle(): void;
  onHoldPressIn(sequence: string): void;
  onHoldPressOut(): void;
  onTapSequence(sequence: string): void;
}

export function TerminalAccessoryShortcutList({
  chrome,
  ctrlArmed,
  onCtrlToggle,
  onHoldPressIn,
  onHoldPressOut,
  onTapSequence,
}: TerminalAccessoryShortcutListProps) {
  return (
    <>
      {TERMINAL_ACCESSORY_SHORTCUT_KEYS.map((key) => {
        if (key.type === "modifier") {
          return (
            <TerminalAccessoryShortcutButton
              key="Ctrl"
              label="Ctrl"
              chrome={chrome}
              active={ctrlArmed}
              onPress={onCtrlToggle}
            />
          );
        }

        if (key.type === "hold") {
          return (
            <TerminalAccessoryShortcutButton
              key={key.sequence}
              label={key.label}
              chrome={chrome}
              onPressIn={() => onHoldPressIn(key.sequence)}
              onPressOut={onHoldPressOut}
              delayLongPress={9999}
            />
          );
        }

        return (
          <TerminalAccessoryShortcutButton
            key={key.sequence}
            label={key.label}
            chrome={chrome}
            onPress={() => onTapSequence(key.sequence)}
          />
        );
      })}
    </>
  );
}
