import { expect, test } from "bun:test";

/**
 * The service harness mocks modules globally, so it must run in its own
 * process; otherwise it contaminates the capability/preflight suites.
 */
test("prepareDesktopConnection selects Moonlight on direct engine with real proofs", () => {
  const child = Bun.spawnSync({
    cmd: ["bun", new URL("../test-support/remoteDesktopServiceHarness.ts", import.meta.url).pathname],
    env: { ...process.env, NODE_ENV: "test" },
    stdout: "pipe",
    stderr: "pipe",
  });
  const stdout = child.stdout.toString();
  const stderr = child.stderr.toString();
  expect(child.exitCode, `${stdout}\n${stderr}`).toBe(0);
  const result = JSON.parse(stdout.trim().split("\n").pop() || "{}");
  expect(result.moonlight).toBe("moonlight");
  expect(result.legacy).toBe("pinned-link");
  expect(result.trusted).toBe("trusted-lan");
  expect(result.tunnelCalls).toBe(1);
});
