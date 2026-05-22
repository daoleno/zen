// Keys that fire once per tap.
export type TerminalAccessoryTapKey =
  | { label: "Ctrl"; type: "modifier" }
  | { label: string; type: "tap"; sequence: string };

// Keys that repeat while held.
export type TerminalAccessoryHoldKey = {
  label: string;
  type: "hold";
  sequence: string;
};

export type TerminalAccessoryShortcutKey =
  | TerminalAccessoryTapKey
  | TerminalAccessoryHoldKey;

export const TERMINAL_ACCESSORY_SHORTCUT_KEYS: readonly TerminalAccessoryShortcutKey[] = [
  { label: "Ctrl", type: "modifier" },
  { label: "Esc", type: "tap", sequence: "\x1b" },
  { label: "Tab", type: "tap", sequence: "\t" },
  { label: "⌃B", type: "tap", sequence: "\x02" },
  { label: "⌃C", type: "tap", sequence: "\x03" },
  { label: "⌃D", type: "tap", sequence: "\x04" },
  { label: "←", type: "hold", sequence: "\x1b[D" },
  { label: "↑", type: "hold", sequence: "\x1b[A" },
  { label: "↓", type: "hold", sequence: "\x1b[B" },
  { label: "→", type: "hold", sequence: "\x1b[C" },
];
