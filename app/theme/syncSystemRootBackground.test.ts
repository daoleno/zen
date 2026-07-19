import { describe, expect, test } from "bun:test";
import { resolveTheme } from "./resolve";
import { syncSystemRootBackground } from "./syncSystemRootBackground";

describe("mobile system root background sync", () => {
  test("forwards the exact resolved dark and light bgPrimary to the setter", async () => {
    const dark = resolveTheme({ colorScheme: "dark" });
    const light = resolveTheme({ colorScheme: "light" });
    const darkOnLightSystem = resolveTheme({
      colorScheme: "light",
      themeId: "classic-dark",
    });
    const calls: string[] = [];
    const setBackgroundColorAsync = async (color: string) => {
      calls.push(color);
    };

    await syncSystemRootBackground(dark.colors.bgPrimary, {
      setBackgroundColorAsync,
    });
    await syncSystemRootBackground(light.colors.bgPrimary, {
      setBackgroundColorAsync,
    });
    await syncSystemRootBackground(darkOnLightSystem.colors.bgPrimary, {
      setBackgroundColorAsync,
    });

    expect(calls).toEqual([
      dark.colors.bgPrimary,
      light.colors.bgPrimary,
      dark.colors.bgPrimary,
    ]);
    expect(calls[0]).toBe("#0F0F14");
    expect(calls[1]).toBe("#F7F8F6");
    expect(darkOnLightSystem.colorScheme).toBe("dark");
  });

  test("invokes the shared mobile setter path without platform branching", async () => {
    let called = false;
    await syncSystemRootBackground("#0F0F14", {
      setBackgroundColorAsync: async () => {
        called = true;
      },
    });
    expect(called).toBe(true);
  });

  test("setter rejection stays best-effort and does not reject", async () => {
    const result = syncSystemRootBackground("#0F0F14", {
      setBackgroundColorAsync: async () => {
        throw new Error("root surface unavailable");
      },
    });

    await expect(result).resolves.toBeUndefined();
  });
});
