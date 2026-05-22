import React from "react";
import type {
  TerminalThemeChrome,
  TerminalThemePalette,
} from "../../constants/terminalThemes";
import type { CodexSlashCommand } from "../../services/websocket";
import { CodexQuickCommandMenu } from "./CodexQuickCommandMenu";

interface CodexChatComposerCommandMenuProps {
  visible: boolean;
  commands: CodexSlashCommand[];
  commandQuery: string;
  chrome: TerminalThemeChrome;
  theme: TerminalThemePalette;
  onSelectCommand(command: CodexSlashCommand): void;
}

export function CodexChatComposerCommandMenu({
  visible,
  commands,
  commandQuery,
  chrome,
  theme,
  onSelectCommand,
}: CodexChatComposerCommandMenuProps) {
  if (!visible) {
    return null;
  }

  return (
    <CodexQuickCommandMenu
      commands={commands}
      commandQuery={commandQuery}
      chrome={chrome}
      theme={theme}
      onSelectCommand={onSelectCommand}
    />
  );
}
