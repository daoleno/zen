import { describe, expect, test } from "bun:test";
import {
  enqueueCurrentServerPersistence,
  resolveCurrentServerId,
  selectCurrentServer,
} from "./currentServerSelection";

const servers = [
  { id: "server-a", name: "A" },
  { id: "server-b", name: "B" },
];

describe("canonical current-server selection", () => {
  test("a newer explicit switch persists after an older delayed refresh write", async () => {
    let releaseRefreshWrite: () => void = () => undefined;
    const refreshWriteBlocked = new Promise<void>((resolve) => {
      releaseRefreshWrite = resolve;
    });
    let persisted = "server-a";
    let tail = Promise.resolve();

    const refreshWrite = enqueueCurrentServerPersistence(tail, async () => {
      await refreshWriteBlocked;
      persisted = "server-refresh";
    });
    tail = refreshWrite.tail;
    const switchWrite = enqueueCurrentServerPersistence(tail, async () => {
      persisted = "server-b";
    });
    tail = switchWrite.tail;

    releaseRefreshWrite();
    await tail;
    expect(persisted).toBe("server-b");
  });

  test("an explicit switch generation invalidates a refresh before it can persist", async () => {
    let refreshGeneration = 0;
    let persisted = "server-a";
    let tail = Promise.resolve();
    const refreshToken = ++refreshGeneration;

    const switchToken = ++refreshGeneration;
    const switchWrite = enqueueCurrentServerPersistence(tail, async () => {
      persisted = "server-b";
    });
    tail = switchWrite.tail;
    await tail;

    if (refreshToken === refreshGeneration) {
      const staleRefresh = enqueueCurrentServerPersistence(tail, async () => {
        persisted = "server-refresh";
      });
      tail = staleRefresh.tail;
    }
    await tail;

    expect(switchToken).toBe(refreshGeneration);
    expect(persisted).toBe("server-b");
  });

  test("keeps the persisted current server without consulting connection state", () => {
    expect(resolveCurrentServerId(servers, "server-b")).toBe("server-b");
    expect(selectCurrentServer(servers, "server-b")).toEqual(servers[1]);
  });

  test("uses array order only for one-time migration or removed configurations", () => {
    expect(resolveCurrentServerId(servers, null)).toBe("server-a");
    expect(resolveCurrentServerId(servers, "removed")).toBe("server-a");
    expect(resolveCurrentServerId([], "server-a")).toBeNull();
  });

  test("never invents a current server outside the canonical owner", () => {
    expect(selectCurrentServer(servers, "missing")).toBeNull();
    expect(selectCurrentServer(servers, null)).toBeNull();
  });
});
