// @ts-nocheck
import { describe, expect, test } from "bun:test";
import { submitProviderCommandAsUserInput } from "./providerCommandSubmission";

describe("provider command input preparation", () => {
  test("typed commands retain their draft recovery context", () => {
    const calls: unknown[][] = [];
    const attachments = [{ id: "a", name: "a.png", path: "/tmp/a.png" }];
    submitProviderCommandAsUserInput(
      "/review focused",
      "/review focused",
      attachments,
      (...args) => calls.push(args),
    );
    expect(calls).toEqual([
      ["/review focused", "/review focused", attachments],
    ]);
  });

  test("picker commands still create a local optimistic user row", () => {
    const calls: unknown[][] = [];
    submitProviderCommandAsUserInput(
      "/status",
      undefined,
      undefined,
      (...args) => calls.push(args),
    );
    expect(calls).toEqual([["/status", "/status", []]]);
  });

  test("thread controls use the same local row and recovery context", () => {
    const attachments = [{ id: "queued-file", path: "/tmp/queued.txt" }];
    for (const command of ["/new", "/clear"]) {
      const calls: unknown[][] = [];
      submitProviderCommandAsUserInput(
        command,
        command,
        attachments,
        (...args) => calls.push(args),
      );
      expect(calls).toEqual([[command, command, attachments]]);
    }
  });
});
