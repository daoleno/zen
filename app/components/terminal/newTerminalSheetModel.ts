export type NewTerminalSheetPanel = "form" | "directory";

export type NewTerminalSheetFormState = {
  cwd: string;
  command: string;
  name: string;
  advanced: boolean;
  panel: NewTerminalSheetPanel;
};

export function createNewTerminalSheetFormState(input: {
  cwd?: string;
  command?: string;
  name?: string;
}): NewTerminalSheetFormState {
  return {
    cwd: input.cwd ?? "",
    command: input.command ?? "",
    name: input.name ?? "",
    advanced: false,
    panel: "form",
  };
}

/** Open in-card directory browsing without a second Modal owner. */
export function openDirectoryPanel(
  state: NewTerminalSheetFormState,
): NewTerminalSheetFormState {
  return {
    ...state,
    panel: "directory",
  };
}

/** Return to the launch form; preserve cwd/command/name/advanced. */
export function returnToFormPanel(
  state: NewTerminalSheetFormState,
): NewTerminalSheetFormState {
  return {
    ...state,
    panel: "form",
  };
}

/** Apply a remote directory selection and return to the form. */
export function selectDirectoryPath(
  state: NewTerminalSheetFormState,
  path: string,
): NewTerminalSheetFormState {
  return {
    ...state,
    cwd: path,
    panel: "form",
  };
}

/**
 * Backdrop / system back while the sheet is open.
 * Directory panel backs to form; form closes the sheet.
 */
export function resolveNewTerminalSheetDismiss(
  panel: NewTerminalSheetPanel,
): "return-to-form" | "close-sheet" {
  return panel === "directory" ? "return-to-form" : "close-sheet";
}
