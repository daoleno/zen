// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { resolveTerminalSurfaceMountPolicy } from "../../app/terminal/useTerminalViewportModel";

describe("resolveTerminalSurfaceMountPolicy", () => {
  test("keeps the terminal surface mounted under chat while focused", () => {
    expect(
      resolveTerminalSurfaceMountPolicy({
        canRenderTerminal: true,
        screenFocused: true,
        showInterfaceChat: true,
      }),
    ).toEqual({
      shouldMountTerminalSurface: true,
      terminalSurfaceActive: false,
      accessoryVisible: false,
    });
  });

  test("activates the surface for terminal mode while focused", () => {
    expect(
      resolveTerminalSurfaceMountPolicy({
        canRenderTerminal: true,
        screenFocused: true,
        showInterfaceChat: false,
      }),
    ).toEqual({
      shouldMountTerminalSurface: true,
      terminalSurfaceActive: true,
      accessoryVisible: true,
    });
  });

  test("unmounts only when the screen is unfocused or terminal cannot render", () => {
    expect(
      resolveTerminalSurfaceMountPolicy({
        canRenderTerminal: true,
        screenFocused: false,
        showInterfaceChat: false,
      }),
    ).toEqual({
      shouldMountTerminalSurface: false,
      terminalSurfaceActive: false,
      accessoryVisible: false,
    });

    expect(
      resolveTerminalSurfaceMountPolicy({
        canRenderTerminal: false,
        screenFocused: true,
        showInterfaceChat: false,
      }),
    ).toEqual({
      shouldMountTerminalSurface: false,
      terminalSurfaceActive: false,
      accessoryVisible: false,
    });
  });
});
