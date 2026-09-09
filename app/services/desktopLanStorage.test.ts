import { beforeEach, expect, mock, test } from "bun:test";
import type { StoredServer } from "./storedServerContract";

if (!process.env.ZEN_DESKTOP_LAN_STORAGE_CHILD) {
  test("desktop LAN consent storage runs in an isolated process", () => {
    const result = Bun.spawnSync(
      [process.execPath, "test", import.meta.filename],
      { env: { ...process.env, ZEN_DESKTOP_LAN_STORAGE_CHILD: "1" } },
    );
    if (result.exitCode) {
      throw new Error(
        new TextDecoder().decode(result.stdout) +
          new TextDecoder().decode(result.stderr),
      );
    }
    expect(result.exitCode).toBe(0);
  });
} else {
  const values = new Map<string, string>();
  mock.module("@react-native-async-storage/async-storage", () => ({ default: {
    getItem: async (key: string) => values.get(key) ?? null,
    setItem: async (key: string, value: string) => { values.set(key, value); },
    removeItem: async (key: string) => { values.delete(key); },
  } }));
  const { getServers, saveServer, setDesktopLanConsent, removeServer } = await import("./storage");
  const server: StoredServer = { id: "lan", name: "Owned LAN", url: "ws://192.168.1.2:9876/ws",
    daemonId: "a".repeat(64), daemonPublicKey: "b".repeat(64), transportKind: "manual" };
  beforeEach(async () => { values.clear(); await saveServer(server); });

  test("approval survives a storage read and revocation removes it", async () => {
    await setDesktopLanConsent(server, true, () => true);
    expect((await getServers())[0].desktopLanConsent?.origin).toBe("ws://192.168.1.2:9876");
    await setDesktopLanConsent(server, false, () => true);
    expect((await getServers())[0].desktopLanConsent).toBeUndefined();
  });

  test("serialized endpoint update prevents a queued stale approval", async () => {
    const update = saveServer({ ...server, url: "ws://192.168.1.3:9876/ws" });
    const approve = setDesktopLanConsent(server, true, () => true);
    await update;
    await expect(approve).rejects.toThrow("paired server changed");
    expect((await getServers())[0].desktopLanConsent).toBeUndefined();
  });

  test("owner loss and removal never recreate or approve the previous server", async () => {
    await expect(setDesktopLanConsent(server, true, () => false)).rejects.toThrow("paired server changed");
    const removal = removeServer(server.id);
    const approve = setDesktopLanConsent(server, true, () => true);
    await removal;
    await expect(approve).rejects.toThrow("paired server changed");
    expect(await getServers()).toEqual([]);
  });

  test("concurrent server saves retain both owners without sharing approval", async () => {
    await Promise.all([setDesktopLanConsent(server, true, () => true),
      saveServer({ ...server, id: "other", url: "ws://192.168.1.3:9876/ws" })]);
    const saved = await getServers();
    expect(saved).toHaveLength(2);
    expect(saved.find((item) => item.id === "lan")?.desktopLanConsent).toBeDefined();
    expect(saved.find((item) => item.id === "other")?.desktopLanConsent).toBeUndefined();
  });
}
