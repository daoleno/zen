import type { TerminalThemePalette } from '../../constants/terminalThemes';

export interface TerminalSurfaceHandle {
  sendInput(data: string, options?: { focus?: boolean }): void;
  focus(): void;
  blur(): void;
  /** Force viewport sync + draw after focus/visibility restore (no input focus). */
  wakeRenderer(): void;
  resumeInput(): void;
  scrollToBottom(): void;
}

export interface TerminalSurfaceProps {
  serverId: string;
  targetId: string;
  backend?: string;
  theme: TerminalThemePalette;
  ctrlArmed?: boolean;
  onCtrlArmedChange?: (next: boolean) => void;
}
