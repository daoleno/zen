import type { TerminalThemeName, TerminalThemePalette } from '../../constants/terminalThemes';

export interface TerminalSurfaceHandle {
  sendInput(data: string, options?: { focus?: boolean }): void;
  focus(): void;
  blur(): void;
  resumeInput(): void;
  scrollToBottom(): void;
}

export interface TerminalSurfaceProps {
  serverId: string;
  targetId: string;
  backend?: string;
  themeName?: TerminalThemeName;
  themeOverrides?: Partial<TerminalThemePalette>;
  ctrlArmed?: boolean;
  onCtrlArmedChange?: (next: boolean) => void;
}
