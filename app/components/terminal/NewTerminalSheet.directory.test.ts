import { describe, expect, test } from "bun:test";
import {
  beginDirectoryLoad,
  completeDirectoryLoad,
  createIdleDirectoryPickerState,
  failDirectoryLoad,
  nextDirectoryListEpoch,
  parentDirectoryPath,
  shouldApplyDirectoryListResult,
} from "./directoryPickerState";
import {
  createNewTerminalSheetFormState,
  openDirectoryPanel,
  resolveNewTerminalSheetDismiss,
  returnToFormPanel,
  selectDirectoryPath,
} from "./newTerminalSheetModel";

describe("NewTerminalSheet panel model", () => {
  test("opens directory browsing inside the same sheet without dropping form fields", () => {
    const started = createNewTerminalSheetFormState({
      cwd: "/home/zen/project",
      command: "codex",
      name: "work",
    });
    const advanced = { ...started, advanced: true };
    const browsing = openDirectoryPanel(advanced);

    expect(browsing).toEqual({
      cwd: "/home/zen/project",
      command: "codex",
      name: "work",
      advanced: true,
      panel: "directory",
    });
    expect(resolveNewTerminalSheetDismiss(browsing.panel)).toBe(
      "return-to-form",
    );
  });

  test("select and back restore the form while preserving cwd/command/name", () => {
    const browsing = openDirectoryPanel(
      createNewTerminalSheetFormState({
        cwd: "/home/zen/project",
        command: "claude",
        name: "agent",
      }),
    );

    expect(selectDirectoryPath(browsing, "/home/zen/project/app")).toEqual({
      cwd: "/home/zen/project/app",
      command: "claude",
      name: "agent",
      advanced: false,
      panel: "form",
    });

    expect(returnToFormPanel(browsing)).toEqual({
      cwd: "/home/zen/project",
      command: "claude",
      name: "agent",
      advanced: false,
      panel: "form",
    });
    expect(resolveNewTerminalSheetDismiss("form")).toBe("close-sheet");
  });
});

describe("remote directory list_dir model", () => {
  test("loads entries and walks daemon-host parent paths", () => {
    let state = createIdleDirectoryPickerState();
    state = beginDirectoryLoad(state);
    expect(state.loading).toBe(true);

    state = completeDirectoryLoad(state, {
      path: "/home/zen/project",
      entries: [
        { name: "app", path: "/home/zen/project/app" },
        { name: "daemon", path: "/home/zen/project/daemon" },
      ],
    });
    expect(state.currentPath).toBe("/home/zen/project");
    expect(state.entries).toHaveLength(2);
    expect(parentDirectoryPath(state.currentPath)).toBe("/home/zen");
    expect(parentDirectoryPath("/")).toBe("/");

    state = beginDirectoryLoad(state);
    state = failDirectoryLoad(state, "list_dir failed");
    expect(state.error).toBe("list_dir failed");
    expect(state.currentPath).toBe("/home/zen/project");
  });

  test("discards stale listDir responses after a newer request epoch", () => {
    let epoch = 0;
    const first = nextDirectoryListEpoch(epoch);
    epoch = first;
    const second = nextDirectoryListEpoch(epoch);
    epoch = second;

    expect(shouldApplyDirectoryListResult(first, epoch)).toBe(false);
    expect(shouldApplyDirectoryListResult(second, epoch)).toBe(true);

    // Leaving the directory panel bumps the epoch so an old path cannot win.
    epoch = nextDirectoryListEpoch(epoch);
    expect(shouldApplyDirectoryListResult(second, epoch)).toBe(false);
  });

  test("discards an old path or server response after a newer listDir starts", () => {
    let epoch = 0;
    const forOldPath = nextDirectoryListEpoch(epoch);
    epoch = forOldPath;
    const forNewPath = nextDirectoryListEpoch(epoch);
    epoch = forNewPath;

    // Response for /old arrives after /new was requested.
    expect(shouldApplyDirectoryListResult(forOldPath, epoch)).toBe(false);
    expect(shouldApplyDirectoryListResult(forNewPath, epoch)).toBe(true);

    const forNewServer = nextDirectoryListEpoch(epoch);
    epoch = forNewServer;
    expect(shouldApplyDirectoryListResult(forNewPath, epoch)).toBe(false);
    expect(shouldApplyDirectoryListResult(forNewServer, epoch)).toBe(true);
  });

  test("sheet close and unmount invalidate in-flight listDir epochs", () => {
    let epoch = 0;
    const inFlight = nextDirectoryListEpoch(epoch);
    epoch = inFlight;
    // handleCloseSheet / handleSheetDismiss / effect cleanup bump immediately.
    epoch = nextDirectoryListEpoch(epoch);
    expect(shouldApplyDirectoryListResult(inFlight, epoch)).toBe(false);
    // Unmount-style second bump still discards the same in-flight response.
    epoch = nextDirectoryListEpoch(epoch);
    expect(shouldApplyDirectoryListResult(inFlight, epoch)).toBe(false);
  });
});
